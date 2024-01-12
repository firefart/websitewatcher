package config

import (
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/firefart/websitewatcher/internal/helper"
	"github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
)

type Configuration struct {
	Mail                    MailConfig    `koanf:"mail"`
	Retry                   RetryConfig   `koanf:"retry"`
	DiffMethod              string        `koanf:"diff_method"`
	Useragent               string        `koanf:"useragent"`
	Timeout                 time.Duration `koanf:"timeout"`
	Database                string        `koanf:"database"`
	NoErrorMailOnStatusCode []int         `koanf:"no_errormail_on_statuscode"`
	RetryOnMatch            []string      `koanf:"retry_on_match"`
	Watches                 []WatchConfig `koanf:"watches"`
}

type MailConfig struct {
	Server string `koanf:"server"`
	Port   int    `koanf:"port"`
	From   struct {
		Name string `koanf:"name"`
		Mail string `koanf:"mail"`
	} `koanf:"from"`
	To       []string `koanf:"to"`
	User     string   `koanf:"user"`
	Password string   `koanf:"password"`
	SkipTLS  bool     `koanf:"skiptls"`
	Retries  int      `koanf:"retries"`
}

type RetryConfig struct {
	Count int           `koanf:"count"`
	Delay time.Duration `koanf:"delay"`
}

type WatchConfig struct {
	Cron                    string            `koanf:"cron"`
	Name                    string            `koanf:"name"`
	Description             string            `koanf:"description"`
	URL                     string            `koanf:"url"`
	Method                  string            `koanf:"method"`
	Body                    string            `koanf:"body"`
	Header                  map[string]string `koanf:"header"`
	AdditionalTo            []string          `koanf:"additional_to"`
	NoErrorMailOnStatusCode []int             `koanf:"no_errormail_on_statuscode"`
	Disabled                bool              `koanf:"disabled"`
	Pattern                 string            `koanf:"pattern"`
	Replaces                []ReplaceConfig   `koanf:"replaces"`
	RetryOnMatch            []string          `koanf:"retry_on_match"`
	SkipSofterrorPatterns   bool              `koanf:"skip_soft_error_patterns"`
	JQ                      string            `koanf:"jq"`
}

type ReplaceConfig struct {
	Pattern     string `koanf:"pattern"`
	ReplaceWith string `koanf:"replace_with"`
}

var defaultConfig = Configuration{
	Retry: RetryConfig{
		Count: 3,
		Delay: 3 * time.Second,
	},
	Database: "db.sqlite3",
	Mail: MailConfig{
		Retries: 3,
	},
	DiffMethod: "git",
}

func GetConfig(f string) (Configuration, error) {
	var k = koanf.NewWithConf(koanf.Conf{
		Delim: ".",
	})

	if err := k.Load(structs.Provider(defaultConfig, "koanf"), nil); err != nil {
		return Configuration{}, fmt.Errorf("could ont load default config: %v", err)
	}

	if err := k.Load(file.Provider(f), json.Parser()); err != nil {
		return Configuration{}, fmt.Errorf("could not load config: %v", err)
	}

	var config Configuration
	if err := k.Unmarshal("", &config); err != nil {
		return Configuration{}, err
	}

	// set some defaults for watches if not set in json
	for i, watch := range config.Watches {
		if watch.Method == "" {
			config.Watches[i].Method = http.MethodGet
		}
		// default to hourly checks
		if watch.Cron == "" {
			config.Watches[i].Cron = "@hourly"
		}
	}

	// check some stuff
	if config.Mail.Server == "" {
		return Configuration{}, fmt.Errorf("please supply an email server")
	}
	if config.DiffMethod != "api" && config.DiffMethod != "git" && config.DiffMethod != "internal" {
		return Configuration{}, fmt.Errorf("invalid diff method %q", config.DiffMethod)
	}
	if config.DiffMethod == "git" && !helper.IsGitInstalled() {
		return Configuration{}, fmt.Errorf("diff mode git requires git to be installed")
	}

	// check for uniqueness
	var tmpArray []string
	for _, wc := range config.Watches {
		key := fmt.Sprintf("%s%s", wc.Name, wc.URL)
		if slices.Contains(tmpArray, key) {
			return Configuration{}, fmt.Errorf("name and url combinations need to be unique. Please use another name or url for entry %s", wc.Name)
		}
		tmpArray = append(tmpArray, key)
	}

	return config, nil
}
