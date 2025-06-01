package watch

import (
	"log/slog"
	gohttp "net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/firefart/websitewatcher/internal/config"
	"github.com/firefart/websitewatcher/internal/http"
	"github.com/stretchr/testify/require"
)

func TestCheck(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		UserAgent     string
		ServerContent string
		ServerStatus  int
		WantContent   string
		WantStatus    int
	}{
		"Default check": {
			UserAgent:     "xxx",
			ServerContent: "test",
			ServerStatus:  gohttp.StatusOK,
			WantContent:   "test",
			WantStatus:    gohttp.StatusOK,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

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

			logger := slog.New(slog.DiscardHandler)
			client, err := http.NewHTTPClient(logger, tc.UserAgent, 1*time.Second, nil)
			if err != nil {
				t.Fatalf("NewHTTPClient() got err=%s, want nil", err)
			}
			w := New(
				config.WatchConfig{
					Name: "Test",
					URL:  server.URL,
				},
				logger,
				client,
			)

			ret, err := w.doHTTP(t.Context())
			if err != nil {
				t.Fatalf("CheckWatch() got err=%s, want nil", err)
			}
			if ret.StatusCode != tc.WantStatus {
				t.Errorf("CheckWatch() got status %d, want %d", ret.StatusCode, tc.WantStatus)
			}
			contentString := string(ret.Body)
			if contentString != tc.WantContent {
				t.Errorf("CheckWatch() got content %s, want %s", contentString, tc.WantContent)
			}
		})
	}
}

func TestExtractBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{
			input: `
			<!DOCTYPE html>
<html>
<head>
<title>
Title of the document
</title>
</head>
<body>body content<p>more content</p></body>
</html>
`,
			want: `<body>bodycontent<p>morecontent</p></body>`,
		},
	}

	for i, tc := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()

			gotBytes, err := extractBody([]byte(tc.input))
			require.NoError(t, err)
			// remove all whitespaces and newlines for comparison
			// as the renderer intruoduces newlines and spaces
			re := regexp.MustCompile(`\s+`)
			out := re.ReplaceAll(gotBytes, []byte(""))
			got := string(out)
			if got != tc.want {
				t.Errorf("extractBody() got:\n%s, want:\n%s", got, tc.want)
			}
		})
	}
}

func TestEmptyLineRegex(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Single empty line",
			input:    "line1\n\nline2",
			expected: "line1\nline2",
		},
		{
			name:     "Multiple consecutive empty lines",
			input:    "line1\n\n\n\nline2",
			expected: "line1\nline2",
		},
		{
			name:     "Empty lines with spaces",
			input:    "line1\n  \n\t\n\nline2",
			expected: "line1\nline2",
		},
		{
			name:     "Empty lines with mixed whitespace",
			input:    "line1\n \t \n   \n\t\t\nline2",
			expected: "line1\nline2",
		},
		{
			name:     "No empty lines",
			input:    "line1\nline2\nline3",
			expected: "line1\nline2\nline3",
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "Only newlines",
			input:    "\n\n\n",
			expected: "\n",
		},
		{
			name:     "Empty lines at beginning",
			input:    "\n\nline1\nline2",
			expected: "\nline1\nline2",
		},
		{
			name:     "Empty lines at end",
			input:    "line1\nline2\n\n\n",
			expected: "line1\nline2\n",
		},
		{
			name:     "Multiple sections with empty lines",
			input:    "section1\n\n\nsection2\n\nsection3\n\n\n\nsection4",
			expected: "section1\nsection2\nsection3\nsection4",
		},
		{
			name:     "Lines with only whitespace characters",
			input:    "line1\n   \n\t\t\n  \t  \nline2",
			expected: "line1\nline2",
		},
		{
			name:     "Windows line endings with empty lines",
			input:    "line1\r\n\r\n\r\nline2",
			expected: "line1\r\nline2",
		},
		{
			name:     "Mixed content with various empty line patterns",
			input:    "start\n\n  \n\nmiddle\n\t\n \nend\n\n",
			expected: "start\nmiddle\nend\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := emptyLineRegex.ReplaceAll([]byte(tc.input), []byte("\n"))
			got := string(result)

			if got != tc.expected {
				t.Errorf("emptyLineRegex.ReplaceAll() = %q, want %q", got, tc.expected)
			}
		})
	}
}
