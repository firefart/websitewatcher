package webhook

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/firefart/websitewatcher/internal/diff"
	httpint "github.com/firefart/websitewatcher/internal/http"
	"github.com/stretchr/testify/require"
)

func newTestClient(t *testing.T) *httpint.Client {
	t.Helper()
	logger := slog.New(slog.DiscardHandler)
	client, err := httpint.NewHTTPClient(logger, "test-agent", 10*time.Second, nil)
	require.NoError(t, err)
	return client
}

func testDiff() *diff.Diff {
	return &diff.Diff{
		Lines: []diff.Line{
			{Content: "+new line", LineMode: diff.LineModeAdded},
			{Content: "-old line", LineMode: diff.LineModeRemoved},
			{Content: " unchanged", LineMode: diff.LineModeUnchanged},
		},
	}
}

func testMeta(serverURL string) *diff.Metadata {
	return &diff.Metadata{
		Name:            "test-watch",
		URL:             serverURL,
		Description:     "Test description",
		RequestDuration: 100 * time.Millisecond,
		StatusCode:      200,
		BodyLength:      1000,
		LastFetch:       time.Now(),
	}
}

func TestSend_GET(t *testing.T) {
	t.Parallel()

	var gotMethod string
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := newTestClient(t)
	wh := Webhook{URL: server.URL, Method: http.MethodGet}

	err := Send(t.Context(), client, wh, testDiff(), testMeta(server.URL))
	require.NoError(t, err)
	require.Equal(t, http.MethodGet, gotMethod)
	require.Empty(t, gotBody)
}

func TestSend_POST_JSONPayload(t *testing.T) {
	t.Parallel()

	var gotBody []byte
	var gotContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := newTestClient(t)
	wh := Webhook{URL: server.URL, Method: http.MethodPost}
	d := testDiff()
	meta := testMeta(server.URL)

	err := Send(t.Context(), client, wh, d, meta)
	require.NoError(t, err)
	require.Equal(t, "application/json", gotContentType)

	var payload webhookJSON
	err = json.Unmarshal(gotBody, &payload)
	require.NoError(t, err)
	require.Equal(t, meta.Name, payload.Name)
	require.Equal(t, meta.URL, payload.URL)
	require.Equal(t, meta.Description, payload.Description)
	require.Len(t, payload.Diff, len(d.Lines))
	require.Equal(t, "+new line", payload.Diff[0].Content)
	require.Equal(t, string(diff.LineModeAdded), payload.Diff[0].Mode)
}

func TestSend_PUT_JSONPayload(t *testing.T) {
	t.Parallel()

	var gotMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := newTestClient(t)
	wh := Webhook{URL: server.URL, Method: http.MethodPut}

	err := Send(t.Context(), client, wh, testDiff(), testMeta(server.URL))
	require.NoError(t, err)
	require.Equal(t, http.MethodPut, gotMethod)
}

func TestSend_DELETE_NoBody(t *testing.T) {
	t.Parallel()

	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := newTestClient(t)
	wh := Webhook{URL: server.URL, Method: http.MethodDelete}

	err := Send(t.Context(), client, wh, testDiff(), testMeta(server.URL))
	require.NoError(t, err)
	require.Empty(t, gotBody)
}

func TestSend_Accepts2xx(t *testing.T) {
	t.Parallel()

	statuses := []int{http.StatusOK, http.StatusCreated, http.StatusAccepted, http.StatusNoContent}

	for _, status := range statuses {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			}))
			defer server.Close()

			client := newTestClient(t)
			wh := Webhook{URL: server.URL, Method: http.MethodGet}

			err := Send(t.Context(), client, wh, testDiff(), testMeta(server.URL))
			require.NoError(t, err)
		})
	}
}

func TestSend_ErrorOnNon2xx(t *testing.T) {
	t.Parallel()

	statuses := []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusNotFound, http.StatusInternalServerError}

	for _, status := range statuses {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			}))
			defer server.Close()

			client := newTestClient(t)
			wh := Webhook{URL: server.URL, Method: http.MethodGet}

			err := Send(t.Context(), client, wh, testDiff(), testMeta(server.URL))
			require.Error(t, err)
			require.ErrorContains(t, err, "webhook returned status code")
		})
	}
}

func TestSend_CustomHeaders(t *testing.T) {
	t.Parallel()

	var gotAuthorization string
	var gotCustomHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("Authorization")
		gotCustomHeader = r.Header.Get("X-Custom")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := newTestClient(t)
	wh := Webhook{
		URL:    server.URL,
		Method: http.MethodGet,
		Header: map[string]string{
			"Authorization": "Bearer token123",
			"X-Custom":      "custom-value",
		},
	}

	err := Send(t.Context(), client, wh, testDiff(), testMeta(server.URL))
	require.NoError(t, err)
	require.Equal(t, "Bearer token123", gotAuthorization)
	require.Equal(t, "custom-value", gotCustomHeader)
}
