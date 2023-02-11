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
	"golang.org/x/sync/semaphore"

	"github.com/sirupsen/logrus"
)

type app struct {
	log        *logrus.Logger
	config     *config.Configuration
	httpClient *http.HTTPClient
	mailer     *mail.Mail
	testMode   bool
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
	testMode := flag.Bool("test", false, "use test mode (no email sending)")
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
	app.testMode = *testMode
	app.db = db
	app.mailer = mailer

	ctx := context.Background()

	var wg sync.WaitGroup
	sem := semaphore.NewWeighted(configuration.ParallelChecks)
	for _, watch := range configuration.Watches {
		if watch.Disabled {
			app.log.Infof("skipping %s: %s", watch.Name, watch.URL)
			continue
		}

		if err := sem.Acquire(ctx, 1); err != nil {
			app.logError(err)
			continue
		}
		wg.Add(1)

		go func(watch config.Watch) {
			defer sem.Release(1)
			defer wg.Done()

			if err := app.processWatch(ctx, watch); err != nil {
				app.logError(fmt.Errorf("error on %s: %w", watch.Name, err))
				if err2 := app.mailer.SendErrorEmail(watch, err); err2 != nil {
					app.logError(err2)
					return
				}
				return
			}
		}(watch)
	}

	wg.Wait()

	db.SetLastRun(start)
	err = db.SaveDatabase(configuration.Database)
	if err != nil {
		return err
	}

	return nil
}

func (app *app) checkWatch(ctx context.Context, watch config.Watch) (int, map[string][]string, time.Duration, []byte, error) {
	var statusCode int
	var requestDuration time.Duration
	var body []byte
	var header map[string][]string
	var err error
	// check with retries
	for i := 1; i <= app.config.Retry.Count; i++ {
		app.log.Debugf("try #%d for %s", i, watch.URL)
		statusCode, header, requestDuration, body, err = app.httpClient.CheckWatch(ctx, watch)
		if err == nil {
			// first check if our body matches the retry pattern and retry
			if watch.RetryOnMatch != "" {
				re, err := regexp.Compile(watch.RetryOnMatch)
				if err != nil {
					return -1, nil, -1, nil, fmt.Errorf("could not compile pattern %s: %w", watch.Pattern, err)
				}
				if re.Match(body) {
					// retry the request as the body matches
					app.log.Debugf("retrying %s because body matches retry pattern", watch.URL)
					continue
				}
			}

			// no error and no retry pattern matched, so we count it as success --> break out
			break
		}

		if i >= app.config.Retry.Count {
			// break out to not print the rety message on the last try
			break
		}

		// if we reach here, we have an error, retry
		if app.config.Retry.Delay.Duration > 0 {
			app.log.Error(fmt.Errorf("got error on try #%d for %s, retrying after %s: %w", i, watch.URL, app.config.Retry.Delay.Duration, err))
			select {
			case <-ctx.Done():
				return -1, nil, -1, nil, ctx.Err()
			case <-time.After(app.config.Retry.Delay.Duration):
			}
		} else {
			app.log.Error(fmt.Errorf("got error on try #%d for %s, retrying: %w", i, watch.URL, err))
		}
	}

	// last error still set, bail out
	if err != nil {
		return -1, nil, -1, nil, err
	}

	return statusCode, header, requestDuration, body, nil
}

func (app *app) processWatch(ctx context.Context, watch config.Watch) error {
	app.log.Infof("processing %s: %s", watch.Name, watch.URL)
	lastContent := app.db.GetDatabaseEntry(watch.URL)

	statusCode, _, requestDuration, body, err := app.checkWatch(ctx, watch)
	if err != nil {
		var invalidErr *http.InvalidResponseError
		var urlErr *url.Error
		switch {
		case errors.As(err, &invalidErr):
			app.logError(fmt.Errorf("invalid response for %s - status: %d, body: %s, duration: %s", watch.Name, invalidErr.StatusCode, string(invalidErr.Body), requestDuration))

			for _, ignore := range app.config.HTTPErrorsToIgnore {
				if invalidErr.StatusCode == ignore {
					// status is ignored, bail out
					return nil
				}
			}

			for _, ignore := range watch.AdditionalHTTPErrorsToIgnore {
				if invalidErr.StatusCode == ignore {
					// status is ignored, bail out
					return nil
				}
			}

			// send mail to indicate we might have an error
			subject := fmt.Sprintf("Invalid response for %s", watch.Name)
			text := fmt.Sprintf("Name: %s\nURL: %s\nRequest Duration: %s\nStatus: %d\nBodylen: %d\nHeader:\n%s\nBody:\n%s", watch.Name, watch.URL, requestDuration.Round(time.Millisecond), invalidErr.StatusCode, len(invalidErr.Body), html.EscapeString(formatHeaders(invalidErr.Header)), html.EscapeString(string(invalidErr.Body)))
			htmlContent, err := app.generateHTMLContentForEmail(text, false, "", "")
			if err != nil {
				return fmt.Errorf("error on creating htmlcontent: %w", err)
			}
			if err := app.mailer.SendHTMLEmail(watch, subject, htmlContent); err != nil {
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
	if watch.Pattern != "" {
		re, err := regexp.Compile(watch.Pattern)
		if err != nil {
			return fmt.Errorf("could not compile pattern %s: %w", watch.Pattern, err)
		}
		match := re.FindSubmatch(body)
		if len(match) < 2 {
			return fmt.Errorf("pattern %s did not match %s", watch.Pattern, string(body))
		}
		body = match[1]
	}

	for _, replace := range watch.Replaces {
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
		app.log.Debugf("new website %s %s detected, not comparing", watch.Name, watch.URL)
		app.db.SetDatabaseEntry(watch.URL, body)
		return nil
	}

	if !bytes.Equal(lastContent, body) {
		if app.testMode {
			app.log.Debugf("Website %s %s differ! Would send email in prod", watch.Name, watch.URL)
		} else {
			subject := fmt.Sprintf("Detected change on %s", watch.Name)
			app.log.Infof(subject)
			text := fmt.Sprintf("Name: %s\nURL: %s\nRequest Duration: %s\nStatus: %d\nBodylen: %d", watch.Name, watch.URL, requestDuration.Round(time.Millisecond), statusCode, len(body))
			htmlContent, err := app.generateHTMLContentForEmail(text, true, string(lastContent), string(body))
			if err != nil {
				return fmt.Errorf("error on creating htmlcontent: %w", err)
			}
			if err := app.mailer.SendHTMLEmail(watch, subject, htmlContent); err != nil {
				return fmt.Errorf("error on sending email: %w", err)
			}
		}
	}

	// update database entry if we did not have any errors
	app.db.SetDatabaseEntry(watch.URL, body)

	return nil
}
