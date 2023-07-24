package diff

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"

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

func DiffLocal(text1, text2 string) []byte {
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(text1, text2, false)
	htmlDiff := dmp.DiffPrettyHtml(diffs)
	return []byte(htmlDiff)
}

func LocalDiff() {
	// git diff --no-index file1.txt file2.txt
}

func DiffAPI(client *http2.HTTPClient, text1, text2 string) (string, string, error) {
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
