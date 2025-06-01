package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"runtime/debug"
	"time"

	"github.com/firefart/websitewatcher/internal/config"
	"github.com/firefart/websitewatcher/internal/database"
	"github.com/firefart/websitewatcher/internal/diff"
	"github.com/firefart/websitewatcher/internal/http"
	"github.com/firefart/websitewatcher/internal/mail"
	"github.com/firefart/websitewatcher/internal/taskmanager"
	"github.com/firefart/websitewatcher/internal/watch"
	"github.com/firefart/websitewatcher/internal/webhook"
	"github.com/goforj/godump"
	"github.com/google/uuid"
	"github.com/hashicorp/go-multierror"
)

type app struct {
	logger       *slog.Logger
	config       config.Configuration
	httpClient   *http.Client
	dryRun       bool
	dumpDiffHTML bool
	db           database.Interface
	taskmanager  *taskmanager.TaskManager
	errorOccured bool // only used in once mode to track if we should exit with an error code
	timezone     *time.Location
}

const (
	runModeCron = "cron"
	runModeOnce = "once"
)

func main() {
	var debugMode bool
	var configFilename string
	var jsonOutput bool
	var dryRun bool
	var dumpDiffHTML bool
	var version bool
	var configCheckMode bool
	var runMode string
	flag.BoolVar(&debugMode, "debug", false, "Enable DEBUG mode")
	flag.StringVar(&configFilename, "config", "", "config file to use")
	flag.BoolVar(&jsonOutput, "json", false, "output in json instead")
	flag.BoolVar(&dryRun, "dry-run", false, "dry-run - send no emails")
	flag.BoolVar(&dumpDiffHTML, "dump-diff-html", false, "dump the html diff to a file")
	flag.BoolVar(&configCheckMode, "configcheck", false, "just check the config")
	flag.BoolVar(&version, "version", false, "show version")
	flag.StringVar(&runMode, "mode", runModeCron, "runmode: cron or once")
	flag.Parse()

	if version {
		buildInfo, ok := debug.ReadBuildInfo()
		if !ok {
			fmt.Println("Unable to determine version information") // nolint:forbidigo
			os.Exit(1)
		}
		fmt.Printf("%s", buildInfo) // nolint:forbidigo
		os.Exit(0)
	}

	logger := newLogger(debugMode, jsonOutput)
	app := app{
		logger: logger,
	}

	var err error
	if configCheckMode {
		err = configCheck(configFilename)
	} else {
		err = app.run(dryRun, dumpDiffHTML, configFilename, runMode)
	}
	if err != nil {
		// check if we have a multierror
		var merr *multierror.Error
		if errors.As(err, &merr) {
			for _, e := range merr.Errors {
				app.logError(e)
			}
			os.Exit(1)
		}
		// a normal error
		app.logError(err)
		os.Exit(1)
	}

	// ensure we exit with an error code if an error occurred in once mode
	if runMode == runModeOnce && app.errorOccured {
		os.Exit(1)
	}
}

func (app *app) logError(err error) {
	app.errorOccured = true
	app.logger.Error("error occurred", slog.String("err", err.Error()))
}

func configCheck(configFilename string) error {
	_, err := config.GetConfig(configFilename)
	return err
}

