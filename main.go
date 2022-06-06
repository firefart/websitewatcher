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

func sendEmail(config *config.Configuration, httpClient *http.HTTPClient, watch config.Watch, subject, body, text1, text2 string) error {
	htmlDiff, err := diff.DiffAPI(httpClient, text1, text2)
	if err != nil {
		return err
	}

	to := config.Mail.To
	if len(watch.AdditionalTo) > 0 {
		to = append(to, watch.AdditionalTo...)
	}

	body = strings.ReplaceAll(body, "\n", "<br>\n")
	body = fmt.Sprintf("%s<br><br>\n%s", body, string(htmlDiff))

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
	r, err := database.ReadDatabase(config.Database)
	if err != nil {
		return err
	}

	// remove old websites in the database on each run
	database.CleanupDatabase(log, r, *config)

	httpClient := http.NewHTTPClient(config.Useragent, config.Timeout.Duration, *debug, log)

	ctx := context.Background()

	for _, watch := range config.Watches {
		log.Debugf("processing %s: %s", watch.Name, watch.URL)
		lastContent, ok := r.Websites[watch.URL]
		// if it's a new website not yet in the database only process new entries and ignore old ones
		if !ok {
			lastContent = nil
		}
		statusCode, body, err := httpClient.GetRequest(ctx, watch.URL)
		if err != nil {
			log.Errorf("[ERROR]: %v", err)
			continue
		}

		if statusCode != 200 || len(body) == 0 {
			// send mail to indicate we might have an error
			subject := fmt.Sprintf("[WEBSITEWATCHER] Invalid response for %s", watch.Name)
			text := fmt.Sprintf("Name: %s\nURL: %s\nStatus: %d\nBodylen: %d", watch.Name, watch.URL, statusCode, len(body))
			if err := sendEmail(config, httpClient, watch, subject, text, string(lastContent), string(body)); err != nil {
				log.Errorf("[ERROR]: %v", err)
			}
			// do not process non 200 responses and save to database
			continue
		}

		r.Websites[watch.URL] = body

		if lastContent == nil {
			// lastContent = nil on new sites not yet processed, so send no email here
			log.Debugf("new website %s %s detected, not comparing", watch.Name, watch.URL)
			continue
		}

		if !bytes.Equal(lastContent, body) {
			if *debug {
				log.Debugf("Website %s %s differ! Would send email in prod", watch.Name, watch.URL)
			} else {
				subject := fmt.Sprintf("[WEBSITEWATCHER] Detected change on %s", watch.Name)
				text := fmt.Sprintf("Name: %s\nURL: %s\nStatus: %d\nBodylen: %d", watch.Name, watch.URL, statusCode, len(body))
				if err := sendEmail(config, httpClient, watch, subject, text, string(lastContent), string(body)); err != nil {
					log.Errorf("[ERROR]: %v", err)
				}
			}
		}
	}
	r.LastRun = start
	err = database.SaveDatabase(config.Database, r)
	if err != nil {
		return err
	}

	return nil
}
