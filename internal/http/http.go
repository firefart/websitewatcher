package http

import (
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/firefart/websitewatcher/internal/config"
)

type Client struct {
	userAgent string
	client    *http.Client
}

func NewHTTPClient(logger *slog.Logger, userAgent string, timeout time.Duration, proxyConfig *config.ProxyConfig) (*Client, error) {
	// use default transport so proxy is respected
	tr, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("failed to cast default transport to http.Transport")
	}
	tr = tr.Clone()
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // nolint:gosec
	if proxyConfig != nil && proxyConfig.URL != "" {
		authenticated := proxyConfig.Username != "" && proxyConfig.Password != ""
		logger.Info("using proxy", slog.String("url", proxyConfig.URL), slog.Bool("authenticated", authenticated))
		proxy, err := newProxy(*proxyConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create proxy: %w", err)
		}
		tr.Proxy = proxy.ProxyFromConfig
	}
	httpClient := http.Client{
		Timeout:   timeout,
		Transport: tr,
	}
	return &Client{
		userAgent: userAgent,
		client:    &httpClient,
	}, nil
}

func (c *Client) Do(req *http.Request, userAgent string) (*http.Response, error) {
	switch {
	case userAgent != "":
		req.Header.Set("User-Agent", userAgent)
	case c.userAgent != "":
		req.Header.Set("User-Agent", c.userAgent)
	default:
		req.Header.Set("User-Agent", config.DefaultUseragent)
	}
	return c.client.Do(req)
}
