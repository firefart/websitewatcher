package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"html"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/firefart/websitewatcher/internal/config"
	"github.com/firefart/websitewatcher/internal/database"
	"github.com/firefart/websitewatcher/internal/diff"
	"github.com/firefart/websitewatcher/internal/http"
	"github.com/firefart/websitewatcher/internal/mail"
	"github.com/firefart/websitewatcher/internal/watch"
	"golang.org/x/sync/semaphore"

	"github.com/sirupsen/logrus"
)

type app struct {
	log        *logrus.Logger
	config     *config.Configuration
	httpClient *http.HTTPClient
	mailer     *mail.Mail
	dryRun     bool
	db         *database.Database
}

func main() {
	log := logrus.New()
	app := app{
		log: log,
	}
	if err := app.run(); err != nil {
		app.logError(err)
		os.Exit(1)
	}
}

func (app *app) logError(err error) {
	app.log.Errorf("[ERROR] %v", err)
}

func (app *app) generateHTMLContentForEmail(body string, includeDiff bool, text1, text2 string) (string, error) {
	body = strings.ReplaceAll(body, "\n", "<br>\n")

	if includeDiff {
		css, html, err := diff.DiffAPI(app.httpClient, text1, text2)
		if err != nil {
			return "", err
		}
		body = fmt.Sprintf("<html><head><style>%s</style></head><body>%s<br><br>\n%s</body></html>", css, body, html)
	} else {
		body = fmt.Sprintf("<html><body>%s</body></html>", body)
	}
	return body, nil
}

func formatHeaders(header map[string][]string) string {
	var sb strings.Builder
	for key, value := range header {
		sb.WriteString(fmt.Sprintf("%s: %s\n", key, strings.Join(value, ", ")))
	}
	return sb.String()
}

func (app *app) run() error {
	configFile := flag.String("config", "", "config file to use")
	debug := flag.Bool("debug", false, "Print debug output")
	dryRun := flag.Bool("dry-run", false, "dry-run - send no emails")
	flag.Parse()

	app.log.SetOutput(os.Stdout)
	app.log.SetLevel(logrus.InfoLevel)
	if *debug {
		app.log.SetLevel(logrus.DebugLevel)
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
	db.CleanupDatabase(app.log, *configuration)

	httpClient := http.NewHTTPClient(configuration.Useragent, configuration.Timeout.Duration)
	mailer := mail.NewMail(configuration)

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
			app.log.Infof("skipping %s: %s", wc.Name, wc.URL)
			continue
		}

		if err := sem.Acquire(ctx, 1); err != nil {
			app.logError(err)
			continue
		}
		wg.Add(1)

		w := watch.New(wc, app.log, httpClient)

		go func(w2 watch.Watch) {
			defer sem.Release(1)
			defer wg.Done()

			if err := app.processWatch(ctx, w2); err != nil {
				app.logError(fmt.Errorf("error on %s: %w", w2.Name, err))
				if err2 := app.mailer.SendErrorEmail(w2, err); err2 != nil {
					app.logError(err2)
					return
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
	app.log.Infof("processing %s: %s", w.Name, w.URL)
	lastContent := app.db.GetDatabaseEntry(w.URL)

	retries := app.config.Retry.Count
	retryDelay := app.config.Retry.Delay.Duration
	statusCode, _, requestDuration, body, err := w.CheckWithRetries(ctx, retries, retryDelay)
	if err != nil {
		var invalidErr *http.InvalidResponseError
		var urlErr *url.Error
		switch {
		case errors.As(err, &invalidErr):
			app.logError(fmt.Errorf("invalid response for %s - status: %d, body: %s, duration: %s", w.Name, invalidErr.StatusCode, string(invalidErr.Body), requestDuration))

			for _, ignore := range app.config.HTTPErrorsToIgnore {
				if invalidErr.StatusCode == ignore {
					// status is ignored, bail out
					return nil
				}
			}

			for _, ignore := range w.AdditionalHTTPErrorsToIgnore {
				if invalidErr.StatusCode == ignore {
					// status is ignored, bail out
					return nil
				}
			}

			// send mail to indicate we might have an error
			subject := fmt.Sprintf("Invalid response for %s", w.Name)
			text := fmt.Sprintf("Name: %s\nURL: %s\nRequest Duration: %s\nStatus: %d\nBodylen: %d\nHeader:\n%s\nBody:\n%s", w.Name, w.URL, requestDuration.Round(time.Millisecond), invalidErr.StatusCode, len(invalidErr.Body), html.EscapeString(formatHeaders(invalidErr.Header)), html.EscapeString(string(invalidErr.Body)))
			htmlContent, err := app.generateHTMLContentForEmail(text, false, "", "")
			if err != nil {
				return fmt.Errorf("error on creating htmlcontent: %w", err)
			}
			if err := app.mailer.SendHTMLEmail(w, subject, htmlContent); err != nil {
				return fmt.Errorf("error on sending email: %w", err)
			}
			return nil
		case errors.As(err, &urlErr) && urlErr.Timeout():
			// ignore timeout errors so outer mail will not send emails on them
			return nil
		default:
			// no custom handled error, return it so outer loop can handle it
			return err
		}
	}

	// extract content
	if w.Pattern != "" {
		re, err := regexp.Compile(w.Pattern)
		if err != nil {
			return fmt.Errorf("could not compile pattern %s: %w", w.Pattern, err)
		}
		match := re.FindSubmatch(body)
		if len(match) < 2 {
			return fmt.Errorf("pattern %s did not match %s", w.Pattern, string(body))
		}
		body = match[1]
	}

	for _, replace := range w.Replaces {
		app.log.Debugf("replacing %s", replace.Pattern)
		re, err := regexp.Compile(replace.Pattern)
		if err != nil {
			return fmt.Errorf("could not compile replace pattern %s: %w", replace.Pattern, err)
		}
		body = re.ReplaceAll(body, []byte(replace.ReplaceWith))
		app.log.Debugf("After %s:\n%s\n\n", replace.Pattern, string(body))
	}

	// if it's a new website not yet in the database only process new entries and ignore old ones
	if lastContent == nil {
		// lastContent = nil on new sites not yet processed, so send no email here
		app.log.Debugf("new website %s %s detected, not comparing", w.Name, w.URL)
		app.db.SetDatabaseEntry(w.URL, body)
		return nil
	}

	if !bytes.Equal(lastContent, body) {
		if app.dryRun {
			app.log.Debugf("Dry Run: Website %s %s differ", w.Name, w.URL)
		} else {
			subject := fmt.Sprintf("Detected change on %s", w.Name)
			app.log.Infof(subject)
			text := fmt.Sprintf("Name: %s\nURL: %s\nRequest Duration: %s\nStatus: %d\nBodylen: %d", w.Name, w.URL, requestDuration.Round(time.Millisecond), statusCode, len(body))
			htmlContent, err := app.generateHTMLContentForEmail(text, true, string(lastContent), string(body))
			if err != nil {
				return fmt.Errorf("error on creating htmlcontent: %w", err)
			}
			if err := app.mailer.SendHTMLEmail(w, subject, htmlContent); err != nil {
				return fmt.Errorf("error on sending email: %w", err)
			}
		}
	}

	// update database entry if we did not have any errors
	app.db.SetDatabaseEntry(w.URL, body)

	return nil
}
