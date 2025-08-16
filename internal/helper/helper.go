package helper

import (
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

func IsGitInstalled() bool {
	_, err := exec.LookPath("git")
	return err == nil
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

	// Check if this is a script tag - if so, return the text content
	if content.Is("script") {
		return strings.TrimSpace(content.Text()), nil
	}

	// For other elements, get the HTML content but preserve entities
	innerHTML, err := content.Html()
	if err != nil {
		return "", fmt.Errorf("could not extract HTML content: %w", err)
	}

	return strings.TrimSpace(innerHTML), nil
}
