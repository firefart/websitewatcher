package diff

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strings"
	"time"
)

var indexRe = regexp.MustCompile(`^index [A-Fa-f0-9]+\.\.[A-Fa-f0-9]+ [0-9]+$`)

type Diff struct {
	Lines []Line
}

type Line struct {
	Content  string
	LineMode LineMode
}

type LineMode string

const (
	LineModeUnchanged LineMode = "unchanged"
	LineModeAdded     LineMode = "added"
	LineModeRemoved   LineMode = "deleted"
	LineModeMetadata  LineMode = "metadata"
)

type Metadata struct {
	Name            string
	URL             string
	Description     string
	RequestDuration time.Duration
	StatusCode      int
	BodyLength      int
	LastFetch       time.Time
}

func (m Metadata) String() string {
	text := fmt.Sprintf("Name: %s\nURL: %s", m.Name, m.URL)
	if m.Description != "" {
		text = fmt.Sprintf("%s\nDescription: %s", text, m.Description)
	}
	text = fmt.Sprintf("%s\nRequest Duration: %s\nStatus: %d\nBodylen: %d\nLast Fetch: %s", text, m.RequestDuration, m.StatusCode, m.BodyLength, m.LastFetch.Format(time.RFC1123))
	return text
}

func (d Diff) Text(meta *Metadata) (string, error) {
	builder := strings.Builder{}
	for _, line := range d.Lines {
		if _, err := builder.WriteString(fmt.Sprintf("%s\n", line.Content)); err != nil {
			return "", err
		}
	}

	return fmt.Sprintf("%s\n%s", meta.String(), builder.String()), nil
}

func (d Diff) HTML(ctx context.Context, meta *Metadata) (string, error) {
	var buf bytes.Buffer
	if err := HTMLDiff(&d, meta.String()).Render(ctx, &buf); err != nil {
		return "", fmt.Errorf("could not render HTML diff: %w", err)
	}
	return buf.String(), nil
}

func GenerateDiff(ctx context.Context, text1, text2 string) (*Diff, error) {
	rawDiff, err := diffGit(ctx, text1, text2)
	if err != nil {
		return nil, err
	}

	var diff Diff
	scanner := bufio.NewScanner(bytes.NewReader(rawDiff))
	for scanner.Scan() {
		text := scanner.Text()
		// filter out unneeded stuff like
		// 		diff --git a/tmp/websitewatcher_SQINZHLunW/538847876 b/tmp/websitewatcher_SQINZHLunW/4036934040
		// 		index 8d30a96..fed39cb 100644
		// 		--- a/tmp/websitewatcher_SQINZHLunW/538847876
		// 		+++ b/tmp/websitewatcher_SQINZHLunW/4036934040
		switch {
		case strings.HasPrefix(text, "diff --git"):
			continue
		case indexRe.MatchString(text):
			continue
		case strings.HasPrefix(text, "---"):
			continue
		case strings.HasPrefix(text, "+++"):
			continue
		case strings.HasPrefix(text, "@@"):
			diff.Lines = append(diff.Lines, Line{
				Content:  text,
				LineMode: LineModeMetadata,
			})
			continue
		case strings.HasPrefix(text, "-"):
			diff.Lines = append(diff.Lines, Line{
				Content:  text,
				LineMode: LineModeRemoved,
			})
			continue
		case strings.HasPrefix(text, "+"):
			diff.Lines = append(diff.Lines, Line{
				Content:  text,
				LineMode: LineModeAdded,
			})
			continue
		default:
			diff.Lines = append(diff.Lines, Line{
				Content:  text,
				LineMode: LineModeUnchanged,
			})
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return &diff, nil
}

func diffGit(ctx context.Context, text1, text2 string) ([]byte, error) {
	tmpdir := path.Join(os.TempDir(), fmt.Sprintf("websitewatcher_%s", rand.Text()))
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
	cmd := exec.CommandContext( // nolint:gosec
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
