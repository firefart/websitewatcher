package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"time"

	"github.com/firefart/websitewatcher/internal/config"
	"github.com/firefart/websitewatcher/internal/database"
	"github.com/firefart/websitewatcher/internal/http"
	"github.com/firefart/websitewatcher/internal/mail"
	"github.com/firefart/websitewatcher/internal/taskmanager"
	"github.com/firefart/websitewatcher/internal/watch"
	"github.com/robfig/cron/v3"

	"github.com/sirupsen/logrus"
)

type app struct {
	logger      *logrus.Logger
	config      config.Configuration
	httpClient  *http.HTTPClient
	mailer      *mail.Mail
	dryRun      bool
	db          *database.Database
	taskmanager *taskmanager.TaskManager
}

func main() {
	logger := logrus.New()
	app := app{
		logger: logger,
	}
	if err := app.run(); err != nil {
		app.logError(err)
		os.Exit(1)
	}
}

func (app app) logError(err error) {
	app.logger.Errorf("[ERROR] %v", err)
}

func (app *app) run() error {
	configFile := flag.String("config", "", "config file to use")
	debug := flag.Bool("debug", false, "Print debug output")
	dryRun := flag.Bool("dry-run", false, "dry-run - send no emails")
	flag.Parse()

	app.logger.SetOutput(os.Stdout)
	app.logger.SetLevel(logrus.InfoLevel)
	if *debug {
		app.logger.SetLevel(logrus.DebugLevel)
	}

	if *configFile == "" {
		return fmt.Errorf("please supply a config file")
	}

	configuration, err := config.GetConfig(*configFile)
	if err != nil {
		return err
	}

	db, err := database.New(configuration)
	if err != nil {
		return err
	}
	defer db.Close()

	httpClient := http.NewHTTPClient(configuration.Useragent, configuration.Timeout)
	mailer := mail.New(configuration, httpClient, app.logger)

	app.config = configuration
	app.httpClient = httpClient
	app.dryRun = *dryRun
	app.db = db
	app.mailer = mailer
	app.taskmanager = taskmanager.New(app.logger)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer cancel()

	// remove old websites in the database on each run
	newEntries, deletedRows, err := db.PrepareDatabase(ctx, configuration)
	if err != nil {
		return fmt.Errorf("[CLEANUP] %w", err)
	}
	if deletedRows > 0 {
		app.logger.Infof("Removed %d old entries from database", deletedRows)
	}

	firstRunners := make(map[cron.EntryID]string)
	for _, wc := range configuration.Watches {
		if wc.Disabled {
			app.logger.Infof("[%s] skipping because it's disabled", wc.Name)
			continue
		}

		w := watch.New(wc, app.logger, httpClient)

		job := func() {
			if err := app.processWatch(ctx, w); err != nil {
				app.logError(fmt.Errorf("[%s] error: %w", w.Name, err))
				if !app.dryRun {
					if err2 := app.mailer.SendErrorEmail(w, err); err2 != nil {
						app.logError(err2)
						return
					}
				}
				return
			}
		}

		entryID, err := app.taskmanager.AddTask(w.Cron, job)
		if err != nil {
			app.logError(err)
			continue
		}
		app.logger.Debugf("added task %d for %s (%s)", entryID, w.Name, w.Cron)

		// determine if it's a new job that has never been run
		for _, tmp := range newEntries {
			if tmp.Name == wc.Name && tmp.URL == wc.URL {
				firstRunners[entryID] = wc.Name
				break
			}
		}
	}

	// all tasks added, start the cron
	app.taskmanager.Start()

	// if it's a new job run it manually to add a baseline to the database
	// also run as a go func so the program does not block
	for entryID, entryName := range firstRunners {
		go func(id cron.EntryID, name string) {
			app.logger.Debugf("running job for %s as it's a new entry", name)
			if err := app.taskmanager.RunJob(id); err != nil {
				app.logError(err)
				return
			}
		}(entryID, entryName)
	}

	// wait for ctrl+c
	<-ctx.Done()
	cancel()

	app.taskmanager.Stop()

	return nil
}

func (app *app) processWatch(ctx context.Context, w watch.Watch) error {
	watchReturn, err := w.Process(ctx, app.config)
	if err != nil {
		var urlErr *url.Error
		var invalidErr *watch.InvalidResponseError
		switch {
		case errors.As(err, &urlErr) && urlErr.Timeout():
			// ignore timeout errors so outer mail will not send emails on them
			// we also do not update the database so we keep the old, non timeout
			// content in there
			app.logger.Infof("[%s] timed out, ignoring", w.Name)
			return nil
		case errors.As(err, &invalidErr):
			// we still have an error or soft error after all retries
			app.logger.Errorf("[%s] invalid response - message: %s, status: %d, body: %s, duration: %s", w.Name, invalidErr.ErrorMessage, invalidErr.StatusCode, string(invalidErr.Body), invalidErr.Duration)

			// do not send error emails on these status codes
			ignoreStatusCode := false
			for _, ignore := range app.config.NoErrorMailOnStatusCode {
				if invalidErr.StatusCode == ignore {
					ignoreStatusCode = true
				}
			}
			// if we hit an error that we should ignore, bail out
			for _, ignore := range w.NoErrorMailOnStatusCode {
				if invalidErr.StatusCode == ignore {
					ignoreStatusCode = true
				}
			}
			// if statuscode is ignored, do not send email
			if ignoreStatusCode {
				app.logger.Infof("[%s] not sending error mail because status %d is excluded", w.Name, invalidErr.StatusCode)
				return nil
			}

			// send mail to indicate we might have an error
			if !app.dryRun {
				app.logger.Infof("[%s] sending watch error email", w.Name)
				if err := app.mailer.SendWatchError(w, invalidErr); err != nil {
					return err
				}
			}
			return nil
		default:
			return err
		}
	}

	watchID, lastContent, err := app.db.GetLastContent(ctx, w.Name, w.URL)
	if err != nil {
		// if it's a new website not yet in the database only process new entries and ignore old ones
		if errors.Is(err, database.ErrNotFound) {
			// lastContent = nil on new sites not yet processed, so send no email here
			app.logger.Infof("[%s] new website detected, not comparing", w.Name)
			if _, err := app.db.InsertLastContent(ctx, w.Name, w.URL, watchReturn.Body); err != nil {
				return err
			}
			return nil
		}
		// other error, just return it
		return err
	}

	if !bytes.Equal(lastContent, watchReturn.Body) {
		if app.dryRun {
			app.logger.Infof("[%s] Dry Run: Website differs", w.Name)
			app.logger.Debugf("[%s] Last Body %s", w.Name, lastContent)
			app.logger.Debugf("[%s] Returned Body %s", w.Name, watchReturn.Body)
		} else {
			subject := fmt.Sprintf("[%s] change detected", w.Name)
			app.logger.Infof("%s - sending email", subject)
			text := fmt.Sprintf("Name: %s\nURL: %s\nRequest Duration: %s\nStatus: %d\nBodylen: %d", w.Name, w.URL, watchReturn.Duration.Round(time.Millisecond), watchReturn.StatusCode, len(watchReturn.Body))
			if err := app.mailer.SendDiffEmail(w, app.config.DiffMethod, subject, text, string(lastContent), string(watchReturn.Body)); err != nil {
				return fmt.Errorf("error on sending email: %w", err)
			}
		}
	} else {
		app.logger.Infof("[%s] no change detected", w.Name)
	}

	// update database entry if we did not have any errors
	if err := app.db.UpdateLastContent(ctx, watchID, watchReturn.Body); err != nil {
		return err
	}

	return nil
}
