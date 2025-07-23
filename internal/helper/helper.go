package helper

import (
	"context"
	"fmt"
	"io"
	"os/exec"

	"github.com/PuerkitoBio/goquery"
)

func IsGitInstalled(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "git", "--version")
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}

// HTML2Text converts HTML content from an io.Reader to plain text with all script and style tags removed.
func HTML2Text(html io.Reader) (string, error) {
	doc, err := goquery.NewDocumentFromReader(html)
	if err != nil {
		return "", fmt.Errorf("could not parse HTML: %w", err)
	}
	doc.Find("script").Remove() // Remove all script tags
	doc.Find("style").Remove()  // Remove all style tags
	return doc.Text(), nil
}
