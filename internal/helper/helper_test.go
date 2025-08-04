package helper

import (
	"bytes"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

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
			want: `<body>body content<p>more content</p>

</body>`,
		},
	}

	for i, tc := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()

			got, err := ExtractContent(bytes.NewReader([]byte(tc.input)), "body")
			require.NoError(t, err)
			if got != tc.want {
				t.Errorf("extractBody() got:\n%s, want:\n%s", got, tc.want)
			}
		})
	}
}
