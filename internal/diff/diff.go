package diff

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	http2 "github.com/firefart/websitewatcher/internal/http"
	"github.com/sergi/go-diff/diffmatchpatch"
)

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func randStringRunes(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}

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

func GenerateHTMLDiffLocal(body string, text1, text2 string) (string, error) {
	diff, err := diffLocal(text1, text2)
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

func GenerateHTMLDiffAPI(httpClient *http2.HTTPClient, body string, text1, text2 string) (string, error) {
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

func diffLocal(text1, text2 string) ([]byte, error) {
	// git diff --no-color --no-index --text -w -b --output=out.txt test1.html test2.html
	tmpdir := path.Join(os.TempDir(), fmt.Sprintf("websitewatcher_%s", randStringRunes(10))) // nolint:gomnd
	err := os.Mkdir(tmpdir, os.ModePerm)
	if err != nil {
		return nil, fmt.Errorf("could not create temp dir %q: %w", tmpdir, err)
	}
	defer os.RemoveAll(tmpdir)

	inputFile1, err := ioutil.TempFile(tmpdir, "")
	if err != nil {
		return nil, fmt.Errorf("could not create inputFile1: %w", err)
	}
	defer os.Remove(inputFile1.Name())
	if _, err := inputFile1.WriteString(text1); err != nil {
		return nil, fmt.Errorf("could not write inputFile1: %w", err)
	}

	inputFile2, err := ioutil.TempFile(tmpdir, "")
	if err != nil {
		return nil, fmt.Errorf("could not create inputFile2: %w", err)
	}
	defer os.Remove(inputFile2.Name())
	if _, err := inputFile2.WriteString(text2); err != nil {
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
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode := exitError.ExitCode()
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

func convertGitDiffToHTML(input string) (string, string, error) {
	scanner := bufio.NewScanner(strings.NewReader(input))
	builder := strings.Builder{}

	for scanner.Scan() {
		text := scanner.Text()
		firstRune, _ := utf8.DecodeRuneInString(text)
		classname := "default"
		if firstRune == '-' {
			classname = "delete"
		} else if firstRune == '+' {
			classname = "add"
		}
		if _, err := builder.WriteString(fmt.Sprintf(`<div class="%s">%s</div>`, classname, text)); err != nil {
			return "", "", err
		}
	}

	if err := scanner.Err(); err != nil {
		return "", "", err
	}

	css := `
		div {
			font-family: monospace;
		}
		div.default {}
		div.add {
				background-color: #c8f0da;
		}
		div.delete {
				background-color: #ffcbbd;
		}
`

	return css, builder.String(), nil
}

func diffAPI(client *http2.HTTPClient, text1, text2 string) (string, string, error) {
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
	q.Add("email", generateRandomEmail())
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
		return "", "", fmt.Errorf("Error on calling Diff API: %s - %s", jsonResp.Error.Code, jsonResp.Error.Message)
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

func generateRandomEmail() string {
	// https://en.wikipedia.org/wiki/List_of_most_popular_given_names
	givenNames := []string{"James", "John", "Robert", "Michael", "William", "David", "Richard", "Charles", "Joseph", "Thomas", "Liam", "Noah", "Oliver", "Elijah", "Henry", "Lucas", "Benjamin", "Theodore"}
	// https://www.thoughtco.com/most-common-us-surnames-1422656
	lastNames := []string{"Smith", "Johnson", "Williams", "Brown", "Jones", "Garcia", "Miller", "Davis", "Rodriguez"}
	domains := []string{"ridteam.com", "mailinator.com"}
	minNumber := 1
	maxNumber := 99
	number := rand.Intn(maxNumber-minNumber) + minNumber
	return fmt.Sprintf(
		"%s.%s%d@%s",
		strings.ToLower(givenNames[rand.Intn(len(givenNames))]),
		strings.ToLower(lastNames[rand.Intn(len(lastNames))]),
		number,
		strings.ToLower(domains[rand.Intn(len(domains))]),
	)
}
