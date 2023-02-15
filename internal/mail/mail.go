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
	"github.com/firefart/websitewatcher/internal/watch"
	gomail "gopkg.in/mail.v2"
)

type Mail struct {
	config     *config.Configuration
	dialer     *gomail.Dialer
	httpClient *http.HTTPClient
}

func New(config *config.Configuration, httpClient *http.HTTPClient) *Mail {
	d := gomail.NewDialer(config.Mail.Server, config.Mail.Port, config.Mail.User, config.Mail.Password)
	if config.Mail.SkipTLS {
		d.TLSConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &Mail{
		config:     config,
		dialer:     d,
		httpClient: httpClient,
	}
}

func (m *Mail) SendErrorEmail(w watch.Watch, err error) error {
	subject := fmt.Sprintf("[ERROR] error in websitewatcher on %s", w.Name)
	body := fmt.Sprintf("%#v", err)
	return m.send(m.config.Mail.To, subject, body, "text/plain")
}

func (m *Mail) SendDiffEmail(w watch.Watch, subject, body, text1, text2 string) error {
	htmlContent, err := m.generateHTMLDiff(body, text1, text2)
	if err != nil {
		return err
	}
	return m.sendHTMLEmail(w, subject, htmlContent)
}

func (m *Mail) SendWatchError(w watch.Watch, ret *watch.InvalidResponseError) error {
	subject := fmt.Sprintf("Invalid response for %s", w.Name)

	var sb strings.Builder
	if _, err := sb.WriteString(fmt.Sprintf("Name: %s\n", w.Name)); err != nil {
		return err
	}
	if _, err := sb.WriteString(fmt.Sprintf("URL: %s\n", w.URL)); err != nil {
		return err
	}

	if _, err := sb.WriteString(fmt.Sprintf("Request Duration: %s\n", ret.Duration.Round(time.Millisecond))); err != nil {
		return err
	}

	if _, err := sb.WriteString(fmt.Sprintf("Status: %d\n", ret.StatusCode)); err != nil {
		return err
	}
	if _, err := sb.WriteString(fmt.Sprintf("Bodylen: %d\n", len(ret.Body))); err != nil {
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
	to := m.config.Mail.To
	if len(w.AdditionalTo) > 0 {
		to = append(to, w.AdditionalTo...)
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

func (m *Mail) generateHTMLDiff(body string, text1, text2 string) (string, error) {
	diffCSS, diffHTML, err := diff.DiffAPI(m.httpClient, text1, text2)
	if err != nil {
		return "", err
	}
	body = strings.ReplaceAll(body, "\n", "<br>\n")
	body = fmt.Sprintf("<html><head><style>%s</style></head><body>%s<br><br>\n%s</body></html>", diffCSS, body, diffHTML)
	return body, nil
}
