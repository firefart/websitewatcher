package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/firefart/websitewatcher/internal/config"
	"github.com/firefart/websitewatcher/internal/database"
	"github.com/firefart/websitewatcher/internal/diff"
	"github.com/firefart/websitewatcher/internal/http"

	"github.com/sirupsen/logrus"

	gomail "gopkg.in/mail.v2"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("[ERROR] %v", err)
	}
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
	m.SetHeader("Subject", subject)
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

func run() error {
	log := logrus.New()

	configFile := flag.String("config", "", "config file to use")
	debug := flag.Bool("debug", false, "Print debug output")
	flag.Parse()

	log.SetOutput(os.Stdout)
	log.SetLevel(logrus.InfoLevel)
	if *debug {
		log.SetLevel(logrus.DebugLevel)
	}

	config, err := config.GetConfig(*configFile)
	if err != nil {
		return err
	}

	start := time.Now().UnixNano()
	db, err := database.ReadDatabase(config.Database)
	if err != nil {
		return err
	}

	// remove old websites in the database on each run
	db.CleanupDatabase(log, *config)

	httpClient := http.NewHTTPClient(config.Useragent, config.Timeout.Duration, *debug, log)
	ctx := context.Background()

	var wg sync.WaitGroup
	for _, watch := range config.Watches {
		wg.Add(1)
		go checkSite(ctx, &wg, config, log, httpClient, watch, *debug, db)
	}

	wg.Wait()

	db.SetLastRun(start)
	err = db.SaveDatabase(config.Database)
	if err != nil {
		return err
	}

	return nil
}

func checkSite(ctx context.Context, wg *sync.WaitGroup, config *config.Configuration, log *logrus.Logger, httpClient *http.HTTPClient, watch config.Watch, debug bool, db *database.Database) {
	defer wg.Done()

	log.Debugf("processing %s: %s", watch.Name, watch.URL)
	lastContent := db.GetDatabaseEntry(watch.URL)

	statusCode, body, err := httpClient.GetRequest(ctx, watch.URL)
	if err != nil {
		log.Errorf("[ERROR]: %v", err)
		return
	}

	if statusCode != 200 || len(body) == 0 || http.IsSoftError(body) {
		// send mail to indicate we might have an error
		subject := fmt.Sprintf("[WEBSITEWATCHER] Invalid response for %s", watch.Name)
		text := fmt.Sprintf("Name: %s\nURL: %s\nStatus: %d\nBodylen: %d", watch.Name, watch.URL, statusCode, len(body))
		htmlContent, err := htmlContent(httpClient, text, false, "", "")
		if err != nil {
			log.Errorf("[ERROR]: %v", err)
			return
		}
		if err := sendEmail(config, watch, subject, htmlContent); err != nil {
			log.Errorf("[ERROR]: %v", err)
		}
		// do not process non 200 responses and save to database
		return
	}

	// if it's a new website not yet in the database only process new entries and ignore old ones
	if lastContent == nil {
		// lastContent = nil on new sites not yet processed, so send no email here
		log.Debugf("new website %s %s detected, not comparing", watch.Name, watch.URL)
		db.SetDatabaseEntry(watch.URL, body)
		return
	}

	if !bytes.Equal(lastContent, body) {
		if debug {
			log.Debugf("Website %s %s differ! Would send email in prod", watch.Name, watch.URL)
		} else {
			subject := fmt.Sprintf("[WEBSITEWATCHER] Detected change on %s", watch.Name)
			text := fmt.Sprintf("Name: %s\nURL: %s\nStatus: %d\nBodylen: %d", watch.Name, watch.URL, statusCode, len(body))
			htmlContent, err := htmlContent(httpClient, text, true, string(lastContent), string(body))
			if err != nil {
				log.Errorf("[ERROR]: %v", err)
				return
			}
			if err := sendEmail(config, watch, subject, htmlContent); err != nil {
				log.Errorf("[ERROR]: %v", err)
			}
		}
	}

	// update database entry if we did not have any errors
	db.SetDatabaseEntry(watch.URL, body)
}
