package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"strings"
	"time"

	"github.com/firefart/websitewatcher/internal/config"
	"github.com/firefart/websitewatcher/internal/diff"
	"github.com/firefart/websitewatcher/internal/http"
	"github.com/firefart/websitewatcher/internal/watch"

	gomail "github.com/wneessen/go-mail"
)

type Mail struct {
	config     config.Configuration
	httpClient *http.Client
	logger     *slog.Logger
}

func New(config config.Configuration, httpClient *http.Client, logger *slog.Logger) *Mail {
	return &Mail{
		config:     config,
		httpClient: httpClient,
		logger:     logger,
	}
}

// gomail.Client is NOT threadsafe
// https://github.com/wneessen/go-mail/discussions/268
// so we need to create a new client each time :/
func (m *Mail) newClient() (*gomail.Client, error) {
	var options []gomail.Option

	options = append(options, gomail.WithTimeout(m.config.Mail.Timeout))
	options = append(options, gomail.WithPort(m.config.Mail.Port))
	if m.config.Mail.User != "" && m.config.Mail.Password != "" {
		options = append(options, gomail.WithSMTPAuth(gomail.SMTPAuthPlain))
		options = append(options, gomail.WithUsername(m.config.Mail.User))
		options = append(options, gomail.WithPassword(m.config.Mail.Password))
	}
	if m.config.Mail.SkipTLS {
		options = append(options, gomail.WithTLSConfig(&tls.Config{
			InsecureSkipVerify: true,
		}))
	}

	// use either tls, starttls, or starttls with fallback to plaintext
	if m.config.Mail.TLS {
		options = append(options, gomail.WithSSL())
	} else if m.config.Mail.StartTLS {
		options = append(options, gomail.WithTLSPolicy(gomail.TLSMandatory))
	} else {
		options = append(options, gomail.WithTLSPolicy(gomail.TLSOpportunistic))
	}

	mailer, err := gomail.NewClient(m.config.Mail.Server, options...)
	if err != nil {
		return nil, fmt.Errorf("could not create mail client: %w", err)
	}

	return mailer, nil
}

func (m *Mail) SendErrorEmail(ctx context.Context, w watch.Watch, err error) error {
	subject := fmt.Sprintf("[ERROR] error in websitewatcher on %s", w.Name)
	body := fmt.Sprintf("%s", err)
	for _, to := range m.config.Mail.To {
		if err := m.send(ctx, to, subject, body, ""); err != nil {
			return err
		}
	}
	return nil
}

func (m *Mail) SendDiffEmail(ctx context.Context, w watch.Watch, diffMethod, subject, body, text1, text2 string) error {
	htmlContent := ""
	textContent := ""
	var err error
	switch diffMethod {
	case "api":
		htmlContent, err = diff.GenerateHTMLDiffAPI(ctx, m.httpClient, body, text1, text2)
		if err != nil {
			return err
		}
	case "internal":
		htmlContent, err = diff.GenerateHTMLDiffInternal(body, text1, text2)
		if err != nil {
			return err
		}
	case "git":
		textContent, htmlContent, err = diff.GenerateDiffGit(ctx, body, text1, text2)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("invalid diff method %s", diffMethod)
	}
	return m.sendMultipartEmail(ctx, w, subject, textContent, htmlContent)
}

func (m *Mail) SendWatchError(ctx context.Context, w watch.Watch, ret *watch.InvalidResponseError) error {
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
	if err := m.sendHTMLEmail(ctx, w, subject, htmlContent); err != nil {
		return fmt.Errorf("error on sending email: %w", err)
	}

	return nil
}

func (m *Mail) sendHTMLEmail(ctx context.Context, w watch.Watch, subject, htmlBody string) error {
	tos := m.config.Mail.To
	if len(w.AdditionalTo) > 0 {
		tos = append(tos, w.AdditionalTo...)
	}

	for _, to := range tos {
		if err := m.send(ctx, to, fmt.Sprintf("[WEBSITEWATCHER] %s", subject), "", htmlBody); err != nil {
			return err
		}
	}

	return nil
}

func (m *Mail) sendMultipartEmail(ctx context.Context, w watch.Watch, subject, textBody, htmlBody string) error {
	tos := m.config.Mail.To
	if len(w.AdditionalTo) > 0 {
		tos = append(tos, w.AdditionalTo...)
	}

	for _, to := range tos {
		if err := m.send(ctx, to, fmt.Sprintf("[WEBSITEWATCHER] %s", subject), textBody, htmlBody); err != nil {
			return err
		}
	}

	return nil
}

func (m *Mail) send(ctx context.Context, to string, subject, textContent, htmlContent string) error {
	if textContent == "" && htmlContent == "" {
		return fmt.Errorf("need a content to send email")
	}

	m.logger.Debug("sending email", slog.String("subject", subject), slog.String("to", to), slog.String("content-text", textContent), slog.String("html-content", htmlContent))

	mailer, err := m.newClient()
	if err != nil {
		return err
	}
	msg := gomail.NewMsg(gomail.WithNoDefaultUserAgent())
	msg.SetUserAgent("websitewatcher / https://github.com/firefart/websitewatcher")
	if err := msg.FromFormat(m.config.Mail.From.Name, m.config.Mail.From.Mail); err != nil {
		return err
	}
	if err := msg.To(to); err != nil {
		return err
	}
	msg.Subject(subject)
	if textContent != "" {
		msg.SetBodyString(gomail.TypeTextPlain, textContent)
	}
	if htmlContent != "" {
		msg.SetBodyString(gomail.TypeTextHTML, htmlContent)
	}

	for i := 1; i <= m.config.Mail.Retries; i++ {
		err = mailer.DialAndSendWithContext(ctx, msg)
		if err == nil {
			return nil
		}
		// bail out on cancel
		if errors.Is(err, context.Canceled) {
			return err
		}
		m.logger.Error("error on sending email", slog.String("subject", subject), slog.Int("try", i), slog.String("err", err.Error()))
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
