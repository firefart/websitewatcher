package http_test

import (
	"bytes"
	"context"
	gohttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/firefart/websitewatcher/internal/config"
	"github.com/firefart/websitewatcher/internal/http"
)

func TestCheckWatch(t *testing.T) {
	userAgent := ""
	content := []byte("asd")
	status := gohttp.StatusOK
	server := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		if r.Header.Get("User-Agent") != userAgent {
			t.Errorf("CheckWatch() want Useragent %s, got %s", userAgent, r.Header.Get("User-Agent"))
		}
		w.WriteHeader(status)
		w.Write(content)
	}))
	defer server.Close()
	watch := config.Watch{
		Name: "Test",
		URL:  server.URL,
	}
	client := http.NewHTTPClient(userAgent, 1*time.Second)
	statusCode, _, _, responseContent, err := client.CheckWatch(context.TODO(), watch)
	if err != nil {
		t.Fatalf("CheckWatch() got err=%s, want nil", err)
	}
	if statusCode != status {
		t.Errorf("CheckWatch() got status %d, want %d", statusCode, status)
	}
	if !bytes.Equal(responseContent, content) {
		t.Errorf("CheckWatch() got content %s, want %s", string(responseContent), string(content))
	}
}
