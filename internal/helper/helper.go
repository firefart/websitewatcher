package helper

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/PuerkitoBio/goquery"
	xhtml "golang.org/x/net/html"
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

func ExtractContent(html io.Reader, query string) (string, error) {
	doc, err := goquery.NewDocumentFromReader(html)
	if err != nil {
		return "", fmt.Errorf("could not parse HTML: %w", err)
	}
	content := doc.Find(query)
	if content == nil {
		return "", fmt.Errorf("no content found for query: %s", query)
	}

	if content.Length() == 0 {
		return "", fmt.Errorf("no content found for query: %s", query)
	}

	node := content.Get(0)
	if node == nil {
		return "", fmt.Errorf("no child nodes found for query: %s", query)
	}
	b := new(strings.Builder)
	if err := xhtml.Render(b, node); err != nil {
		return "", fmt.Errorf("could not render content: %w", err)
	}

	// Remove leading and trailing whitespace as the parser might add some
	return strings.TrimSpace(b.String()), nil
}
