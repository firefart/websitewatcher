package http

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"time"

	"github.com/sirupsen/logrus"
)

type HTTPClient struct {
	userAgent string
	client    *http.Client
	debug     bool
	logger    *logrus.Logger
}

func NewHTTPClient(userAgent string, timeout time.Duration, debug bool, logger *logrus.Logger) *HTTPClient {
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
		debug:     debug,
		logger:    logger,
	}
}

func (c *HTTPClient) Do(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", c.userAgent)
	return c.client.Do(req)
}

func (c *HTTPClient) GetRequest(ctx context.Context, url string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return -1, nil, fmt.Errorf("could create get request for %s: %w", url, err)
	}

	resp, err := c.Do(req)
	if err != nil {
		return -1, nil, fmt.Errorf("could not get %s: %w", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return -1, nil, fmt.Errorf("could not read body from %s: %w", url, err)
	}

	if c.debug {
		reqDump, err := httputil.DumpRequestOut(req, true)
		if err != nil {
			return -1, nil, fmt.Errorf("error on req debug dump: %w", err)
		}
		respDump, err := httputil.DumpResponse(resp, false)
		if err != nil {
			return -1, nil, fmt.Errorf("error on resp debug dump: %w", err)
		}
		c.logger.Debugf("Request:\n%s", string(reqDump))
		c.logger.Debugf("Response:\n%s", string(respDump))
	}

	return resp.StatusCode, body, nil
}
