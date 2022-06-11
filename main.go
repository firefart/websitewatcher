package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"html"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/firefart/websitewatcher/internal/config"
	"github.com/firefart/websitewatcher/internal/database"
	"github.com/firefart/websitewatcher/internal/diff"
	"github.com/firefart/websitewatcher/internal/http"
	"golang.org/x/sync/semaphore"

	"github.com/sirupsen/logrus"

	gomail "gopkg.in/mail.v2"
)

func main() {
	log := logrus.New()
	if err := run(log); err != nil {
		logError(log, err)
		os.Exit(1)
	}
}

func logError(log *logrus.Logger, err error) {
	log.Errorf("[ERROR] %v", err)
}

func htmlContent(httpClient *http.HTTPClient, body string, includeDiff bool, text1, text2 string) (string, error) {
	body = strings.ReplaceAll(body, "\n", "<br>\n")

	if includeDiff {
		css, html, err := diff.DiffAPI(httpClient, text1, text2)
		if err != nil {
			return "", err
		}
		body = fmt.Sprintf("<html><head><style>%s</style></head><body>%s<br><br>\n%s</body></html>", css, body, html)
	} else {
		body = fmt.Sprintf("<html><body>%s</body></html>", body)
	}
	return body, nil
}

func sendEmail(config *config.Configuration, watch config.Watch, subject, body string) error {
	to := config.Mail.To
	if len(watch.AdditionalTo) > 0 {
		to = append(to, watch.AdditionalTo...)
	}

	m := gomail.NewMessage()
	m.SetAddressHeader("From", config.Mail.From.Mail, config.Mail.From.Name)
	m.SetHeader("To", to...)
	m.SetHeader("Subject", fmt.Sprintf("[WEBSITEWATCHER] %s", subject))
	m.SetBody("text/html", body)
	d := gomail.NewDialer(config.Mail.Server, config.Mail.Port, config.Mail.User, config.Mail.Password)

	if config.Mail.SkipTLS {
		d.TLSConfig = &tls.Config{InsecureSkipVerify: true}
	}

	if err := d.DialAndSend(m); err != nil {
		return err
	}
	return nil
}

func run(log *logrus.Logger) error {
	configFile := flag.String("config", "", "config file to use")
	debug := flag.Bool("debug", false, "Print debug output")
	testMode := flag.Bool("test", false, "use test mode (no email sending)")
	flag.Parse()

	log.SetOutput(os.Stdout)
	log.SetLevel(logrus.InfoLevel)
	if *debug {
		log.SetLevel(logrus.DebugLevel)
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
	db.CleanupDatabase(log, *configuration)

	httpClient := http.NewHTTPClient(configuration.Useragent, configuration.Timeout.Duration, *debug, log)
	ctx := context.Background()

	var wg sync.WaitGroup
	sem := semaphore.NewWeighted(configuration.ParallelChecks)
	for _, watch := range configuration.Watches {
		if watch.Disabled {
			log.Infof("skipping %s: %s", watch.Name, watch.URL)
			continue
		}

		if err := sem.Acquire(ctx, 1); err != nil {
			logError(log, err)
			continue
		}
		wg.Add(1)

		go func(watch config.Watch) {
			defer sem.Release(1)
			defer wg.Done()

			if err := checkSite(ctx, configuration, log, httpClient, watch, *testMode, db); err != nil {
				logError(log, fmt.Errorf("error on %s: %w", watch.Name, err))
				subject := fmt.Sprintf("error on %s", watch.Name)
				htmlContent := html.EscapeString(err.Error())
				if err2 := sendEmail(configuration, watch, subject, htmlContent); err2 != nil {
					logError(log, err2)
				}
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

func checkSite(ctx context.Context, config *config.Configuration, log *logrus.Logger, httpClient *http.HTTPClient, watch config.Watch, testMode bool, db *database.Database) error {
	log.Infof("processing %s: %s", watch.Name, watch.URL)
	lastContent := db.GetDatabaseEntry(watch.URL)

	statusCode, body, err := httpClient.GetRequest(ctx, watch.URL)
	if err != nil {
		return fmt.Errorf("error on get request: %w", err)
	}

	if statusCode != 200 || len(body) == 0 || http.IsSoftError(body) {
		// send mail to indicate we might have an error
		subject := fmt.Sprintf("Invalid response for %s", watch.Name)
		text := fmt.Sprintf("Name: %s\nURL: %s\nStatus: %d\nBodylen: %d\nBody: %s", watch.Name, watch.URL, statusCode, len(body), html.EscapeString(string(body)))
		htmlContent, err := htmlContent(httpClient, text, false, "", "")
		if err != nil {
			return fmt.Errorf("error on creating htmlcontent: %w", err)
		}
		// do not process non 200 responses and save to database
		if err := sendEmail(config, watch, subject, htmlContent); err != nil {
			return fmt.Errorf("error on sending email: %w", err)
		}
		return nil
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
		re, err := regexp.Compile(replace.Pattern)
		if err != nil {
			return fmt.Errorf("could not compile replace pattern %s: %w", replace.Pattern, err)
		}
		body = re.ReplaceAll(body, []byte(replace.ReplaceWith))
	}

	// if it's a new website not yet in the database only process new entries and ignore old ones
	if lastContent == nil {
		// lastContent = nil on new sites not yet processed, so send no email here
		log.Debugf("new website %s %s detected, not comparing", watch.Name, watch.URL)
		db.SetDatabaseEntry(watch.URL, body)
		return nil
	}

	if !bytes.Equal(lastContent, body) {
		if testMode {
			log.Debugf("Website %s %s differ! Would send email in prod", watch.Name, watch.URL)
		} else {
			subject := fmt.Sprintf("Detected change on %s", watch.Name)
			log.Infof(subject)
			text := fmt.Sprintf("Name: %s\nURL: %s\nStatus: %d\nBodylen: %d", watch.Name, watch.URL, statusCode, len(body))
			htmlContent, err := htmlContent(httpClient, text, true, string(lastContent), string(body))
			if err != nil {
				return fmt.Errorf("error on creating htmlcontent: %w", err)
			}
			if err := sendEmail(config, watch, subject, htmlContent); err != nil {
				return fmt.Errorf("error on sending email: %w", err)
			}
		}
	}

	// update database entry if we did not have any errors
	db.SetDatabaseEntry(watch.URL, body)

	return nil
}
