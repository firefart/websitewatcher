package http

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/firefart/websitewatcher/internal/config"
	"github.com/stretchr/testify/require"
)

func TestNewHTTPClient_WithoutProxy(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	client, err := NewHTTPClient(logger, "test-agent", 10*time.Second, nil)

	require.NoError(t, err)
	require.NotNil(t, client)
	require.Equal(t, "test-agent", client.userAgent)
	require.NotNil(t, client.client)
	require.Equal(t, 10*time.Second, client.client.Timeout)

	// No proxy logs should be present
	require.Empty(t, buf.String())
}

func TestNewHTTPClient_WithProxy(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	proxyConfig := &config.ProxyConfig{
		URL:      "http://proxy.example.com:8080",
		Username: "user",
		Password: "pass",
	}

	client, err := NewHTTPClient(logger, "test-agent", 5*time.Second, proxyConfig)

	require.NoError(t, err)
	require.NotNil(t, client)
	require.Equal(t, "test-agent", client.userAgent)

	// Verify proxy logging
	logOutput := buf.String()
	require.Contains(t, logOutput, "using proxy")
	require.Contains(t, logOutput, "url=http://proxy.example.com:8080")
	require.Contains(t, logOutput, "authenticated=true")
}

func TestNewHTTPClient_WithProxyUnauthenticated(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	proxyConfig := &config.ProxyConfig{
		URL: "http://proxy.example.com:8080",
	}

	client, err := NewHTTPClient(logger, "test-agent", 5*time.Second, proxyConfig)

	require.NoError(t, err)
	require.NotNil(t, client)

	// Verify proxy logging shows unauthenticated
	logOutput := buf.String()
	require.Contains(t, logOutput, "using proxy")
	require.Contains(t, logOutput, "authenticated=false")
}

func TestNewHTTPClient_EmptyProxyURL(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	proxyConfig := &config.ProxyConfig{
		URL: "",
	}

	client, err := NewHTTPClient(logger, "test-agent", 5*time.Second, proxyConfig)

	require.NoError(t, err)
	require.NotNil(t, client)

	// No proxy logs should be present for empty URL
	require.Empty(t, buf.String())
}

func TestNewHTTPClient_InvalidProxy(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	proxyConfig := &config.ProxyConfig{
		URL: "inval$#%^&*(Iid url",
	}

	client, err := NewHTTPClient(logger, "test-agent", 5*time.Second, proxyConfig)

	require.Error(t, err)
	require.Nil(t, client)
	require.Contains(t, err.Error(), "failed to create proxy")
}

func TestClient_Do_UserAgentPrecedence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		clientUserAgent   string
		requestUserAgent  string
		expectedUserAgent string
	}{
		{
			name:              "request user agent takes precedence",
			clientUserAgent:   "client-agent",
			requestUserAgent:  "request-agent",
			expectedUserAgent: "request-agent",
		},
		{
			name:              "client user agent when no request agent",
			clientUserAgent:   "client-agent",
			requestUserAgent:  "",
			expectedUserAgent: "client-agent",
		},
		{
			name:              "default user agent when neither set",
			clientUserAgent:   "",
			requestUserAgent:  "",
			expectedUserAgent: config.DefaultUseragent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotUserAgent := ""
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotUserAgent = r.Header.Get("User-Agent")
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			logger := slog.New(slog.DiscardHandler)
			client, err := NewHTTPClient(logger, tt.clientUserAgent, 10*time.Second, nil)
			require.NoError(t, err)

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
			require.NoError(t, err)

			resp, err := client.Do(req, tt.requestUserAgent)
			require.NoError(t, err)
			defer resp.Body.Close()

			require.Equal(t, http.StatusOK, resp.StatusCode)
			require.Equal(t, tt.expectedUserAgent, gotUserAgent)
		})
	}
}

func TestClient_Do_Integration(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Hello, World!"))
	}))
	defer server.Close()

	logger := slog.New(slog.DiscardHandler)
	client, err := NewHTTPClient(logger, "test-agent", 10*time.Second, nil)
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	resp, err := client.Do(req, "")
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "text/plain", resp.Header.Get("Content-Type"))
}

func TestClient_TLSConfiguration(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	client, err := NewHTTPClient(logger, "test-agent", 10*time.Second, nil)
	require.NoError(t, err)

	transport, ok := client.client.Transport.(*http.Transport)
	require.True(t, ok)
	require.NotNil(t, transport.TLSClientConfig)
	require.True(t, transport.TLSClientConfig.InsecureSkipVerify)
}

func TestClient_Timeout(t *testing.T) {
	t.Parallel()

	expectedTimeout := 2 * time.Second
	logger := slog.New(slog.DiscardHandler)
	client, err := NewHTTPClient(logger, "test-agent", expectedTimeout, nil)
	require.NoError(t, err)

	require.Equal(t, expectedTimeout, client.client.Timeout)
}

func captureLogOutput(fn func(*slog.Logger)) string {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	fn(logger)
	return buf.String()
}

func TestLogger_ProxyConfigurationMessages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		proxy     *config.ProxyConfig
		wantLog   bool
		wantInLog []string
	}{
		{
			name:      "no proxy config",
			proxy:     nil,
			wantLog:   false,
			wantInLog: nil,
		},
		{
			name: "authenticated proxy",
			proxy: &config.ProxyConfig{
				URL:      "http://example.com:8080",
				Username: "user",
				Password: "pass",
			},
			wantLog: true,
			wantInLog: []string{
				"using proxy",
				"url=http://example.com:8080",
				"authenticated=true",
			},
		},
		{
			name: "unauthenticated proxy",
			proxy: &config.ProxyConfig{
				URL: "http://example.com:8080",
			},
			wantLog: true,
			wantInLog: []string{
				"using proxy",
				"url=http://example.com:8080",
				"authenticated=false",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			logOutput := captureLogOutput(func(logger *slog.Logger) {
				_, _ = NewHTTPClient(logger, "test", time.Second, tt.proxy)
			})

			if tt.wantLog {
				for _, want := range tt.wantInLog {
					require.Contains(t, logOutput, want)
				}
			} else {
				require.Empty(t, strings.TrimSpace(logOutput))
			}
		})
	}
}
