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
		Server   string `json:"server"`
		Port     int    `json:"port"`
		From     string `json:"rom"`
		To       string `json:"to"`
		User     string `json:"user"`
		Password string `json:"password"`
		SkipTLS  bool   `json:"skiptls"`
	} `json:"mail"`
	Useragent string   `json:"useragent"`
	Timeout   Duration `json:"timeout"`
	Database  string   `json:"database"`
	Watches   []struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	} `json:"watches"`
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
	c := Configuration{}
	if err = decoder.Decode(&c); err != nil {
		return nil, err
	}
	return &c, nil
}
