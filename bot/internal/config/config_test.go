package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const validSettings = `{
  "backend": {
    "url": "http://127.0.0.1:8081",
    "location_path": "/api/v1/guide/location",
    "timeout": "10s",
    "retries": 2,
    "retry_backoff": "300ms",
    "max_response_bytes": 1048576,
    "max_error_body_bytes": 4096
  },
  "telegram": {
    "update_timeout": 60
  },
  "log": {
    "level": "info"
  }
}`

func writeSettings(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "bot.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write settings file: %v", err)
	}

	return path
}

func clearEnv(t *testing.T) {
	t.Helper()

	t.Setenv("BOT_SETTINGS_FILE", "")
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("BACKEND_URL", "")
	t.Setenv("LOG_LEVEL", "")
}

func TestLoadSettings(t *testing.T) {
	clearEnv(t)
	t.Setenv("BOT_SETTINGS_FILE", writeSettings(t, validSettings))
	t.Setenv("TELEGRAM_BOT_TOKEN", "test-token")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.TelegramBotToken != "test-token" {
		t.Fatalf("unexpected token: %q", cfg.TelegramBotToken)
	}

	if cfg.Backend.URL != "http://127.0.0.1:8081" {
		t.Fatalf("unexpected backend URL: %q", cfg.Backend.URL)
	}

	if cfg.Backend.Timeout.Std() != 10*time.Second {
		t.Fatalf("unexpected timeout: %s", cfg.Backend.Timeout.Std())
	}

	if cfg.Backend.Retries != 2 {
		t.Fatalf("unexpected retries: %d", cfg.Backend.Retries)
	}

	if cfg.Backend.RetryBackoff.Std() != 300*time.Millisecond {
		t.Fatalf("unexpected retry backoff: %s", cfg.Backend.RetryBackoff.Std())
	}

	if cfg.Telegram.UpdateTimeout != 60 {
		t.Fatalf("unexpected update timeout: %d", cfg.Telegram.UpdateTimeout)
	}

	if cfg.Log.Level != "info" {
		t.Fatalf("unexpected log level: %q", cfg.Log.Level)
	}
}

func TestLoadAppliesEnvOverrides(t *testing.T) {
	clearEnv(t)
	t.Setenv("BOT_SETTINGS_FILE", writeSettings(t, validSettings))
	t.Setenv("BACKEND_URL", "http://127.0.0.1:9999")
	t.Setenv("LOG_LEVEL", "debug")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Backend.URL != "http://127.0.0.1:9999" {
		t.Fatalf("expected env override for backend URL, got %q", cfg.Backend.URL)
	}

	if cfg.Log.Level != "debug" {
		t.Fatalf("expected env override for log level, got %q", cfg.Log.Level)
	}
}

func TestLoadRejectsInvalidSettings(t *testing.T) {
	invalid := `{
	  "backend": {
	    "url": "http://127.0.0.1:8081",
	    "location_path": "/api/v1/guide/location",
	    "timeout": "10s",
	    "retries": 11,
	    "retry_backoff": "300ms",
	    "max_response_bytes": 1048576,
	    "max_error_body_bytes": 4096
	  },
	  "telegram": {
	    "update_timeout": 60
	  },
	  "log": {
	    "level": "info"
	  }
	}`

	clearEnv(t)
	t.Setenv("BOT_SETTINGS_FILE", writeSettings(t, invalid))

	if _, err := Load(); err == nil {
		t.Fatal("expected validation error for retries > 10")
	}
}
