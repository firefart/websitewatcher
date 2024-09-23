package http

import (
	"crypto/tls"
	"net/http"
	"time"
)

type Client struct {
	userAgent string
	client    *http.Client
}

func NewHTTPClient(userAgent string, timeout time.Duration) *Client {
	// use default transport so proxy is respected
	tr := http.DefaultTransport.(*http.Transport)
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	httpClient := http.Client{
		Timeout:   timeout,
		Transport: tr,
	}
	return &Client{
		userAgent: userAgent,
		client:    &httpClient,
	}
}

func (c *Client) Do(req *http.Request, userAgent string) (*http.Response, error) {
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	} else {
		req.Header.Set("User-Agent", c.userAgent)
	}
	return c.client.Do(req)
}
