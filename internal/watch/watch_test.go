package watch

import (
	"context"
	gohttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/firefart/websitewatcher/internal/config"
	"github.com/firefart/websitewatcher/internal/http"
	"github.com/firefart/websitewatcher/internal/logger"
)

func TestCheck(t *testing.T) {
	tests := map[string]struct {
		UserAgent     string
		ServerContent string
		ServerStatus  int
		WantContant   string
		WantStatus    int
	}{
		"Default check": {
			UserAgent:     "xxx",
			ServerContent: "test",
			ServerStatus:  gohttp.StatusOK,
			WantContant:   "test",
			WantStatus:    gohttp.StatusOK,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
				if tc.UserAgent != "" && r.Header.Get("User-Agent") != tc.UserAgent {
					t.Errorf("CheckWatch() want Useragent %s, got %s", tc.UserAgent, r.Header.Get("User-Agent"))
				}
				w.WriteHeader(tc.ServerStatus)
				if _, err := w.Write([]byte(tc.ServerContent)); err != nil {
					t.Fatalf("Write() err = %s, want nil", err)
				}
			}))
			defer server.Close()

			client := http.NewHTTPClient(tc.UserAgent, 1*time.Second)
			w := New(config.WatchConfig{
				Name: "Test",
				URL:  server.URL,
			}, &logger.NilLogger{}, client)

			ret, err := w.doHTTP(context.Background())
			if err != nil {
				t.Fatalf("CheckWatch() got err=%s, want nil", err)
			}
			if ret.StatusCode != tc.WantStatus {
				t.Errorf("CheckWatch() got status %d, want %d", ret.StatusCode, tc.WantStatus)
			}
			contentString := string(ret.Body)
			if contentString != tc.WantContant {
				t.Errorf("CheckWatch() got content %s, want %s", contentString, tc.WantContant)
			}
		})
	}
}
