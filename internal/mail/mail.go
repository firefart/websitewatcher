package mail

import (
	"crypto/tls"
	"fmt"

	"github.com/firefart/websitewatcher/internal/config"
	gomail "gopkg.in/mail.v2"
)

type Mail struct {
	config *config.Configuration
	dialer *gomail.Dialer
}

func NewMail(config *config.Configuration) *Mail {
	d := gomail.NewDialer(config.Mail.Server, config.Mail.Port, config.Mail.User, config.Mail.Password)
	if config.Mail.SkipTLS {
		d.TLSConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &Mail{
		config: config,
		dialer: d,
	}
}

func (m *Mail) SendErrorEmail(watch config.Watch, err error) error {
	subject := fmt.Sprintf("[ERROR] error in websitewatcher on %s", watch.Name)
	body := fmt.Sprintf("%#v", err)
	return m.send(m.config.Mail.To, subject, body, "text/plain")
}

func (m *Mail) SendTextEmail(watch config.Watch, subject, body string) error {
	to := m.config.Mail.To
	if len(watch.AdditionalTo) > 0 {
		to = append(to, watch.AdditionalTo...)
	}

	return m.send(to, fmt.Sprintf("[WEBSITEWATCHER] %s", subject), body, "text/plain")
}

func (m *Mail) SendHTMLEmail(watch config.Watch, subject, htmlBody string) error {
	to := m.config.Mail.To
	if len(watch.AdditionalTo) > 0 {
		to = append(to, watch.AdditionalTo...)
	}

	return m.send(to, fmt.Sprintf("[WEBSITEWATCHER] %s", subject), htmlBody, "text/html")
}

func (m *Mail) send(to []string, subject, body, contentType string) error {
	msg := gomail.NewMessage()
	msg.SetAddressHeader("From", m.config.Mail.From.Mail, m.config.Mail.From.Name)
	msg.SetHeader("To", to...)
	msg.SetHeader("Subject", subject)
	msg.SetBody(contentType, body)

	if err := m.dialer.DialAndSend(msg); err != nil {
		return err
	}
	return nil
}
