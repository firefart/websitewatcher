package http

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/firefart/websitewatcher/internal/config"
)

type HTTPClient struct {
	userAgent string
	client    *http.Client
}

type InvalidResponseError struct {
	StatusCode int
	Header     map[string][]string
	Body       []byte
}

func (err *InvalidResponseError) Error() string {
	return fmt.Sprintf("got invalid response on http request: status: %d, bodylen: %d", err.StatusCode, len(err.Body))
}

func NewHTTPClient(userAgent string, timeout time.Duration) *HTTPClient {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	httpClient := http.Client{
		Timeout:   timeout,
		Transport: tr,
	}
	return &HTTPClient{
		userAgent: userAgent,
		client:    &httpClient,
	}
}

func (c *HTTPClient) Do(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", c.userAgent)
	return c.client.Do(req)
}

func (c *HTTPClient) CheckWatch(ctx context.Context, watch config.Watch) (int, map[string][]string, time.Duration, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, watch.URL, nil)
	if err != nil {
		return -1, nil, -1, nil, fmt.Errorf("could create get request for %s: %w", watch.URL, err)
	}

	for name, value := range watch.Header {
		req.Header.Set(name, value)
	}

	start := time.Now()
	resp, err := c.Do(req)
	if err != nil {
		return -1, nil, -1, nil, fmt.Errorf("could not get %s: %w", watch.URL, err)
	}
	defer resp.Body.Close()
	duration := time.Since(start)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return -1, nil, -1, nil, fmt.Errorf("could not read body from %s: %w", watch.URL, err)
	}

	if resp.StatusCode != 200 || len(body) == 0 || isSoftError(body) {
		return -1, nil, duration, nil, &InvalidResponseError{
			StatusCode: resp.StatusCode,
			Header:     resp.Header,
			Body:       body,
		}
	}

	return resp.StatusCode, resp.Header, duration, body, nil
}

func isSoftError(body []byte) bool {
	if len(body) == 0 {
		return false
	}

	if bytes.Contains(body, []byte("504 - Gateway Time-out")) ||
		bytes.Contains(body, []byte("404 - Not Found")) ||
		bytes.Contains(body, []byte("503 - Service Unavailable")) {
		return true
	}

	return false
}
