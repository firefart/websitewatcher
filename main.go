package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/firefart/websitewatcher/internal/config"
	"github.com/firefart/websitewatcher/internal/database"
	"github.com/firefart/websitewatcher/internal/http"
	"github.com/firefart/websitewatcher/internal/mail"
	"github.com/firefart/websitewatcher/internal/watch"
	"golang.org/x/sync/semaphore"

	"github.com/sirupsen/logrus"
)

type app struct {
	logger     *logrus.Logger
	config     config.Configuration
	httpClient *http.HTTPClient
	mailer     *mail.Mail
	dryRun     bool
	db         *database.Database
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

func (app *app) logError(err error) {
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

	configuration, err := config.GetConfig(*configFile)
	if err != nil {
		return err
	}

	start := time.Now().UnixNano()
	db, err := database.ReadDatabase(configuration.Database)
	if err != nil {
		return err
	}

	// remove old websites in the database on each run
	db.CleanupDatabase(app.logger, configuration)

	httpClient := http.NewHTTPClient(configuration.Useragent, configuration.Timeout.Duration)
	mailer := mail.New(configuration, httpClient)

	app.config = configuration
	app.httpClient = httpClient
	app.dryRun = *dryRun
	app.db = db
	app.mailer = mailer

	ctx := context.Background()

	var wg sync.WaitGroup
	sem := semaphore.NewWeighted(configuration.ParallelChecks)
	for _, wc := range configuration.Watches {
		if wc.Disabled {
			app.logger.Infof("[%s] skipping because it's disabled", wc.Name)
			continue
		}

		if err := sem.Acquire(ctx, 1); err != nil {
			app.logError(err)
			continue
		}
		wg.Add(1)

		w := watch.New(wc, app.logger, httpClient)

		go func(w2 watch.Watch) {
			defer sem.Release(1)
			defer wg.Done()

			if err := app.processWatch(ctx, w2); err != nil {
				app.logError(fmt.Errorf("[%s] error: %w", w2.Name, err))
				if !app.dryRun {
					if err2 := app.mailer.SendErrorEmail(w2, err); err2 != nil {
						app.logError(err2)
						return
					}
				}
				return
			}
		}(w)
	}

	wg.Wait()

	db.SetLastRun(start)
	err = db.SaveDatabase(configuration.Database)
	if err != nil {
		return err
	}

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

	lastContent := app.db.GetDatabaseEntry(w.URL)
	// if it's a new website not yet in the database only process new entries and ignore old ones
	if lastContent == nil {
		// lastContent = nil on new sites not yet processed, so send no email here
		app.logger.Infof("[%s] new website detected, not comparing", w.Name)
		app.db.SetDatabaseEntry(w.URL, watchReturn.Body)
		return nil
	}

	if !bytes.Equal(lastContent, watchReturn.Body) {
		if app.dryRun {
			app.logger.Infof("[%s] Dry Run: Website differs", w.Name)
		} else {
			subject := fmt.Sprintf("[%s] change detected", w.Name)
			app.logger.Infof("%s - sending email", subject)
			text := fmt.Sprintf("Name: %s\nURL: %s\nRequest Duration: %s\nStatus: %d\nBodylen: %d", w.Name, w.URL, watchReturn.Duration.Round(time.Millisecond), watchReturn.StatusCode, len(watchReturn.Body))
			if err := app.mailer.SendDiffEmail(w, subject, text, string(lastContent), string(watchReturn.Body)); err != nil {
				return fmt.Errorf("error on sending email: %w", err)
			}
		}
	} else {
		app.logger.Infof("[%s] no change detected", w.Name)
	}

	// update database entry if we did not have any errors
	app.db.SetDatabaseEntry(w.URL, watchReturn.Body)

	return nil
}
