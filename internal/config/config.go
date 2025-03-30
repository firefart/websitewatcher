package config

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/firefart/websitewatcher/internal/helper"
	"github.com/hashicorp/go-multierror"
	"github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"

	"github.com/go-playground/validator/v10"
	"github.com/itchyny/gojq"
)

const DefaultUseragent = "websitewatcher / https://github.com/firefart/websitewatcher"

type Configuration struct {
	Mail                    MailConfig    `koanf:"mail"`
	Proxy                   *ProxyConfig  `koanf:"proxy"`
	Retry                   RetryConfig   `koanf:"retry"`
	Useragent               string        `koanf:"useragent"`
	Timeout                 time.Duration `koanf:"timeout"`
	Database                string        `koanf:"database" validate:"required"`
	NoErrorMailOnStatusCode []int         `koanf:"no_errormail_on_statuscode" validate:"dive,gte=100,lte=999"`
	RetryOnMatch            []string      `koanf:"retry_on_match"`
	Watches                 []WatchConfig `koanf:"watches" validate:"dive"`
	GracefulTimeout         time.Duration `koanf:"graceful_timeout"`
	Location                string        `koanf:"location" validate:"omitempty,timezone"`
}

type ProxyConfig struct {
	URL      string `koanf:"url" validate:"omitempty,url"`
	Username string `koanf:"username" validate:"required_with=Password"`
	Password string `koanf:"password" validate:"required_with=Username"`
	NoProxy  string `koanf:"no_proxy"`
}

type MailConfig struct {
	Server string `koanf:"server" validate:"required"`
	Port   int    `koanf:"port" validate:"required,gt=0,lte=65535"`
	From   struct {
		Name string `koanf:"name" validate:"required"`
		Mail string `koanf:"mail" validate:"required,email"`
	} `koanf:"from"`
	To       []string      `koanf:"to" validate:"required,dive,email"`
	User     string        `koanf:"user"`
	Password string        `koanf:"password"`
	TLS      bool          `koanf:"tls"`
	StartTLS bool          `koanf:"starttls"`
	SkipTLS  bool          `koanf:"skiptls"`
	Retries  int           `koanf:"retries" validate:"required"`
	Timeout  time.Duration `koanf:"timeout"`
}

type RetryConfig struct {
	Count int           `koanf:"count" validate:"required"`
	Delay time.Duration `koanf:"delay" validate:"required"`
}

type WatchConfig struct {
	Cron                    string            `koanf:"cron" validate:"required,cron"`
	Name                    string            `koanf:"name" validate:"required"`
	Description             string            `koanf:"description"`
	URL                     string            `koanf:"url" validate:"required,url"`
	Method                  string            `koanf:"method" validate:"required,uppercase"`
	Body                    string            `koanf:"body"`
	Header                  map[string]string `koanf:"header"`
	AdditionalTo            []string          `koanf:"additional_to" validate:"dive,email"`
	NoErrorMailOnStatusCode []int             `koanf:"no_errormail_on_statuscode" validate:"dive,gte=100,lte=999"`
	Disabled                bool              `koanf:"disabled"`
	Pattern                 string            `koanf:"pattern"`
	Replaces                []ReplaceConfig   `koanf:"replaces" validate:"dive"`
	RetryOnMatch            []string          `koanf:"retry_on_match"`
	SkipSofterrorPatterns   bool              `koanf:"skip_soft_error_patterns"`
	JQ                      string            `koanf:"jq"`
	Useragent               string            `koanf:"useragent"`
	RemoveEmptyLines        bool              `koanf:"remove_empty_lines"`
	TrimWhitespace          bool              `koanf:"trim_whitespace"`
	Webhooks                []WebhookConfig   `koanf:"webhooks" validate:"dive"`
}

type WebhookConfig struct {
	URL       string            `koanf:"url" validate:"required,url"`
	Header    map[string]string `koanf:"header"`
	Method    string            `koanf:"method" validate:"required,uppercase,oneof=GET POST PUT PATCH DELETE"`
	Useragent string            `koanf:"useragent"`
}

type ReplaceConfig struct {
	Pattern     string `koanf:"pattern" validate:"required"`
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
		Timeout: 10 * time.Second,
	},
	GracefulTimeout: 5 * time.Second,
	Useragent:       DefaultUseragent,
}

func GetConfig(f string) (Configuration, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())

	k := koanf.NewWithConf(koanf.Conf{
		Delim: ".",
	})

	if err := k.Load(structs.Provider(defaultConfig, "koanf"), nil); err != nil {
		return Configuration{}, fmt.Errorf("could ont load default config: %w", err)
	}

	if err := k.Load(file.Provider(f), json.Parser()); err != nil {
		return Configuration{}, fmt.Errorf("could not load config: %w", err)
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

	if err := validate.Struct(config); err != nil {
		var invalidValidationError *validator.InvalidValidationError
		if errors.As(err, &invalidValidationError) {
			return Configuration{}, err
		}

		var resultErr error
		for _, err := range err.(validator.ValidationErrors) {
			resultErr = multierror.Append(resultErr, err)
		}
		return Configuration{}, resultErr
	}

	if !helper.IsGitInstalled() {
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

	// check for valid jq filters
	for _, wc := range config.Watches {
		if wc.JQ != "" {
			_, err := gojq.Parse(wc.JQ)
			if err != nil {
				return Configuration{}, fmt.Errorf("invalid jq filter %s: %w", wc.JQ, err)
			}
		}
	}

	return config, nil
}
