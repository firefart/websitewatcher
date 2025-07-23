package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReplaceConfig(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		configJSON  string
		wantErr     bool
		errContains string
	}{
		"Valid replace config": {
			configJSON: `{
				"database": "test.db",
				"mail": {
					"server": "smtp.example.com",
					"port": 587,
					"from": {
						"name": "Test",
						"mail": "test@example.com"
					},
					"to": ["recipient@example.com"],
					"retries": 3
				},
				"watches": [{
					"name": "test",
					"url": "https://example.com",
					"cron": "@hourly",
					"method": "GET",
					"replaces": [{
						"pattern": "old_text",
						"replace_with": "new_text"
					}]
				}]
			}`,
			wantErr: false,
		},
		"Valid replace config with empty replace_with": {
			configJSON: `{
				"database": "test.db",
				"mail": {
					"server": "smtp.example.com",
					"port": 587,
					"from": {
						"name": "Test",
						"mail": "test@example.com"
					},
					"to": ["recipient@example.com"],
					"retries": 3
				},
				"watches": [{
					"name": "test",
					"url": "https://example.com",
					"cron": "@hourly",
					"method": "GET",
					"replaces": [{
						"pattern": "remove_this",
						"replace_with": ""
					}]
				}]
			}`,
			wantErr: false,
		},
		"Valid replace config with multiple replaces": {
			configJSON: `{
				"database": "test.db",
				"mail": {
					"server": "smtp.example.com",
					"port": 587,
					"from": {
						"name": "Test",
						"mail": "test@example.com"
					},
					"to": ["recipient@example.com"],
					"retries": 3
				},
				"watches": [{
					"name": "test",
					"url": "https://example.com",
					"cron": "@hourly",
					"method": "GET",
					"replaces": [
						{
							"pattern": "pattern1",
							"replace_with": "replacement1"
						},
						{
							"pattern": "pattern2",
							"replace_with": "replacement2"
						}
					]
				}]
			}`,
			wantErr: false,
		},
		"Invalid replace config - missing pattern": {
			configJSON: `{
				"database": "test.db",
				"mail": {
					"server": "smtp.example.com",
					"port": 587,
					"from": {
						"name": "Test",
						"mail": "test@example.com"
					},
					"to": ["recipient@example.com"],
					"retries": 3
				},
				"watches": [{
					"name": "test",
					"url": "https://example.com",
					"cron": "@hourly",
					"method": "GET",
					"replaces": [{
						"replace_with": "new_text"
					}]
				}]
			}`,
			wantErr:     true,
			errContains: "Pattern",
		},
		"Invalid replace config - empty pattern": {
			configJSON: `{
				"database": "test.db",
				"mail": {
					"server": "smtp.example.com",
					"port": 587,
					"from": {
						"name": "Test",
						"mail": "test@example.com"
					},
					"to": ["recipient@example.com"],
					"retries": 3
				},
				"watches": [{
					"name": "test",
					"url": "https://example.com",
					"cron": "@hourly",
					"method": "GET",
					"replaces": [{
						"pattern": "",
						"replace_with": "new_text"
					}]
				}]
			}`,
			wantErr:     true,
			errContains: "Pattern",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Create temporary config file
			tmpDir := t.TempDir()
			configFile := filepath.Join(tmpDir, "config.json")
			err := os.WriteFile(configFile, []byte(tc.configJSON), 0o644)
			require.NoError(t, err)

			// Test GetConfig
			config, err := GetConfig(t.Context(), configFile)

			if tc.wantErr {
				require.Error(t, err)
				if tc.errContains != "" {
					require.Contains(t, err.Error(), tc.errContains)
				}
				return
			}

			require.NoError(t, err)
			require.NotEmpty(t, config.Watches)

			// Verify replace configs are properly loaded
			if len(config.Watches[0].Replaces) > 0 {
				for _, replace := range config.Watches[0].Replaces {
					require.NotEmpty(t, replace.Pattern, "Pattern should not be empty")
					// replace_with can be empty (valid use case for removal)
				}
			}
		})
	}
}
