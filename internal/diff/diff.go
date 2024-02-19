package diff

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/brianvoe/gofakeit/v7"
	"github.com/firefart/websitewatcher/internal/helper"
	http2 "github.com/firefart/websitewatcher/internal/http"
	"github.com/sergi/go-diff/diffmatchpatch"
)

type api struct {
	Left  string `json:"left"`
	Right string `json:"right"`
}

type apiResponse struct {
	HTML  string `json:"html"`
	CSS   string `json:"css"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Errors []struct {
		Message  string `json:"msg"`
		Param    string `json:"left"`
		Location string `json:"location"`
	} `json:"errors"`
}

func GenerateHTMLDiffInternal(body string, text1, text2 string) (string, error) {
	diffHTML := diffInternal(text1, text2)
	body = strings.ReplaceAll(body, "\n", "<br>\n")
	body = fmt.Sprintf("<html><head></head><body>%s<br><br>\n%s</body></html>", body, diffHTML)
	return body, nil
}

func GenerateHTMLDiffGit(body string, text1, text2 string) (string, error) {
	diff, err := diffGit(text1, text2)
	if err != nil {
		return "", err
	}
	diffCSS, diffHTML, err := convertGitDiffToHTML(string(diff))
	if err != nil {
		return "", err
	}
	body = strings.ReplaceAll(body, "\n", "<br>\n")
	body = fmt.Sprintf("<html><head><style>%s</style></head><body>%s<br><br>\n%s</body></html>", diffCSS, body, diffHTML)
	return body, nil
}

func GenerateHTMLDiffAPI(httpClient *http2.Client, body string, text1, text2 string) (string, error) {
	diffCSS, diffHTML, err := diffAPI(httpClient, text1, text2)
	if err != nil {
		return "", err
	}
	body = strings.ReplaceAll(body, "\n", "<br>\n")
	body = fmt.Sprintf("<html><head><style>%s</style></head><body>%s<br><br>\n%s</body></html>", diffCSS, body, diffHTML)
	return body, nil
}

func diffInternal(text1, text2 string) []byte {
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(text1, text2, false)
	htmlDiff := dmp.DiffPrettyHtml(diffs)
	return []byte(htmlDiff)
}

func diffGit(text1, text2 string) ([]byte, error) {
	tmpdir := path.Join(os.TempDir(), fmt.Sprintf("websitewatcher_%s", helper.RandStringRunes(10))) // nolint:gomnd
	err := os.Mkdir(tmpdir, os.ModePerm)
	if err != nil {
		return nil, fmt.Errorf("could not create temp dir %q: %w", tmpdir, err)
	}
	defer os.RemoveAll(tmpdir)

	inputFile1, err := os.CreateTemp(tmpdir, "")
	if err != nil {
		return nil, fmt.Errorf("could not create inputFile1: %w", err)
	}
	defer os.Remove(inputFile1.Name())
	if _, err := inputFile1.WriteString(fmt.Sprintf("%s\n", text1)); err != nil { // add a newline at the end so git does not complain
		return nil, fmt.Errorf("could not write inputFile1: %w", err)
	}

	inputFile2, err := os.CreateTemp(tmpdir, "")
	if err != nil {
		return nil, fmt.Errorf("could not create inputFile2: %w", err)
	}
	defer os.Remove(inputFile2.Name())
	if _, err := inputFile2.WriteString(fmt.Sprintf("%s\n", text2)); err != nil { // add a newline at the end so git does not complain
		return nil, fmt.Errorf("could not write inputFile2: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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

const gitDiffCss = `
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

func convertGitDiffToHTML(input string) (string, string, error) {
	scanner := bufio.NewScanner(strings.NewReader(input))
	builder := strings.Builder{}

	re := regexp.MustCompile(`^index [A-Fa-f0-9]+\.\.[A-Fa-f0-9]+ [0-9]+$`)

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
		} else if re.MatchString(text) {
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

		if _, err := builder.WriteString(fmt.Sprintf(`<div class="%s">%s</div>`, classname, text)); err != nil {
			return "", "", err
		}
	}

	if err := scanner.Err(); err != nil {
		return "", "", err
	}

	inner := builder.String()
	html := fmt.Sprintf(`<div class="container">%s</div>`, inner)

	return gitDiffCss, html, nil
}

func diffAPI(client *http2.Client, text1, text2 string) (string, string, error) {
	// 	curl --location --request POST 'https://api.diffchecker.com/public/text?output_type=html&email=YOUR_EMAIL' \
	// --header 'Content-Type: application/json' \
	// --data-raw '{
	//     "left": "roses are red\nviolets are blue",
	//     "right": "roses are green\nviolets are purple",
	//     "diff_level": "word"
	// }'
	// url := "https://api.diffchecker.com/public/text?output_type=html_json&email=api%40mailinator.com&diff_level=character"

	u, err := url.Parse("https://api.diffchecker.com/public/text")
	if err != nil {
		return "", "", err
	}
	q := u.Query()
	q.Add("output_type", "html_json")
	q.Add("email", gofakeit.Email())
	q.Add("diff_level", "character")
	u.RawQuery = q.Encode()

	j := api{
		Left:  text1,
		Right: text2,
	}
	jsonStr, err := json.Marshal(j)
	if err != nil {
		return "", "", fmt.Errorf("could not marshal data: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, u.String(), bytes.NewBuffer(jsonStr))
	if err != nil {
		return "", "", fmt.Errorf("error on diff http creation: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("error on diff http: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("error on diff body read: %w", err)
	}

	var jsonResp apiResponse
	if err := json.Unmarshal(body, &jsonResp); err != nil {
		return "", "", fmt.Errorf("could not unmarshal: %w", err)
	}

	if jsonResp.Error != nil {
		return "", "", fmt.Errorf("error on calling Diff API: %s - %s", jsonResp.Error.Code, jsonResp.Error.Message)
	}

	if len(jsonResp.Errors) > 0 {
		msg := "Error on calling Diff API:"
		for _, err := range jsonResp.Errors {
			msg = fmt.Sprintf("%s - Message: %s Location: %s Param: %s", msg, err.Message, err.Location, err.Param)
		}
		return "", "", fmt.Errorf(msg)
	}

	return jsonResp.CSS, jsonResp.HTML, nil
}
