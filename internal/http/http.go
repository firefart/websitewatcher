package http

import (
	"crypto/tls"
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
	tr := http.DefaultTransport.(*http.Transport)
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
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
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	} else {
		req.Header.Set("User-Agent", c.userAgent)
	}
	return c.client.Do(req)
}
