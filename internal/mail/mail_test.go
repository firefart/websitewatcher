package mail // revive:disable:var-naming

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/firefart/websitewatcher/internal/config"
	"github.com/stretchr/testify/require"
)

func newTestMail(t *testing.T) *Mail {
	t.Helper()
	logger := slog.New(slog.DiscardHandler)
	cfg := config.Configuration{
		Useragent: "test-agent",
		Mail: config.MailConfig{
			Server:  "localhost",
			Port:    25,
			From:    config.MailConfigFrom{Name: "Test Sender", Mail: "sender@example.com"},
			To:      []string{"recipient@example.com"},
			Retries: 1,
			Timeout: time.Second,
		},
	}
	m, err := New(cfg, logger)
	require.NoError(t, err)
	return m
}

func TestNew_BasicConfig(t *testing.T) {
	t.Parallel()

	m := newTestMail(t)
	require.NotNil(t, m)
	require.NotNil(t, m.client)
}

func TestNew_TLSOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		tls      bool
		startTLS bool
		skipTLS  bool
	}{
		{name: "plain", tls: false, startTLS: false, skipTLS: false},
		{name: "TLS", tls: true, startTLS: false, skipTLS: false},
		{name: "StartTLS", tls: false, startTLS: true, skipTLS: false},
		{name: "SkipTLS", tls: false, startTLS: false, skipTLS: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			logger := slog.New(slog.DiscardHandler)
			cfg := config.Configuration{
				Useragent: "test-agent",
				Mail: config.MailConfig{
					Server:   "localhost",
					Port:     465,
					From:     config.MailConfigFrom{Name: "Test", Mail: "test@example.com"},
					To:       []string{"to@example.com"},
					Retries:  1,
					Timeout:  time.Second,
					TLS:      tc.tls,
					StartTLS: tc.startTLS,
					SkipTLS:  tc.skipTLS,
				},
			}
			m, err := New(cfg, logger)
			require.NoError(t, err)
			require.NotNil(t, m)
		})
	}
}

func TestNew_WithCredentials(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	cfg := config.Configuration{
		Useragent: "test-agent",
		Mail: config.MailConfig{
			Server:   "smtp.example.com",
			Port:     587,
			From:     config.MailConfigFrom{Name: "Test", Mail: "test@example.com"},
			To:       []string{"to@example.com"},
			User:     "user@example.com",
			Password: "secret",
			Retries:  3,
			Timeout:  10 * time.Second,
		},
	}
	m, err := New(cfg, logger)
	require.NoError(t, err)
	require.NotNil(t, m)
}

func TestSend_EmptyContent(t *testing.T) {
	t.Parallel()

	m := newTestMail(t)
	err := m.send(context.Background(), "to@example.com", "subject", "", "")
	require.Error(t, err)
	require.ErrorContains(t, err, "need a content to send email")
}

func TestSend_TextOnlyDoesNotError(t *testing.T) {
	t.Parallel()

	// The error from send() with non-empty text is an SMTP connection error, not a
	// content-validation error — confirming the content check passes.
	m := newTestMail(t)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := m.send(ctx, "to@example.com", "subject", "text body", "")
	// Should fail at SMTP dial, not at content validation
	require.Error(t, err)
	require.NotContains(t, err.Error(), "need a content to send email")
}

func TestGenerateHTML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "newlines converted to br",
			input:    "line1\nline2\nline3",
			expected: "<html><body>line1<br>\nline2<br>\nline3</body></html>",
		},
		{
			name:     "empty body",
			input:    "",
			expected: "<html><body></body></html>",
		},
		{
			name:     "single line",
			input:    "hello world",
			expected: "<html><body>hello world</body></html>",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := generateHTML(tc.input)
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestFormatHeaders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		headers map[string][]string
		checks  []string
	}{
		{
			name:    "empty headers",
			headers: map[string][]string{},
			checks:  nil,
		},
		{
			name: "single header single value",
			headers: map[string][]string{
				"Content-Type": {"text/html"},
			},
			checks: []string{"Content-Type: text/html"},
		},
		{
			name: "single header multiple values",
			headers: map[string][]string{
				"Accept": {"text/html", "application/json"},
			},
			checks: []string{"Accept: text/html, application/json"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := formatHeaders(tc.headers)
			for _, check := range tc.checks {
				require.Contains(t, result, check)
			}
		})
	}
}
