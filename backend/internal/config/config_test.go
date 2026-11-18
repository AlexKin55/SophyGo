package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const validSettings = `{
  "server": {
    "addr": "127.0.0.1:9999",
    "read_header_timeout": "2s",
    "shutdown_timeout": "3s"
  },
  "api": {
    "health_path": "/healthz",
    "location_path": "/api/v2/guide",
    "max_location_body_bytes": 1024,
    "ai_session_path": "/api/v1/ai/session",
    "ai_session_stop_path": "/api/v1/ai/session/stop",
    "ai_voice_path": "/api/v1/ai/voice"
  },
  "log": {
    "level": "debug"
  }
}`

func writeSettings(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "backend.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write settings file: %v", err)
	}

	return path
}

func clearEnv(t *testing.T) {
	t.Helper()

	t.Setenv("BACKEND_SETTINGS_FILE", "")
	t.Setenv("BACKEND_ADDR", "")
	t.Setenv("LOG_LEVEL", "")
}

func TestLoadSettings(t *testing.T) {
	clearEnv(t)
	t.Setenv("BACKEND_SETTINGS_FILE", writeSettings(t, validSettings))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Server.Addr != "127.0.0.1:9999" {
		t.Fatalf("unexpected addr: %q", cfg.Server.Addr)
	}

	if cfg.Server.ReadHeaderTimeout.Std() != 2*time.Second {
		t.Fatalf("unexpected read header timeout: %s", cfg.Server.ReadHeaderTimeout.Std())
	}

	if cfg.Server.ShutdownTimeout.Std() != 3*time.Second {
		t.Fatalf("unexpected shutdown timeout: %s", cfg.Server.ShutdownTimeout.Std())
	}

	if cfg.API.HealthPath != "/healthz" || cfg.API.LocationPath != "/api/v2/guide" {
		t.Fatalf("unexpected API paths: %+v", cfg.API)
	}

	if cfg.API.MaxLocationBodyBytes != 1024 {
		t.Fatalf("unexpected body limit: %d", cfg.API.MaxLocationBodyBytes)
	}

	if cfg.Log.Level != "debug" {
		t.Fatalf("unexpected log level: %q", cfg.Log.Level)
	}
}

func TestLoadAppliesEnvOverrides(t *testing.T) {
	clearEnv(t)
	t.Setenv("BACKEND_SETTINGS_FILE", writeSettings(t, validSettings))
	t.Setenv("BACKEND_ADDR", "0.0.0.0:7777")
	t.Setenv("LOG_LEVEL", "error")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Server.Addr != "0.0.0.0:7777" {
		t.Fatalf("expected env override for addr, got %q", cfg.Server.Addr)
	}

	if cfg.Log.Level != "error" {
		t.Fatalf("expected env override for log level, got %q", cfg.Log.Level)
	}
}

func TestLoadRejectsInvalidSettings(t *testing.T) {
	invalid := `{
	  "server": {
	    "addr": "",
	    "read_header_timeout": "2s",
	    "shutdown_timeout": "3s"
	  },
	  "api": {
	    "health_path": "/health",
	    "location_path": "/api/v1/guide/location",
	    "max_location_body_bytes": 1024,
	    "ai_session_path": "/api/v1/ai/session",
	    "ai_session_stop_path": "/api/v1/ai/session/stop",
	    "ai_voice_path": "/api/v1/ai/voice"
	  },
	  "log": {
	    "level": "info"
	  }
	}`

	clearEnv(t)
	t.Setenv("BACKEND_SETTINGS_FILE", writeSettings(t, invalid))

	if _, err := Load(); err == nil {
		t.Fatal("expected validation error for empty addr")
	}
}
