package mail

import (
	"crypto/tls"
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/firefart/websitewatcher/internal/config"
	"github.com/firefart/websitewatcher/internal/diff"
	"github.com/firefart/websitewatcher/internal/http"
	"github.com/firefart/websitewatcher/internal/logger"
	"github.com/firefart/websitewatcher/internal/watch"
	gomail "gopkg.in/mail.v2"
)

type Mail struct {
	config     config.Configuration
	dialer     *gomail.Dialer
	httpClient *http.HTTPClient
	logger     logger.Logger
}

func New(config config.Configuration, httpClient *http.HTTPClient, logger logger.Logger) *Mail {
	d := gomail.NewDialer(config.Mail.Server, config.Mail.Port, config.Mail.User, config.Mail.Password)
	if config.Mail.SkipTLS {
		d.TLSConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &Mail{
		config:     config,
		dialer:     d,
		httpClient: httpClient,
		logger:     logger,
	}
}

func (m *Mail) SendErrorEmail(w watch.Watch, err error) error {
	subject := fmt.Sprintf("[ERROR] error in websitewatcher on %s", w.Name)
	body := fmt.Sprintf("%s", err)
	for _, to := range m.config.Mail.To {
		if err := m.send(to, subject, body, "text/plain"); err != nil {
			return err
		}
	}
	return nil
}

func (m *Mail) SendDiffEmail(w watch.Watch, diffMethod, subject, body, text1, text2 string) error {
	content := ""
	switch diffMethod {
	case "api":
		htmlContent, err := diff.GenerateHTMLDiffAPI(m.httpClient, body, text1, text2)
		if err != nil {
			return err
		}
		content = htmlContent
	case "internal":
		htmlContent, err := diff.GenerateHTMLDiffInternal(body, text1, text2)
		if err != nil {
			return err
		}
		content = htmlContent
	case "local":
		htmlContent, err := diff.GenerateHTMLDiffLocal(body, text1, text2)
		if err != nil {
			return err
		}
		content = htmlContent
	default:
		return fmt.Errorf("invalid diff method %s", diffMethod)
	}
	m.logger.Debugf("Mail Content: %s", content)
	return m.sendHTMLEmail(w, subject, content)
}

func (m *Mail) SendWatchError(w watch.Watch, ret *watch.InvalidResponseError) error {
	subject := fmt.Sprintf("Invalid response for %s", w.Name)

	var sb strings.Builder
	if _, err := sb.WriteString(fmt.Sprintf("%s\n\n", html.EscapeString(ret.ErrorMessage))); err != nil {
		return err
	}

	if _, err := sb.WriteString(fmt.Sprintf("Name: %s\n", html.EscapeString(w.Name))); err != nil {
		return err
	}
	if _, err := sb.WriteString(fmt.Sprintf("URL: %s\n", html.EscapeString(w.URL))); err != nil {
		return err
	}
	if _, err := sb.WriteString(fmt.Sprintf("Status: %d\n", ret.StatusCode)); err != nil {
		return err
	}
	if _, err := sb.WriteString(fmt.Sprintf("Bodylen: %d\n", len(ret.Body))); err != nil {
		return err
	}
	if _, err := sb.WriteString(fmt.Sprintf("Request Duration: %s\n", ret.Duration.Round(time.Millisecond))); err != nil {
		return err
	}
	if _, err := sb.WriteString(fmt.Sprintf("Header:\n%s\n", html.EscapeString(formatHeaders(ret.Header)))); err != nil {
		return err
	}
	if _, err := sb.WriteString(fmt.Sprintf("Body:\n%s\n", html.EscapeString(string(ret.Body)))); err != nil {
		return err
	}

	text := sb.String()
	htmlContent := generateHTML(text)
	if err := m.sendHTMLEmail(w, subject, htmlContent); err != nil {
		return fmt.Errorf("error on sending email: %w", err)
	}

	return nil
}

func (m *Mail) sendHTMLEmail(w watch.Watch, subject, htmlBody string) error {
	tos := m.config.Mail.To
	if len(w.AdditionalTo) > 0 {
		tos = append(tos, w.AdditionalTo...)
	}

	for _, to := range tos {
		if err := m.send(to, fmt.Sprintf("[WEBSITEWATCHER] %s", subject), htmlBody, "text/html"); err != nil {
			return err
		}
	}

	return nil
}

func (m *Mail) send(to string, subject, body, contentType string) error {
	msg := gomail.NewMessage()
	msg.SetAddressHeader("From", m.config.Mail.From.Mail, m.config.Mail.From.Name)
	msg.SetHeader("To", to)
	msg.SetHeader("Subject", subject)
	msg.SetBody(contentType, body)

	var err error
	for i := 1; i <= m.config.Mail.Retries; i++ {
		err = m.dialer.DialAndSend(msg)
		if err == nil {
			return nil
		}
		m.logger.Errorf("error on sending email %q on try %d: %v", subject, i, err)
	}
	return fmt.Errorf("could not send mail %q after %d retries. Last error: %w", subject, m.config.Mail.Retries, err)
}

func formatHeaders(header map[string][]string) string {
	var sb strings.Builder
	for key, value := range header {
		sb.WriteString(fmt.Sprintf("%s: %s\n", key, strings.Join(value, ", ")))
	}
	return sb.String()
}

func generateHTML(body string) string {
	body = strings.ReplaceAll(body, "\n", "<br>\n")
	body = fmt.Sprintf("<html><body>%s</body></html>", body)
	return body
}
