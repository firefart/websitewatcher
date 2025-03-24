package diff

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/firefart/websitewatcher/internal/helper"
	"github.com/sergi/go-diff/diffmatchpatch"
)

func GenerateHTMLDiffInternal(body string, text1, text2 string) (string, error) {
	diffHTML := diffInternal(text1, text2)
	body = strings.ReplaceAll(body, "\n", "<br>\n")
	body = fmt.Sprintf("<html><head></head><body>%s<br><br>\n%s</body></html>", body, diffHTML)
	return body, nil
}

func GenerateDiffGit(ctx context.Context, body string, text1, text2 string) (string, string, error) {
	diff, err := diffGit(ctx, text1, text2)
	if err != nil {
		return "", "", err
	}
	diffCSS, diffHTML, err := convertGitDiffToHTML(string(diff))
	if err != nil {
		return "", "", err
	}

	textBody := fmt.Sprintf("%s\n%s", body, diff)

	body = strings.ReplaceAll(body, "\n", "<br>\n")
	htmlBody := fmt.Sprintf("<html><head><style>%s</style></head><body>%s<br><br>\n%s</body></html>", diffCSS, body, diffHTML)
	return textBody, htmlBody, nil
}

func diffInternal(text1, text2 string) []byte {
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(text1, text2, false)
	htmlDiff := dmp.DiffPrettyHtml(diffs)
	return []byte(htmlDiff)
}

func diffGit(ctx context.Context, text1, text2 string) ([]byte, error) {
	tmpdir := path.Join(os.TempDir(), fmt.Sprintf("websitewatcher_%s", helper.RandStringRunes(10))) // nolint:gomnd
	err := os.Mkdir(tmpdir, os.ModePerm)
	if err != nil {
		return nil, fmt.Errorf("could not create temp dir %q: %w", tmpdir, err)
	}
	defer func(path string) {
		_ = os.RemoveAll(path)
	}(tmpdir)

	inputFile1, err := os.CreateTemp(tmpdir, "")
	if err != nil {
		return nil, fmt.Errorf("could not create inputFile1: %w", err)
	}
	defer func(name string) {
		_ = os.Remove(name)
	}(inputFile1.Name())
	if _, err := fmt.Fprintf(inputFile1, "%s\n", text1); err != nil { // add a newline at the end so git does not complain
		return nil, fmt.Errorf("could not write inputFile1: %w", err)
	}

	inputFile2, err := os.CreateTemp(tmpdir, "")
	if err != nil {
		return nil, fmt.Errorf("could not create inputFile2: %w", err)
	}
	defer func(name string) {
		_ = os.Remove(name)
	}(inputFile2.Name())
	if _, err := fmt.Fprintf(inputFile2, "%s\n", text2); err != nil { // add a newline at the end so git does not complain
		return nil, fmt.Errorf("could not write inputFile2: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	outFile := path.Join(tmpdir, "diff.txt")

	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd := exec.CommandContext(
		ctx,
		"git",
		"diff",
		"--no-color",                        // disable color output as we will parse it manually
		"--no-index",                        // no git processing
		"--text",                            // treat files as text
		"-w",                                // ignore whitespaces
		"-b",                                // ignore change in whitespaces
		fmt.Sprintf("--output=%s", outFile), // output file
		inputFile1.Name(),                   // input file 1
		inputFile2.Name(),                   // input file 2
	)
	cmd.Dir = tmpdir
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		// exit error 0 and 1 are good ones so ignore them
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode := exitErr.ExitCode()
			if exitCode != 0 && exitCode != 1 {
				return nil, fmt.Errorf("could not execute git diff: %w - Stderr: %s", err, stderr.String())
			}
			// otherwise continue
		} else {
			return nil, fmt.Errorf("could not execute git diff: %w - Stderr: %s", err, stderr.String())
		}
	}

	diff, err := os.ReadFile(outFile)
	if err != nil {
		return nil, fmt.Errorf("could not read outfile: %w", err)
	}
	return diff, nil
}

const gitDiffCSS = `
	div {
		font-family: monospace;
		padding-left: 0.5em;
    padding-right: 0.5em;
	}
	div.container {
		display: inline-block;
	}
	div.default {}
	div.add {
		background-color: #c8f0da;
	}
	div.delete {
		background-color: #ffcbbd;
	}
	div.changes {
		background-color: lightyellow;
	}
`

var indexRe = regexp.MustCompile(`^index [A-Fa-f0-9]+\.\.[A-Fa-f0-9]+ [0-9]+$`)

func convertGitDiffToHTML(input string) (string, string, error) {
	scanner := bufio.NewScanner(strings.NewReader(input))
	builder := strings.Builder{}

	for scanner.Scan() {
		text := scanner.Text()
		classname := "default"
		// filter out unneeded stuff like
		// 		diff --git a/tmp/websitewatcher_SQINZHLunW/538847876 b/tmp/websitewatcher_SQINZHLunW/4036934040
		// 		index 8d30a96..fed39cb 100644
		// 		--- a/tmp/websitewatcher_SQINZHLunW/538847876
		// 		+++ b/tmp/websitewatcher_SQINZHLunW/4036934040
		if strings.HasPrefix(text, "diff --git") {
			continue
		} else if indexRe.MatchString(text) {
			continue
		} else if strings.HasPrefix(text, "---") {
			continue
		} else if strings.HasPrefix(text, "+++") {
			continue
		} else if strings.HasPrefix(text, "@@") {
			classname = "changes"
		} else if strings.HasPrefix(text, "-") {
			classname = "delete"
		} else if strings.HasPrefix(text, "+") {
			classname = "add"
		}

		if _, err := builder.WriteString(fmt.Sprintf(`<div class="%s">%s</div>`, classname, html.EscapeString(text))); err != nil {
			return "", "", err
		}
	}

	if err := scanner.Err(); err != nil {
		return "", "", err
	}

	inner := builder.String()
	html := fmt.Sprintf(`<div class="container">%s</div>`, inner)

	return gitDiffCSS, html, nil
}
