package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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
	Retry struct {
		Count int       `json:"count"`
		Delay *Duration `json:"delay"`
	} `json:"retry"`
	ParallelChecks          int64         `json:"parallel_checks"`
	Useragent               string        `json:"useragent"`
	Timeout                 Duration      `json:"timeout"`
	Database                string        `json:"database"`
	NoErrorMailOnStatusCode []int         `json:"no_errormail_on_statuscode"`
	RetryOnMatch            []string      `json:"retry_on_match"`
	Watches                 []WatchConfig `json:"watches"`
}

type WatchConfig struct {
	Name                    string            `json:"name"`
	URL                     string            `json:"url"`
	Method                  string            `json:"method"`
	Body                    string            `json:"body"`
	Header                  map[string]string `json:"header"`
	AdditionalTo            []string          `json:"additional_to"`
	NoErrorMailOnStatusCode []int             `json:"no_errormail_on_statuscode"`
	Disabled                bool              `json:"disabled"`
	Pattern                 string            `json:"pattern"`
	Replaces                []ReplaceConfig   `json:"replaces"`
	RetryOnMatch            []string          `json:"retry_on_match"`
}

type ReplaceConfig struct {
	Pattern     string `json:"pattern"`
	ReplaceWith string `json:"replace_with"`
}

func GetConfig(f string) (Configuration, error) {
	if f == "" {
		return Configuration{}, fmt.Errorf("please provide a valid config file")
	}

	b, err := os.ReadFile(f) // nolint: gosec
	if err != nil {
		return Configuration{}, err
	}
	reader := bytes.NewReader(b)

	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()

	// set some defaults
	c := Configuration{
		ParallelChecks: 1,
	}
	c.Retry.Count = 3
	c.Retry.Delay = &Duration{Duration: 3 * time.Second}

	if err = decoder.Decode(&c); err != nil {
		var syntaxErr *json.SyntaxError
		var unmarshalErr *json.UnmarshalTypeError
		switch {
		case errors.As(err, &syntaxErr):
			custom := fmt.Sprintf("%q <-", string(b[syntaxErr.Offset-20:syntaxErr.Offset]))
			return Configuration{}, fmt.Errorf("could not parse JSON: %v: %s", syntaxErr.Error(), custom)
		case errors.As(err, &unmarshalErr):
			custom := fmt.Sprintf("%q <-", string(b[unmarshalErr.Offset-20:unmarshalErr.Offset]))
			return Configuration{}, fmt.Errorf("could not parse JSON: type %v cannot be converted into %v (%s.%v): %v: %s", unmarshalErr.Value, unmarshalErr.Type.Name(), unmarshalErr.Struct, unmarshalErr.Field, unmarshalErr.Error(), custom)
		default:
			return Configuration{}, err
		}
	}

	// set some defaults for watches if not set in json
	for i, watch := range c.Watches {
		if watch.Method == "" {
			c.Watches[i].Method = http.MethodGet
		}
	}

	return c, nil
}
