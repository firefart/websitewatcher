package http

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/firefart/websitewatcher/internal/logger"
)

type HTTPClient struct {
	userAgent  string
	retries    int
	retryDelay time.Duration
	client     *http.Client
	logger     logger.Logger
}

type InvalidResponseError struct {
	StatusCode      int
	Header          map[string][]string
	Body            []byte
	RequestDuration time.Duration
}

func (err *InvalidResponseError) Error() string {
	return fmt.Sprintf("got invalid response on http request: status: %d, bodylen: %d", err.StatusCode, len(err.Body))
}

func NewHTTPClient(userAgent string, retries int, retryDelay time.Duration, timeout time.Duration, logger logger.Logger) *HTTPClient {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	httpClient := http.Client{
		Timeout:   timeout,
		Transport: tr,
	}
	return &HTTPClient{
		userAgent:  userAgent,
		retries:    retries,
		retryDelay: retryDelay,
		client:     &httpClient,
		logger:     logger,
	}
}

func (c *HTTPClient) Do(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", c.userAgent)
	return c.client.Do(req)
}

func (c *HTTPClient) fetchURL(ctx context.Context, url string) (int, map[string][]string, []byte, error) {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return -1, nil, nil, fmt.Errorf("could create get request for %s: %w", url, err)
	}

	resp, err := c.Do(req)
	if err != nil {
		return -1, nil, nil, fmt.Errorf("could not get %s: %w", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return -1, nil, nil, fmt.Errorf("could not read body from %s: %w", url, err)
	}

	if resp.StatusCode != 200 || len(body) == 0 || isSoftError(body) {
		duration := time.Since(start)
		return -1, nil, nil, &InvalidResponseError{
			StatusCode:      resp.StatusCode,
			Header:          resp.Header,
			Body:            body,
			RequestDuration: duration,
		}
	}

	return resp.StatusCode, resp.Header, body, nil
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

func (c *HTTPClient) GetRequest(ctx context.Context, url string) (int, map[string][]string, []byte, error) {
	var statusCode int
	var body []byte
	var header map[string][]string
	var err error
	// check with retries
	for i := 1; i <= c.retries; i++ {
		c.logger.Debugf("try #%d for %s", i, url)
		statusCode, header, body, err = c.fetchURL(ctx, url)
		if err == nil {
			// break out on success
			break
		}

		// if we reach here, we have an error, retry
		if i == c.retries {
			// break out to not print the rety message on the last try
			break
		}

		if c.retryDelay > 0 {
			c.logger.Error(fmt.Errorf("got error on try #%d for %s, retrying after %s: %w", i, url, c.retryDelay, err))
			select {
			case <-ctx.Done():
				return -1, nil, nil, ctx.Err()
			case <-time.After(c.retryDelay):
			}
		} else {
			c.logger.Error(fmt.Errorf("got error on try #%d for %s, retrying: %w", i, url, err))
		}
	}

	// last error still set, bail out
	if err != nil {
		return -1, nil, nil, err
	}

	return statusCode, header, body, nil
}
