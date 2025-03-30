package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/firefart/websitewatcher/internal/diff"
	httpint "github.com/firefart/websitewatcher/internal/http"
)

type webhookData struct {
	Name            string        `json:"name"`
	URL             string        `json:"url"`
	Description     string        `json:"description"`
	Diff            []webhookDiff `json:"diff"`
	RequestDuration time.Duration `json:"request_duration"`
	StatusCode      int           `json:"status_code"`
	BodyLength      int           `json:"body_length"`
}

type webhookDiff struct {
	Content string `json:"content"`
	Mode    string `json:"mode"`
}

func Send(ctx context.Context, httpClient *httpint.Client, url string, d *diff.Diff, meta *diff.Metadata) error {
	data := webhookData{
		Name:            meta.Name,
		URL:             meta.URL,
		Description:     meta.Description,
		RequestDuration: meta.RequestDuration,
		StatusCode:      meta.StatusCode,
		BodyLength:      meta.BodyLength,
		Diff:            make([]webhookDiff, 0, len(d.Lines)),
	}
	for i, line := range d.Lines {
		data.Diff[i] = webhookDiff{
			Content: line.Content,
			Mode:    string(line.LineMode),
		}
	}
	jsonValue, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook data: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonValue))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("webhook returned status code %d", resp.StatusCode)
	}
	return nil
}
