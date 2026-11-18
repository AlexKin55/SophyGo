package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"travelbuddy/backend/internal/ai"
	"travelbuddy/internal/settings"
)

const defaultSettingsFile = "backend/settings/backend.json"

type Server struct {
	Addr              string            `json:"addr"`
	ReadHeaderTimeout settings.Duration `json:"read_header_timeout"`
	ShutdownTimeout   settings.Duration `json:"shutdown_timeout"`
}

type API struct {
	HealthPath           string `json:"health_path"`
	LocationPath         string `json:"location_path"`
	MaxLocationBodyBytes int64  `json:"max_location_body_bytes"`

	AISessionPath     string `json:"ai_session_path"`
	AISessionStopPath string `json:"ai_session_stop_path"`
	AIVoicePath       string `json:"ai_voice_path"`
}

type Log struct {
	Level string `json:"level"`
}

type Config struct {
	Server Server    `json:"server"`
	API    API       `json:"api"`
	AI     ai.Config `json:"ai"`
	Log    Log       `json:"log"`
}

// Load reads backend settings from a JSON file and applies environment
// overrides. The settings file path can be changed with BACKEND_SETTINGS_FILE.
func Load() (*Config, error) {
	path := strings.TrimSpace(os.Getenv("BACKEND_SETTINGS_FILE"))
	if path == "" {
		path = defaultSettingsFile
	}

	cfg := &Config{}
	if err := settings.Load(path, cfg); err != nil {
		return nil, err
	}

	cfg.AI.ApplyDefaults()

	if v := strings.TrimSpace(os.Getenv("BACKEND_ADDR")); v != "" {
		cfg.Server.Addr = v
	}
	if v := strings.TrimSpace(os.Getenv("LOG_LEVEL")); v != "" {
		cfg.Log.Level = v
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	return cfg, nil
}

// Validate checks that required settings are present and consistent.
func (c *Config) Validate() error {
	if strings.TrimSpace(c.Server.Addr) == "" {
		return errors.New("server.addr is required")
	}
	if c.Server.ReadHeaderTimeout.Std() <= 0 {
		return errors.New("server.read_header_timeout must be positive")
	}
	if c.Server.ShutdownTimeout.Std() <= 0 {
		return errors.New("server.shutdown_timeout must be positive")
	}
	if !strings.HasPrefix(c.API.HealthPath, "/") {
		return errors.New("api.health_path must start with /")
	}
	if !strings.HasPrefix(c.API.LocationPath, "/") {
		return errors.New("api.location_path must start with /")
	}
	if c.API.MaxLocationBodyBytes <= 0 {
		return errors.New("api.max_location_body_bytes must be positive")
	}
	if !strings.HasPrefix(c.API.AISessionPath, "/") {
		return errors.New("api.ai_session_path must start with /")
	}
	if !strings.HasPrefix(c.API.AISessionStopPath, "/") {
		return errors.New("api.ai_session_stop_path must start with /")
	}
	if !strings.HasPrefix(c.API.AIVoicePath, "/") {
		return errors.New("api.ai_voice_path must start with /")
	}
	if err := c.AI.Validate(); err != nil {
		return err
	}
	if !validLogLevel(c.Log.Level) {
		return fmt.Errorf("log.level %q is not supported", c.Log.Level)
	}

	return nil
}

func validLogLevel(level string) bool {
	switch level {
	case "debug", "info", "warn", "error":
		return true
	default:
		return false
	}
}
