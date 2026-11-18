package settings

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

type testSettings struct {
	Timeout Duration `json:"timeout"`
}

func writeSettingsFile(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write settings file: %v", err)
	}

	return path
}

func TestLoad(t *testing.T) {
	path := writeSettingsFile(t, `{"timeout":"5s"}`)

	var cfg testSettings
	if err := Load(path, &cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Timeout.Std() != 5*time.Second {
		t.Fatalf("expected 5s, got %s", cfg.Timeout.Std())
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	path := writeSettingsFile(t, `{"timeout":"5s","unknown":1}`)

	var cfg testSettings
	if err := Load(path, &cfg); err == nil {
		t.Fatal("expected error for unknown field")
	}
}

func TestLoadRejectsInvalidDuration(t *testing.T) {
	path := writeSettingsFile(t, `{"timeout":"soon"}`)

	var cfg testSettings
	if err := Load(path, &cfg); err == nil {
		t.Fatal("expected error for invalid duration")
	}
}

func TestLoadRejectsTrailingData(t *testing.T) {
	path := writeSettingsFile(t, `{"timeout":"5s"}{}`)

	var cfg testSettings
	if err := Load(path, &cfg); err == nil {
		t.Fatal("expected error for trailing data")
	}
}

func TestLoadMissingFile(t *testing.T) {
	var cfg testSettings

	if err := Load(filepath.Join(t.TempDir(), "missing.json"), &cfg); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadEmptyPath(t *testing.T) {
	var cfg testSettings

	if err := Load("", &cfg); err == nil {
		t.Fatal("expected error for empty path")
	}
}

func unsetenv(t *testing.T, key string) {
	t.Helper()

	value, existed := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("failed to unset %s: %v", key, err)
	}

	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(key, value)
			return
		}

		_ = os.Unsetenv(key)
	})
}

func TestLoadEnvFile(t *testing.T) {
	unsetenv(t, "SOPHYGO_TEST_VALUE")
	unsetenv(t, "SOPHYGO_TEST_QUOTED")

	path := writeSettingsFile(t, "# comment\nSOPHYGO_TEST_VALUE=hello\nexport SOPHYGO_TEST_QUOTED=\"quoted value\"\n")

	if err := LoadEnvFile(path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := os.Getenv("SOPHYGO_TEST_VALUE"); got != "hello" {
		t.Fatalf("expected hello, got %q", got)
	}

	if got := os.Getenv("SOPHYGO_TEST_QUOTED"); got != "quoted value" {
		t.Fatalf("expected quoted value, got %q", got)
	}
}

func TestLoadEnvFileDoesNotOverrideExistingValue(t *testing.T) {
	t.Setenv("SOPHYGO_TEST_VALUE", "from-env")

	path := writeSettingsFile(t, "SOPHYGO_TEST_VALUE=from-file\n")

	if err := LoadEnvFile(path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := os.Getenv("SOPHYGO_TEST_VALUE"); got != "from-env" {
		t.Fatalf("expected environment value to win, got %q", got)
	}
}

func TestLoadEnvFileMissingFileIsIgnored(t *testing.T) {
	if err := LoadEnvFile(filepath.Join(t.TempDir(), "missing.env")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadEnvFileRejectsInvalidLine(t *testing.T) {
	path := writeSettingsFile(t, "INVALID LINE\n")

	if err := LoadEnvFile(path); err == nil {
		t.Fatal("expected error for invalid line")
	}
}
