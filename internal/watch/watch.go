package watch

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/firefart/websitewatcher/internal/config"
	httpint "github.com/firefart/websitewatcher/internal/http"
	"github.com/firefart/websitewatcher/internal/logger"
)

type Watch struct {
	httpClient *httpint.HTTPClient
	logger     logger.Logger

	Name                         string
	URL                          string
	Method                       string
	Body                         string
	Header                       map[string]string
	AdditionalTo                 []string
	AdditionalHTTPErrorsToIgnore []int
	AdditionalSoftErrorPatterns  []string
	Disabled                     bool
	Pattern                      string
	Replaces                     []Replace
	RetryOnMatch                 string
}

type Replace struct {
	Pattern     string
	ReplaceWith string
}

func New(c config.WatchConfig, logger logger.Logger, httpClient *httpint.HTTPClient) Watch {
	w := Watch{
		logger:                       logger,
		httpClient:                   httpClient,
		Name:                         c.Name,
		URL:                          c.URL,
		Method:                       c.Method,
		Body:                         c.Body,
		Header:                       c.Header,
		AdditionalTo:                 c.AdditionalTo,
		AdditionalHTTPErrorsToIgnore: c.AdditionalHTTPErrorsToIgnore,
		AdditionalSoftErrorPatterns:  c.AdditionalSoftErrorPatterns,
		Disabled:                     c.Disabled,
		Pattern:                      c.Pattern,
		Replaces:                     make([]Replace, len(c.Replaces)),
		RetryOnMatch:                 c.RetryOnMatch,
	}
	for i, x := range c.Replaces {
		r := Replace{
			Pattern:     x.Pattern,
			ReplaceWith: x.ReplaceWith,
		}
		w.Replaces[i] = r
	}
	return w
}

// CheckWatchWithRetries runs http.CheckWatch in a loop up to x times (configurable) to retry requests on errors
// it returns the same values as http.CheckWatch
// if the last request still results in an error the error is returned
func (w Watch) CheckWithRetries(ctx context.Context, retries int, retryDelay time.Duration) (int, map[string][]string, time.Duration, []byte, error) {
	var statusCode int
	var requestDuration time.Duration
	var body []byte
	var header map[string][]string
	var err error
	// check with retries
	for i := 1; i <= retries; i++ {
		w.logger.Infof("try #%d for %s", i, w.URL)
		statusCode, header, requestDuration, body, err = w.Check(ctx)
		if err == nil {
			// first check if our body matches the retry pattern and retry
			if w.RetryOnMatch != "" {
				re, err := regexp.Compile(w.RetryOnMatch)
				if err != nil {
					return -1, nil, -1, nil, fmt.Errorf("could not compile pattern %s: %w", w.Pattern, err)
				}
				if re.Match(body) {
					// retry the request as the body matches
					w.logger.Infof("retrying %s because body matches retry pattern", w.URL)
					continue
				}
			}

			// no error and no retry pattern matched, so we count it as success --> break out
			break
		}

		if i >= retries {
			// break out to not print the rety message on the last try
			break
		}

		// if we reach here, we have an error, retry
		if retryDelay > 0 {
			w.logger.Error(fmt.Errorf("got error on try #%d for %s, retrying after %s: %w", i, w.URL, retryDelay, err))
			select {
			case <-ctx.Done():
				return -1, nil, -1, nil, ctx.Err()
			case <-time.After(retryDelay):
			}
		} else {
			w.logger.Error(fmt.Errorf("got error on try #%d for %s, retrying: %w", i, w.URL, err))
		}
	}

	// last error still set, bail out
	if err != nil {
		return -1, nil, -1, nil, err
	}

	return statusCode, header, requestDuration, body, nil
}

// CheckWatch checks a watch.URL and returns the status code, the response headers, the request duration,
// the body and an optional error
// If the request hits an soft error by matching the response body a InvalidResponseError is returned
func (w Watch) Check(ctx context.Context) (int, map[string][]string, time.Duration, []byte, error) {
	method := http.MethodGet
	if w.Method != "" {
		method = strings.ToUpper(w.Method)
	}

	var requestBody io.Reader
	if w.Body != "" {
		requestBody = strings.NewReader(w.Body)
	}

	req, err := http.NewRequestWithContext(ctx, method, w.URL, requestBody)
	if err != nil {
		return -1, nil, -1, nil, fmt.Errorf("could create get request for %s: %w", w.URL, err)
	}

	for name, value := range w.Header {
		req.Header.Set(name, value)
	}

	start := time.Now()
	resp, err := w.httpClient.Do(req)
	if err != nil {
		return -1, nil, -1, nil, fmt.Errorf("could not get %s: %w", w.URL, err)
	}
	defer resp.Body.Close()
	duration := time.Since(start)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return -1, nil, -1, nil, fmt.Errorf("could not read body from %s: %w", w.URL, err)
	}

	softError, err := isSoftError(body, w)
	if err != nil {
		return -1, nil, -1, nil, fmt.Errorf("could not check for soft error on %s: %w", w.URL, err)
	}

	if resp.StatusCode != 200 || len(body) == 0 || softError {
		return -1, nil, duration, nil, &httpint.InvalidResponseError{
			StatusCode: resp.StatusCode,
			Header:     resp.Header,
			Body:       body,
		}
	}

	return resp.StatusCode, resp.Header, duration, body, nil
}

func isSoftError(body []byte, w Watch) (bool, error) {
	if len(body) == 0 {
		return false, nil
	}

	if bytes.Contains(body, []byte("504 - Gateway Time-out")) ||
		bytes.Contains(body, []byte("404 - Not Found")) ||
		bytes.Contains(body, []byte("503 - Service Unavailable")) {
		return true, nil
	}

	for _, p := range w.AdditionalSoftErrorPatterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return false, err
		}
		if re.Match(body) {
			return true, nil
		}
	}

	return false, nil
}
