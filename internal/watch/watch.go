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
	RetryOnMatch                 []string
}

type Replace struct {
	Pattern     string
	ReplaceWith string
}

type ReturnObject struct {
	StatusCode int
	Body       []byte
	Duration   time.Duration
	Header     map[string][]string
}

type InvalidResponseError struct {
	StatusCode int
	Header     map[string][]string
	Body       []byte
	Duration   time.Duration
}

func (err *InvalidResponseError) Error() string {
	return fmt.Sprintf("got invalid response on http request: status: %d, bodylen: %d", err.StatusCode, len(err.Body))
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

func (w Watch) shouldRetry(ret *ReturnObject, config *config.Configuration) (bool, string, error) {
	softError, err := isSoftError(ret.Body, w)
	if err != nil {
		return false, "", fmt.Errorf("could not check for soft error on %s: %w", w.URL, err)
	}

	// if we hit a soft error we should retry the request
	if softError {
		return true, "response body is a soft error", nil
	}

	ignoreStatusCode := false
	for _, ignore := range config.HTTPErrorsToIgnore {
		if ret.StatusCode == ignore {
			ignoreStatusCode = true
		}
	}

	// if we hit an error that we should ignore, bail out
	for _, ignore := range w.AdditionalHTTPErrorsToIgnore {
		if ret.StatusCode == ignore {
			ignoreStatusCode = true
		}
	}

	// if statuscode is ignored, do not retry
	if ignoreStatusCode {
		return false, "", nil
	}

	if ret.StatusCode != 200 {
		// non 200 status code, retry
		return true, "statuscode is not 200", nil
	}

	if len(ret.Body) == 0 {
		// zero length body, retry
		return true, "of zero length body", nil
	}

	// nothing else matched, good request, do not retry
	return false, "", nil
}

// checkWithRetries runs http.CheckWatch in a loop up to x times (configurable) to retry requests on errors
// it returns the same values as http.CheckWatch
// if the last request still results in an error the error is returned
func (w Watch) checkWithRetries(ctx context.Context, config *config.Configuration) (*ReturnObject, error) {
	var ret *ReturnObject
	var err error
	retries := config.Retry.Count
	retryDelay := config.Retry.Delay.Duration
	// check with retries
	for i := 1; i <= retries; i++ {
		// no sleep on first try
		if i > 1 {
			if retryDelay > 0 {
				w.logger.Error(fmt.Errorf("[%s] retrying after %s", w.Name, retryDelay))
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(retryDelay):
				}
			} else {
				w.logger.Error(fmt.Errorf("[%s] retrying without delay", w.Name))
			}
		}
		w.logger.Infof("try #%d for %s", i, w.Name)
		ret, err = w.doHTTP(ctx)
		if err != nil {
			w.logger.Errorf("[%s] received error %s", w.Name, err)
			continue
		}
		// check for additional errors like soft errors and status codes here
		retryResult, cause, err := w.shouldRetry(ret, config)
		if err != nil {
			return nil, err
		}

		if retryResult {
			w.logger.Infof("[%s] retrying because %s", w.Name, cause)
			continue
		}

		// no retry needed, return result
		return ret, nil
	}

	// err still set? return the error
	if err != nil {
		return nil, err
	}

	// if we reach here we still have an soft error after all retries
	return nil, &InvalidResponseError{
		StatusCode: ret.StatusCode,
		Body:       ret.Body,
		Header:     ret.Header,
		Duration:   ret.Duration,
	}
}

func (w Watch) doHTTP(ctx context.Context) (*ReturnObject, error) {
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
		return nil, fmt.Errorf("could create get request for %s: %w", w.URL, err)
	}

	for name, value := range w.Header {
		req.Header.Set(name, value)
	}

	start := time.Now()
	resp, err := w.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not get %s: %w", w.URL, err)
	}
	defer resp.Body.Close()
	duration := time.Since(start)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("could not read body from %s: %w", w.URL, err)
	}

	return &ReturnObject{
		StatusCode: resp.StatusCode,
		Header:     resp.Header,
		Duration:   duration,
		Body:       body,
	}, nil
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

func (w *Watch) Process(ctx context.Context, config *config.Configuration) (*ReturnObject, error) {
	ret, err := w.checkWithRetries(ctx, config)
	if err != nil {
		// if we reach here the last retry resulted in an error
		// or we have another config error
		return nil, err
	}

	// extract content
	if w.Pattern != "" {
		re, err := regexp.Compile(w.Pattern)
		if err != nil {
			return ret, fmt.Errorf("could not compile pattern %s: %w", w.Pattern, err)
		}
		match := re.FindSubmatch(ret.Body)
		if len(match) < 2 {
			return ret, fmt.Errorf("pattern %s did not match %s", w.Pattern, string(ret.Body))
		}
		ret.Body = match[1]
	}

	for _, replace := range w.Replaces {
		w.logger.Debugf("replacing %s", replace.Pattern)
		re, err := regexp.Compile(replace.Pattern)
		if err != nil {
			return ret, fmt.Errorf("could not compile replace pattern %s: %w", replace.Pattern, err)
		}
		ret.Body = re.ReplaceAll(ret.Body, []byte(replace.ReplaceWith))
		w.logger.Debugf("After %s:\n%s\n\n", replace.Pattern, string(ret.Body))
	}

	return ret, nil
}
