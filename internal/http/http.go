package http

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"time"
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
