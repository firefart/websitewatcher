package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

type Duration struct {
	time.Duration
}

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch value := v.(type) {
	case float64:
		d.Duration = time.Duration(value)
		return nil
	case string:
		var err error
		d.Duration, err = time.ParseDuration(value)
		if err != nil {
			return err
		}
		return nil
	default:
		return errors.New("invalid duration")
	}
}

type Configuration struct {
	Mail struct {
		Server string `json:"server"`
		Port   int    `json:"port"`
		From   struct {
			Name string `json:"name"`
			Mail string `json:"mail"`
		} `json:"from"`
		To       []string `json:"to"`
		User     string   `json:"user"`
		Password string   `json:"password"`
		SkipTLS  bool     `json:"skiptls"`
	} `json:"mail"`
	Retries            int       `json:"retries"`
	RetryDelay         *Duration `json:"retry_delay"`
	ParallelChecks     int64     `json:"parallel_checks"`
	Useragent          string    `json:"useragent"`
	Timeout            Duration  `json:"timeout"`
	Database           string    `json:"database"`
	HTTPErrorsToIgnore []int     `json:"http_errors_to_ignore"`
	Watches            []Watch   `json:"watches"`
}

type Watch struct {
	Name                         string    `json:"name"`
	URL                          string    `json:"url"`
	AdditionalTo                 []string  `json:"additional_to"`
	AdditionalHTTPErrorsToIgnore []int     `json:"additional_http_errors_to_ignore"`
	Disabled                     bool      `json:"disabled"`
	Pattern                      string    `json:"pattern"`
	Replaces                     []Replace `json:"replaces"`
}

type Replace struct {
	Pattern     string `json:"pattern"`
	ReplaceWith string `json:"replace_with"`
}

func GetConfig(f string) (*Configuration, error) {
	if f == "" {
		return nil, fmt.Errorf("please provide a valid config file")
	}

	b, err := os.ReadFile(f) // nolint: gosec
	if err != nil {
		return nil, err
	}
	reader := bytes.NewReader(b)

	decoder := json.NewDecoder(reader)

	// set some defaults
	c := Configuration{
		ParallelChecks: 1,
		Retries:        3,
		RetryDelay:     &Duration{Duration: 3 * time.Second},
	}
	if err = decoder.Decode(&c); err != nil {
		var syntaxErr *json.SyntaxError
		var unmarshalErr *json.UnmarshalTypeError
		switch {
		case errors.As(err, &syntaxErr):
			custom := fmt.Sprintf("%q <-", string(b[syntaxErr.Offset-20:syntaxErr.Offset]))
			return nil, fmt.Errorf("could not parse JSON: %v: %s", syntaxErr.Error(), custom)
		case errors.As(err, &unmarshalErr):
			custom := fmt.Sprintf("%q <-", string(b[unmarshalErr.Offset-20:unmarshalErr.Offset]))
			return nil, fmt.Errorf("could not parse JSON: type %v cannot be converted into %v (%s.%v): %v: %s", unmarshalErr.Value, unmarshalErr.Type.Name(), unmarshalErr.Struct, unmarshalErr.Field, unmarshalErr.Error(), custom)
		default:
			return nil, err
		}
	}

	return &c, nil
}
