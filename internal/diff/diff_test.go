package diff

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateDiff(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		text1    string
		text2    string
		wantErr  bool
		validate func(t *testing.T, diff *Diff)
	}{
		"identical strings": {
			text1: "hello world",
			text2: "hello world",
			validate: func(t *testing.T, diff *Diff) {
				require.Empty(t, diff.Lines, "identical strings should produce no diff lines")
			},
		},
		"empty strings": {
			text1: "",
			text2: "",
			validate: func(t *testing.T, diff *Diff) {
				require.Empty(t, diff.Lines, "empty strings should produce no diff lines")
			},
		},
		"added line": {
			text1: "line1",
			text2: "line1\nline2",
			validate: func(t *testing.T, diff *Diff) {
				require.Len(t, diff.Lines, 3)
				require.Equal(t, "@@ -1 +1,2 @@", diff.Lines[0].Content)
				require.Equal(t, LineModeMetadata, diff.Lines[0].LineMode)
				require.Equal(t, " line1", diff.Lines[1].Content)
				require.Equal(t, LineModeUnchanged, diff.Lines[1].LineMode)
				require.Equal(t, "+line2", diff.Lines[2].Content)
				require.Equal(t, LineModeAdded, diff.Lines[2].LineMode)
			},
		},
		"removed line": {
			text1: "line1\nline2",
			text2: "line1",
			validate: func(t *testing.T, diff *Diff) {
				require.Len(t, diff.Lines, 3)
				require.Equal(t, "@@ -1,2 +1 @@", diff.Lines[0].Content)
				require.Equal(t, LineModeMetadata, diff.Lines[0].LineMode)
				require.Equal(t, " line1", diff.Lines[1].Content)
				require.Equal(t, LineModeUnchanged, diff.Lines[1].LineMode)
				require.Equal(t, "-line2", diff.Lines[2].Content)
				require.Equal(t, LineModeRemoved, diff.Lines[2].LineMode)
			},
		},
		"complete replacement": {
			text1: "old content",
			text2: "new content",
			validate: func(t *testing.T, diff *Diff) {
				require.NotEmpty(t, diff.Lines)
				var hasRemoved, hasAdded bool
				for _, line := range diff.Lines {
					if line.LineMode == LineModeRemoved && line.Content == "-old content" {
						hasRemoved = true
					}
					if line.LineMode == LineModeAdded && line.Content == "+new content" {
						hasAdded = true
					}
				}
				require.True(t, hasRemoved, "should have removed line")
				require.True(t, hasAdded, "should have added line")
			},
		},
		"multiline with mixed changes": {
			text1: "line1\nline2\nline3",
			text2: "line1\nmodified line2\nline3\nline4",
			validate: func(t *testing.T, diff *Diff) {
				require.NotEmpty(t, diff.Lines)
				var hasUnchanged, hasRemoved, hasAdded bool
				for _, line := range diff.Lines {
					switch line.LineMode {
					case LineModeUnchanged:
						if line.Content == " line1" || line.Content == " line3" {
							hasUnchanged = true
						}
					case LineModeRemoved:
						if line.Content == "-line2" {
							hasRemoved = true
						}
					case LineModeAdded:
						if line.Content == "+modified line2" || line.Content == "+line4" {
							hasAdded = true
						}
					case LineModeMetadata:
						// nothing to do here
					}
				}
				require.True(t, hasUnchanged, "should have unchanged lines")
				require.True(t, hasRemoved, "should have removed lines")
				require.True(t, hasAdded, "should have added lines")
			},
		},
		"single character change": {
			text1: "hello",
			text2: "hallo",
			validate: func(t *testing.T, diff *Diff) {
				require.NotEmpty(t, diff.Lines)
				var hasRemoved, hasAdded bool
				for _, line := range diff.Lines {
					if line.LineMode == LineModeRemoved && line.Content == "-hello" {
						hasRemoved = true
					}
					if line.LineMode == LineModeAdded && line.Content == "+hallo" {
						hasAdded = true
					}
				}
				require.True(t, hasRemoved, "should have removed original line")
				require.True(t, hasAdded, "should have added modified line")
			},
		},
		"add to empty": {
			text1: "",
			text2: "new content",
			validate: func(t *testing.T, diff *Diff) {
				require.NotEmpty(t, diff.Lines)
				found := false
				for _, line := range diff.Lines {
					if line.LineMode == LineModeAdded && line.Content == "+new content" {
						found = true
						break
					}
				}
				require.True(t, found, "should have added line with new content")
			},
		},
		"remove all": {
			text1: "content to remove",
			text2: "",
			validate: func(t *testing.T, diff *Diff) {
				require.NotEmpty(t, diff.Lines)
				found := false
				for _, line := range diff.Lines {
					if line.LineMode == LineModeRemoved && line.Content == "-content to remove" {
						found = true
						break
					}
				}
				require.True(t, found, "should have removed line")
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			diff, err := GenerateDiff(ctx, tc.text1, tc.text2)

			if tc.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, diff)
			tc.validate(t, diff)
		})
	}
}

func TestGenerateDiff_ContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // Cancel immediately

	_, err := GenerateDiff(ctx, "test1", "test2")
	require.Error(t, err)
	require.Contains(t, err.Error(), "could not generate git diff")
}
