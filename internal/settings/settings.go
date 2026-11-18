// Package settings provides loading of JSON settings files shared by the bot
// and the backend.
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"
)

// Duration is a time.Duration that is unmarshalled from a human-readable
// string such as "5s" or "300ms".
type Duration time.Duration

// UnmarshalJSON implements json.Unmarshaler.
func (d *Duration) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("duration must be a string: %w", err)
	}

	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", raw, err)
	}

	*d = Duration(parsed)

	return nil
}

// Std returns the standard library time.Duration value.
func (d Duration) Std() time.Duration {
	return time.Duration(d)
}

// Load reads a JSON settings file into v. Unknown fields are rejected so that
// typos fail fast instead of being silently ignored.
func Load(path string, v any) error {
	if path == "" {
		return errors.New("settings file path is empty")
	}

	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open settings file: %w", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(v); err != nil {
		return fmt.Errorf("decode settings file %s: %w", path, err)
	}

	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("decode settings file %s: unexpected trailing data", path)
	}

	return nil
}

// Logger builds a slog logger for the given level. Supported levels are
// "debug", "info", "warn" and "error"; any other value falls back to "info".
func Logger(level string) *slog.Logger {
	var opts slog.HandlerOptions

	switch level {
	case "debug":
		opts.Level = slog.LevelDebug
	case "warn":
		opts.Level = slog.LevelWarn
	case "error":
		opts.Level = slog.LevelError
	default:
		opts.Level = slog.LevelInfo
	}

	return slog.New(slog.NewTextHandler(os.Stdout, &opts))
}