func (app *app) run(dryRun, dumpDiffHTML bool, configFile string, runMode string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer cancel()

	if runMode != runModeCron && runMode != runModeOnce {
		return fmt.Errorf("invalid runmode %q, must be either cron or once", runMode)
	}

	if configFile == "" {
		return errors.New("please supply a config file")
	}

	configuration, err := config.GetConfig(configFile)
	if err != nil {
		return err
	}

	app.logger.Debug(godump.DumpStr(configuration))

	db, err := database.New(ctx, configuration, app.logger)
	if err != nil {
		return err
	}

	defer func() {
		if err := db.Close(configuration.GracefulTimeout); err != nil {
			app.logger.Error("error on database close", slog.String("err", err.Error()))
		}
	}()

	httpClient, err := http.NewHTTPClient(app.logger, configuration.Useragent, configuration.Timeout, configuration.Proxy)
	if err != nil {
		return err
	}

	app.config = configuration
	app.httpClient = httpClient
	app.dryRun = dryRun
	app.dumpDiffHTML = dumpDiffHTML
	app.db = db

	if configuration.Location != "" {
		tz, err := time.LoadLocation(configuration.Location)
		if err != nil {
			return fmt.Errorf("could not load timezone %q: %w", configuration.Location, err)
		}
		app.timezone = tz
	} else {
		app.timezone = time.UTC
	}

	app.taskmanager, err = taskmanager.New(app.logger)
	if err != nil {
		return fmt.Errorf("could not create taskmanager: %w", err)
	}

	// remove old websites in the database on each run
	newEntries, deletedRows, err := db.PrepareDatabase(ctx, configuration)
	if err != nil {
		return fmt.Errorf("[CLEANUP] %w", err)
	}
	if deletedRows > 0 {
		app.logger.Info("removed old entries from database", slog.Int("deleted-rows", deletedRows))
	}

	firstRunners := make(map[uuid.UUID]string)
	for _, wc := range configuration.Watches {
		if wc.Disabled {
			app.logger.Info("skipping watch because it's disabled", slog.String("name", wc.Name))
			continue
		}

		w := watch.New(wc, app.logger, httpClient)

		job := func() {
			if err := app.processWatch(ctx, w); err != nil {
				app.logError(fmt.Errorf("[%s] error: %w", w.Name, err))
				if !app.dryRun {
					mailer, mailErr := mail.New(configuration, app.logger)
					if mailErr != nil {
						app.logError(fmt.Errorf("[%s] error: %w", w.Name, mailErr))
						return
					}
					if err2 := mailer.SendErrorEmail(ctx, w, err); err2 != nil {
						app.logError(err2)
						return
					}
				}
				return
			}
		}
		switch runMode {
		case runModeOnce:
			// check the context in once mode to bail out on error or ctrl+c
			select {
			case <-ctx.Done():
				app.errorOccured = true
				return errors.New("execution cancelled before all jobs were run")
			default:
			}
			app.logger.Info("running watch immediately", slog.String("name", w.Name))
			job()
		case runModeCron:
			entryID, err := app.taskmanager.AddTask(w.Name, w.Cron, job)
			if err != nil {
				app.logError(err)
				continue
			}
			app.logger.Debug("added task", slog.String("id", entryID.String()), slog.String("name", w.Name), slog.String("schedule", w.Cron))

			// determine if it's a new job that has never been run
			for _, tmp := range newEntries {
				if tmp.Name == wc.Name && tmp.URL == wc.URL {
					firstRunners[entryID] = wc.Name
					break
				}
			}
		default:
			return fmt.Errorf("invalid runmode %q, must be either cron or once", runMode)
		}
	}

	// taskmanager is only used in cron mode
	if runMode == runModeCron {
		// all tasks added, start the cron
		app.taskmanager.Start()
		// if it's a new job run it manually to add a baseline to the database
		// also run as a go func so the program does not block
		for entryID, entryName := range firstRunners {
			go func(id uuid.UUID, name string) {
				app.logger.Debug("running new job", slog.String("name", name))
				if err := app.taskmanager.RunJob(id); err != nil {
					app.logError(err)
					return
				}
			}(entryID, entryName)
		}

		// wait for ctrl+c, only in cron mode
		<-ctx.Done()
		cancel()

		if err := app.taskmanager.Stop(); err != nil {
			return fmt.Errorf("error stopping taskmanager: %w", err)
		}
	}

	return nil
}

