package diff

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	http2 "github.com/firefart/websitewatcher/internal/http"
	"github.com/sergi/go-diff/diffmatchpatch"
)

type api struct {
	Left  string `json:"left"`
	Right string `json:"right"`
}

type apiResponse struct {
	HTML string `json:"html"`
	CSS  string `json:"css"`
}

func DiffLocal(text1, text2 string) []byte {
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(text1, text2, false)
	htmlDiff := dmp.DiffPrettyHtml(diffs)
	return []byte(htmlDiff)
}

func DiffAPI(client *http2.HTTPClient, text1, text2 string) (string, string, error) {
	// 	curl --location --request POST 'https://api.diffchecker.com/public/text?output_type=html&email=YOUR_EMAIL' \
	// --header 'Content-Type: application/json' \
	// --data-raw '{
	//     "left": "roses are red\nviolets are blue",
	//     "right": "roses are green\nviolets are purple",
	//     "diff_level": "word"
	// }'

	url := "https://api.diffchecker.com/public/text?output_type=html_json&email=api%40mailinator.com&diff_level=character"

	j := api{
		Left:  text1,
		Right: text2,
	}
	jsonStr, err := json.Marshal(j)
	if err != nil {
		return "", "", fmt.Errorf("could not marshal data: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(jsonStr))
	if err != nil {
		return "", "", fmt.Errorf("error on diff http creation: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("error on diff http: %w", err)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("error on diff body read: %w", err)
	}

	var jsonResp apiResponse
	if err := json.Unmarshal(body, &jsonResp); err != nil {
		return "", "", fmt.Errorf("could not unmarshal: %w", err)
	}

	return jsonResp.CSS, jsonResp.HTML, nil
}
