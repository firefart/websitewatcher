package helper

import (
	"bytes"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		selector string
		want     string
	}{
		{
			selector: "body",
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
		{
			selector: "#__NEXT_DATA__",
			input: `<div>masdknmflasdf</div>
<script id="__NEXT_DATA__" type="application/json">
{"a":{"a":"a"}}
</script>
<div id="outer">&lt;<div id="inner">&gt;</div> </div>`,
			want: `<script id="__NEXT_DATA__" type="application/json">
{"a":{"a":"a"}}
</script>`,
		},
	}

	for i, tc := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()

			got, err := ExtractContent(bytes.NewReader([]byte(tc.input)), tc.selector)
			require.NoError(t, err)
			if got != tc.want {
				t.Errorf("extractBody() got:\n%s, want:\n%s", got, tc.want)
			}
		})
	}
}
