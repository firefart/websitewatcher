package main

import (
	"bufio"
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/firefart/websitewatcher/internal/config"
	httpint "github.com/firefart/websitewatcher/internal/http"
	"github.com/firefart/websitewatcher/internal/watch"
	"github.com/stretchr/testify/require"
)

// fakeDB is a minimal database.Interface implementation that lets tests observe
// whether UpdateLastContent was called without needing a real SQLite database.
type fakeDB struct {
	watchID     int64
	lastFetch   time.Time
	lastContent []byte

	updateLastContentCalled bool
	updatedID               int64
	updatedContent          []byte
}

func (f *fakeDB) Close(time.Duration) error { return nil }

func (f *fakeDB) GetLastContent(context.Context, string, string) (int64, time.Time, []byte, error) {
	return f.watchID, f.lastFetch, f.lastContent, nil
}

func (f *fakeDB) InsertWatch(context.Context, string, string, []byte) (int64, error) {
	return f.watchID, nil
}

func (f *fakeDB) UpdateLastContent(_ context.Context, id int64, content []byte) error {
	f.updateLastContentCalled = true
	f.updatedID = id
	f.updatedContent = content
	return nil
}

func (f *fakeDB) PrepareDatabase(context.Context, config.Configuration) ([]config.WatchConfig, int, error) {
	return nil, 0, nil
}

// startFakeSMTPServer starts a minimal SMTP server that accepts any message without
// authentication or TLS, so mail.Mail.send() can successfully deliver a message in tests.
func startFakeSMTPServer(t *testing.T) (string, int) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleFakeSMTPConn(conn)
		}
	}()

	addr, ok := ln.Addr().(*net.TCPAddr)
	require.True(t, ok)
	return addr.IP.String(), addr.Port
}

func handleFakeSMTPConn(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	write := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }

	write("220 localhost ESMTP fake server ready")

	inData := false
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")

		if inData {
			// textproto's DotWriter terminates the message with a lone "." line
			if line == "." {
				inData = false
				write("250 OK: message accepted")
			}
			continue
		}

		switch upper := strings.ToUpper(line); {
		case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
			write("250 localhost greets you")
		case strings.HasPrefix(upper, "MAIL FROM"):
			write("250 OK")
		case strings.HasPrefix(upper, "RCPT TO"):
			write("250 OK")
		case upper == "DATA":
			write("354 Start mail input; end with <CRLF>.<CRLF>")
			inData = true
		case upper == "QUIT":
			write("221 Bye")
			return
		default:
			// covers NOOP and anything else the client checks for
			write("250 OK")
		}
	}
}

// TestProcessWatch_WebhookFailureDoesNotBlockDatabaseUpdate verifies the fix for the bug where a
// failing webhook (after an already successful diff email) aborted processWatch before the
// database was updated. That caused the same change to be re-detected - and the already
// delivered diff email re-sent - on every subsequent run until the webhook started succeeding.
// It also verifies that a failing webhook no longer prevents other webhooks from being sent.
func TestProcessWatch_WebhookFailureDoesNotBlockDatabaseUpdate(t *testing.T) {
	t.Parallel()

	watchServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("new content"))
	}))
	defer watchServer.Close()

	var okHits, failHits int32
	okWebhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&okHits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer okWebhook.Close()

	failWebhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&failHits, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failWebhook.Close()

	smtpHost, smtpPort := startFakeSMTPServer(t)

	logger := slog.New(slog.DiscardHandler)
	httpClient, err := httpint.NewHTTPClient(logger, "test-agent", 5*time.Second, nil, false)
	require.NoError(t, err)

	w := watch.New(config.WatchConfig{
		Name: "test-watch",
		URL:  watchServer.URL,
		Webhooks: []config.WebhookConfig{
			{URL: okWebhook.URL, Method: http.MethodGet},
			{URL: failWebhook.URL, Method: http.MethodGet},
		},
	}, logger, httpClient)

	db := &fakeDB{
		watchID:     42,
		lastFetch:   time.Now().Add(-time.Hour),
		lastContent: []byte("old content"),
	}

	app := &app{
		logger: logger,
		config: config.Configuration{
			Useragent: "test-agent",
			Retry: config.RetryConfig{
				Count: 1,
				Delay: time.Second,
			},
			Mail: config.MailConfig{
				Server:  smtpHost,
				Port:    smtpPort,
				From:    config.MailConfigFrom{Name: "Test", Mail: "test@example.com"},
				To:      []string{"to@example.com"},
				Retries: 1,
				Timeout: 5 * time.Second,
			},
		},
		db:       db,
		timezone: time.UTC,
	}

	err = app.processWatch(t.Context(), w)
	require.Error(t, err)
	require.Contains(t, err.Error(), "could not send webhooks")

	// the crux of the fix: the database must be updated even though a webhook failed,
	// since the (primary) email notification already went out successfully
	require.True(t, db.updateLastContentCalled)
	require.Equal(t, db.watchID, db.updatedID)
	require.Equal(t, []byte("new content"), db.updatedContent)

	// both webhooks must be attempted, even though the first one in the list could fail
	require.Equal(t, int32(1), atomic.LoadInt32(&okHits))
	require.Equal(t, int32(1), atomic.LoadInt32(&failHits))
}
