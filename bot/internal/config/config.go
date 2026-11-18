package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"travelbuddy/internal/settings"
)

const defaultSettingsFile = "bot/settings/bot.json"

type Backend struct {
	URL               string            `json:"url"`
	LocationPath      string            `json:"location_path"`
	Timeout           settings.Duration `json:"timeout"`
	Retries           int               `json:"retries"`
	RetryBackoff      settings.Duration `json:"retry_backoff"`
	MaxResponseBytes  int64             `json:"max_response_bytes"`
	MaxErrorBodyBytes int64             `json:"max_error_body_bytes"`
}

type AI struct {
	SessionPath     string            `json:"session_path"`
	SessionStopPath string            `json:"session_stop_path"`
	VoicePath       string            `json:"voice_path"`
	Timeout         settings.Duration `json:"timeout"`
	MaxVoiceBytes   int64             `json:"max_voice_bytes"`
}

type Telegram struct {
	UpdateTimeout int `json:"update_timeout"`
}

type Log struct {
	Level string `json:"level"`
}

type Config struct {
	// TelegramBotToken is a secret and is read from the environment only.
	TelegramBotToken string `json:"-"`

	Backend  Backend  `json:"backend"`
	AI       AI       `json:"ai"`
	Telegram Telegram `json:"telegram"`
	Log      Log      `json:"log"`
}

// Load reads bot settings from a JSON file and applies environment overrides.
// The settings file path can be changed with BOT_SETTINGS_FILE.
func Load() (*Config, error) {
	path := strings.TrimSpace(os.Getenv("BOT_SETTINGS_FILE"))
	if path == "" {
		path = defaultSettingsFile
	}

	cfg := &Config{}
	if err := settings.Load(path, cfg); err != nil {
		return nil, err
	}

	cfg.applyDefaults()

	cfg.TelegramBotToken = strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))
	if v := strings.TrimSpace(os.Getenv("BACKEND_URL")); v != "" {
		cfg.Backend.URL = v
	}
	if v := strings.TrimSpace(os.Getenv("LOG_LEVEL")); v != "" {
		cfg.Log.Level = v
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	return cfg, nil
}

func (c *Config) applyDefaults() {
	if c.AI.SessionPath == "" {
		c.AI.SessionPath = "/api/v1/ai/session"
	}
	if c.AI.SessionStopPath == "" {
		c.AI.SessionStopPath = "/api/v1/ai/session/stop"
	}
	if c.AI.VoicePath == "" {
		c.AI.VoicePath = "/api/v1/ai/voice"
	}
	if c.AI.Timeout.Std() <= 0 {
		c.AI.Timeout = settings.Duration(120 * time.Second)
	}
	if c.AI.MaxVoiceBytes <= 0 {
		c.AI.MaxVoiceBytes = 10 << 20
	}
}

// Validate checks that required settings are present and consistent.
func (c *Config) Validate() error {
	if strings.TrimSpace(c.Backend.URL) == "" {
		return errors.New("backend.url is required")
	}
	if !strings.HasPrefix(c.Backend.URL, "http://") && !strings.HasPrefix(c.Backend.URL, "https://") {
		return errors.New("backend.url must start with http:// or https://")
	}
	if !strings.HasPrefix(c.Backend.LocationPath, "/") {
		return errors.New("backend.location_path must start with /")
	}
	if c.Backend.Timeout.Std() <= 0 {
		return errors.New("backend.timeout must be positive")
	}
	if c.Backend.Retries < 0 {
		return errors.New("backend.retries must not be negative")
	}
	if c.Backend.Retries > 10 {
		return errors.New("backend.retries must not exceed 10")
	}
	if c.Backend.RetryBackoff.Std() < 0 {
		return errors.New("backend.retry_backoff must not be negative")
	}
	if c.Backend.MaxResponseBytes <= 0 {
		return errors.New("backend.max_response_bytes must be positive")
	}
	if c.Backend.MaxErrorBodyBytes <= 0 {
		return errors.New("backend.max_error_body_bytes must be positive")
	}
	if !strings.HasPrefix(c.AI.SessionPath, "/") {
		return errors.New("ai.session_path must start with /")
	}
	if !strings.HasPrefix(c.AI.SessionStopPath, "/") {
		return errors.New("ai.session_stop_path must start with /")
	}
	if !strings.HasPrefix(c.AI.VoicePath, "/") {
		return errors.New("ai.voice_path must start with /")
	}
	if c.AI.Timeout.Std() <= 0 {
		return errors.New("ai.timeout must be positive")
	}
	if c.AI.MaxVoiceBytes <= 0 {
		return errors.New("ai.max_voice_bytes must be positive")
	}
	if c.Telegram.UpdateTimeout <= 0 {
		return errors.New("telegram.update_timeout must be positive")
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
