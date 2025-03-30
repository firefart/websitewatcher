package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/firefart/websitewatcher/internal/diff"
	httpint "github.com/firefart/websitewatcher/internal/http"
)

type Webhook struct {
	URL       string
	Header    map[string]string
	Method    string
	Useragent string
}

type webhookJSON struct {
	Name            string            `json:"name"`
	URL             string            `json:"url"`
	Description     string            `json:"description"`
	Diff            []webhookJSONDiff `json:"diff"`
	RequestDuration time.Duration     `json:"request_duration"`
	StatusCode      int               `json:"status_code"`
	BodyLength      int               `json:"body_length"`
	LastFetch       time.Time         `json:"last_fetch"`
}

type webhookJSONDiff struct {
	Content string `json:"content"`
	Mode    string `json:"mode"`
}

func Send(ctx context.Context, httpClient *httpint.Client, wh Webhook, d *diff.Diff, meta *diff.Metadata) error {
	var data io.Reader
	// we only need the payload on post, put or patch
	if wh.Method == http.MethodPost || wh.Method == http.MethodPut || wh.Method == http.MethodPatch {
		postData := webhookJSON{
			Name:            meta.Name,
			URL:             meta.URL,
			Description:     meta.Description,
			RequestDuration: meta.RequestDuration,
			StatusCode:      meta.StatusCode,
			BodyLength:      meta.BodyLength,
			Diff:            make([]webhookJSONDiff, 0, len(d.Lines)),
			LastFetch:       meta.LastFetch,
		}
		for i, line := range d.Lines {
			postData.Diff[i] = webhookJSONDiff{
				Content: line.Content,
				Mode:    string(line.LineMode),
			}
		}
		jsonValue, err := json.Marshal(postData)
		if err != nil {
			return fmt.Errorf("failed to marshal webhook data: %w", err)
		}
		data = bytes.NewReader(jsonValue)
	}

	req, err := http.NewRequestWithContext(ctx, wh.Method, wh.URL, data)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	// if we have a body, set the content type
	if data != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// allow for custom headers
	for k, v := range wh.Header {
		req.Header.Set(k, v)
	}

	resp, err := httpClient.Do(req, wh.Useragent)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("webhook returned status code %d", resp.StatusCode)
	}
	return nil
}
