package watch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/firefart/websitewatcher/internal/config"
	httpint "github.com/firefart/websitewatcher/internal/http"
	"github.com/itchyny/gojq"
)

type Watch struct {
	httpClient *httpint.Client
	logger     *slog.Logger

	Name                    string
	Cron                    string
	URL                     string
	Description             string
	Method                  string
	Body                    string
	Header                  map[string]string
	AdditionalTo            []string
	NoErrorMailOnStatusCode []int
	Disabled                bool
	Pattern                 string
	Replaces                []Replace
	RetryOnMatch            []string
	SkipSofterrorPatterns   bool
	JQ                      string
	UserAgent               string
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
	ErrorMessage string
	StatusCode   int
	Header       map[string][]string
	Body         []byte
	Duration     time.Duration
}

func (err *InvalidResponseError) Error() string {
	return fmt.Sprintf("got invalid response on http request: message: %s, status: %d, bodylen: %d", err.ErrorMessage, err.StatusCode, len(err.Body))
}

func New(c config.WatchConfig, logger *slog.Logger, httpClient *httpint.Client) Watch {
	w := Watch{
		logger:                  logger,
		httpClient:              httpClient,
		Cron:                    c.Cron,
		Name:                    c.Name,
		URL:                     c.URL,
		Description:             c.Description,
		Method:                  c.Method,
		Body:                    c.Body,
		Header:                  c.Header,
		AdditionalTo:            c.AdditionalTo,
		NoErrorMailOnStatusCode: c.NoErrorMailOnStatusCode,
		Disabled:                c.Disabled,
		Pattern:                 c.Pattern,
		Replaces:                make([]Replace, len(c.Replaces)),
		RetryOnMatch:            c.RetryOnMatch,
		SkipSofterrorPatterns:   c.SkipSofterrorPatterns,
		JQ:                      c.JQ,
		UserAgent:               c.Useragent,
	}
	if w.Method == "" {
		w.Method = http.MethodGet
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

func (w Watch) shouldRetry(ret *ReturnObject, config config.Configuration) (bool, string, error) {
	if ret.StatusCode != 200 {
		// non 200 status code, retry
		return true, fmt.Sprintf("statuscode is %d - %s", ret.StatusCode, http.StatusText(ret.StatusCode)), nil
	}

	if len(ret.Body) == 0 {
		return false, "zero length body", nil
	}

	if !w.SkipSofterrorPatterns {
		// https://github.com/nginx/nginx/blob/master/src/http/ngx_http_special_response.c
		patterns := [...]string{
			"504 - Gateway Time-out",
			"404 - Not Found",
			"503 - Service Unavailable",
			"<h1>503 Service Unavailable</h1>",
			"<h1>403 Forbidden</h1>",
			"<h1>404 Not Found</h1>",
			"<h1>405 Not Allowed</h1>",
			"<h1>429 Too Many Requests</h1>",
			"<h1>500 Internal Server Error</h1>",
			"<h1>502 Bad Gateway</h1>",
			"<h1>503 Service Temporarily Unavailable</h1>",
			"Faithfully yours, nginx.",
			"<!-- a padding to disable MSIE and Chrome friendly error page -->",
		}
		for _, p := range patterns {
			if bytes.Contains(ret.Body, []byte(p)) {
				return true, fmt.Sprintf("matches the hardcoded pattern %q", p), nil
			}
		}
	}

	for _, p := range config.RetryOnMatch {
		re, err := regexp.Compile(p)
		if err != nil {
			return false, "", err
		}
		if re.Match(ret.Body) {
			return true, fmt.Sprintf("matches the global pattern %q", p), nil
		}
	}

	for _, p := range w.RetryOnMatch {
		re, err := regexp.Compile(p)
		if err != nil {
			return false, "", err
		}
		if re.Match(ret.Body) {
			return true, fmt.Sprintf("matches the pattern %q", p), nil
		}
	}

	// nothing else matched, good request, do not retry
	return false, "", nil
}

// checkWithRetries runs http.CheckWatch in a loop up to x times (configurable) to retry requests on errors
// it returns the same values as http.CheckWatch
// if the last request still results in an error the error is returned
func (w Watch) checkWithRetries(ctx context.Context, config config.Configuration) (*ReturnObject, error) {
	var ret *ReturnObject
	var err error
	retries := config.Retry.Count
	retryDelay := config.Retry.Delay
	// check with retries
	for i := 1; i <= retries; i++ {
		// no sleep on first try
		if i > 1 {
			if retryDelay > 0 {
				w.logger.Info("retrying", slog.String("name", w.Name), slog.Duration("delay", retryDelay))
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(retryDelay):
				}
			} else {
				w.logger.Info("retrying without delay", slog.String("name", w.Name))
			}
		}
		w.logger.Info("checking watch", slog.String("name", w.Name), slog.Int("try", i))
		ret, err = w.doHTTP(ctx)
		if err != nil {
			w.logger.Error("received error", slog.String("name", w.Name), slog.String("err", err.Error()))
			if i != retries {
				w.logger.Info("retrying", slog.String("name", w.Name), slog.Int("try", i))
				// only continue if it's not the last retry
				continue
			}
			// return error if still a retry response on the last iteration
			return nil, &InvalidResponseError{
				ErrorMessage: fmt.Sprintf("still an error after %d retries: %v", retries, err),
				StatusCode:   ret.StatusCode,
				Body:         ret.Body,
				Header:       ret.Header,
				Duration:     ret.Duration,
			}
		}
		// check for additional errors like soft errors and status codes here
		retryResult, cause, err := w.shouldRetry(ret, config)
		if err != nil {
			return nil, err
		}

		if retryResult {
			w.logger.Info("retry check", slog.String("name", w.Name), slog.String("cause", cause))
			if i != retries {
				// only continue if it's not the last retry
				continue
			}
			// return error if still a retry response on the last iteration
			return nil, &InvalidResponseError{
				ErrorMessage: fmt.Sprintf("still a response error after %d retries: %s", retries, cause),
				StatusCode:   ret.StatusCode,
				Body:         ret.Body,
				Header:       ret.Header,
				Duration:     ret.Duration,
			}
		}

		// no retry needed, return result
		return ret, nil
	}

	// err still set? return the error
	if err != nil {
		return nil, err
	}

	// if we reach here we still have a soft error after all retries
	return nil, &InvalidResponseError{
		ErrorMessage: "response error after all retries",
		StatusCode:   ret.StatusCode,
		Body:         ret.Body,
		Header:       ret.Header,
		Duration:     ret.Duration,
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
	resp, err := w.httpClient.Do(req, w.UserAgent)
	if err != nil {
		return nil, fmt.Errorf("could not get %s: %w", w.URL, err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			w.logger.Error("error on body close", slog.String("err", err.Error()))
		}
	}()
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

func (w Watch) Process(ctx context.Context, config config.Configuration) (*ReturnObject, error) {
	ret, err := w.checkWithRetries(ctx, config)
	if err != nil {
		// if we reach here the last retry resulted in an error,
		// or we have another config error
		// the InvalidResponseError is handled by the calling function
		return nil, err
	}

	// check if we need to do some jq magic
	if w.JQ != "" {
		query, err := gojq.Parse(w.JQ)
		if err != nil {
			return nil, fmt.Errorf("invalid jq query: %w", err)
		}
		var x any
		if err := json.Unmarshal(ret.Body, &x); err != nil {
			var body []byte
			if len(ret.Body) > 500 {
				body = ret.Body[:500]
			} else {
				body = ret.Body
			}
			return nil, fmt.Errorf("supplied a jq query but the body is no valid json: %w. Body: %s", err, string(body))
		}
		iter := query.Run(x)
		var newBody []any
		for {
			v, ok := iter.Next()
			if !ok {
				break
			}
			if err, ok := v.(error); ok {
				return nil, err
			}
			newBody = append(newBody, v)
		}
		j2, err := json.MarshalIndent(newBody, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("could not remarshal json: %w", err)
		}
		ret.Body = j2
	}

	// extract content
	if w.Pattern != "" {
		re, err := regexp.Compile(w.Pattern)
		if err != nil {
			return ret, fmt.Errorf("could not compile pattern %q: %w", w.Pattern, err)
		}
		match := re.FindSubmatch(ret.Body)
		if match == nil || len(match) < 2 {
			w.logger.Error("pattern did not match", slog.String("pattern", w.Pattern), slog.String("body", string(ret.Body)))
			return ret, &InvalidResponseError{
				ErrorMessage: fmt.Sprintf("pattern %q did not match %s", w.Pattern, string(ret.Body)),
				StatusCode:   ret.StatusCode,
				Header:       ret.Header,
				Body:         ret.Body,
				Duration:     ret.Duration,
			}
		}
		ret.Body = match[1]
	}

	for _, replace := range w.Replaces {
		w.logger.Debug("replacing", slog.String("name", w.Name), slog.String("pattern", replace.Pattern), slog.String("replacement", replace.ReplaceWith))
		re, err := regexp.Compile(replace.Pattern)
		if err != nil {
			return ret, fmt.Errorf("could not compile replace pattern %s: %w", replace.Pattern, err)
		}
		ret.Body = re.ReplaceAll(ret.Body, []byte(replace.ReplaceWith))
		w.logger.Debug("after replacement", slog.String("pattern", replace.Pattern), slog.String("replacement", replace.ReplaceWith), slog.String("body", string(ret.Body)))
	}

	return ret, nil
}