func (app *app) processWatch(ctx context.Context, w watch.Watch) error {
	app.logger.Debug(godump.DumpStr(w))
	watchReturn, err := w.Process(ctx, app.config)
	if err != nil {
		var urlErr *url.Error
		var invalidErr *watch.InvalidResponseError
		switch {
		case errors.As(err, &urlErr) && urlErr.Timeout():
			// ignore timeout errors so outer mail will not send emails on, them
			// we also do not update the database, so we keep the old, non timeout
			// content in there
			app.logger.Info("watch timed out, ignoring", slog.String("name", w.Name))
			return nil
		case errors.As(err, &invalidErr):
			// we still have an error or soft error after all retries
			app.logger.Error("invalid response", slog.String("name", w.Name), slog.String("error-message", invalidErr.ErrorMessage), slog.Int("error-code", invalidErr.StatusCode), slog.String("error-body", string(invalidErr.Body)), slog.Duration("duration", invalidErr.Duration))

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
				app.logger.Info("not sending error mail because status is excluded", slog.String("name", w.Name), slog.Int("status-code", invalidErr.StatusCode))
				return nil
			}

			// send mail to indicate we might have an error
			if !app.dryRun {
				app.logger.Info("sending watch error email", slog.String("name", w.Name))
				mailer, err := mail.New(app.config, app.logger)
				if err != nil {
					return fmt.Errorf("could not create mailer: %w", err)
				}
				if err := mailer.SendWatchError(ctx, w, invalidErr); err != nil {
					return fmt.Errorf("could not send error email: %w", err)
				}
			}
			return nil
		default:
			return fmt.Errorf("could not process watch: %w", err)
		}
	}

	watchID, lastFetch, lastContent, err := app.db.GetLastContent(ctx, w.Name, w.URL)
	if err != nil {
		// if it's a new website not yet in the database only process new entries and ignore old ones
		if errors.Is(err, database.ErrNotFound) {
			// lastContent = nil on new sites not yet processed, so send no email here
			app.logger.Info("new website detected, not comparing", slog.String("name", w.Name))
			app.logger.Debug("website content", slog.String("content", string(watchReturn.Body)))
			if _, err2 := app.db.InsertWatch(ctx, w.Name, w.URL, watchReturn.Body); err2 != nil {
				return fmt.Errorf("could not insert new watch: %w", err2)
			}
			return nil
		}
		// other error, just return it
		return fmt.Errorf("could not get last content: %w", err)
	}

	if !bytes.Equal(lastContent, watchReturn.Body) {
		m := diff.Metadata{
			Name:            w.Name,
			URL:             w.URL,
			Description:     w.Description,
			RequestDuration: watchReturn.Duration.Round(time.Millisecond),
			StatusCode:      watchReturn.StatusCode,
			BodyLength:      len(watchReturn.Body),
			LastFetch:       lastFetch.In(app.timezone),
		}
		d, err := diff.GenerateDiff(ctx, string(lastContent), string(watchReturn.Body))
		if err != nil {
			return fmt.Errorf("could not create diff: %w", err)
		}

		if app.dumpDiffHTML {
			// dump the diff to a file
			dumpFile := fmt.Sprintf("dump_%s_%s.html", time.Now().Format("20060102150405"), w.Name)
			h, err := d.HTML(ctx, &m)
			if err != nil {
				return fmt.Errorf("could not create html diff: %w", err)
			}
			if err := os.WriteFile(dumpFile, []byte(h), 0o600); err != nil {
				return fmt.Errorf("could not write diff file: %w", err)
			}
			app.logger.Info("dumped html diff to file", slog.String("file", dumpFile))
		}

		if app.dryRun {
			app.logger.Info("Dry Run: Website differs", slog.String("name", w.Name), slog.String("last-content", string(lastContent)), slog.String("returned-body", string(watchReturn.Body)))
		} else {
			app.logger.Info("sending diff email", slog.String("name", w.Name))
			mailer, err := mail.New(app.config, app.logger)
			if err != nil {
				return fmt.Errorf("could not create mailer: %w", err)
			}
			subject := fmt.Sprintf("[%s] change detected", w.Name)
			if err := mailer.SendDiffEmail(ctx, subject, d, &m, w.AdditionalTo); err != nil {
				return fmt.Errorf("error on sending email: %w", err)
			}
			for _, wh := range w.Webhooks {
				app.logger.Info("sending webhook", slog.String("name", w.Name), slog.String("url", wh.URL))
				if err := webhook.Send(ctx, app.httpClient, wh, d, &m); err != nil {
					return fmt.Errorf("could not send webhook: %w", err)
				}
			}
		}
	} else {
		app.logger.Info("no change detected", slog.String("name", w.Name))
	}

	// update database entry if we did not have any errors
	if err := app.db.UpdateLastContent(ctx, watchID, watchReturn.Body); err != nil {
		return fmt.Errorf("could not update last content: %w", err)
	}

	return nil
}
