package ai

import (
	"testing"
	"time"

	"travelbuddy/internal/settings"
)

func TestConfigApplyDefaults(t *testing.T) {
	cfg := Config{Enabled: true}
	cfg.ApplyDefaults()

	if cfg.STT.Endpoint != DefaultSTTEndpoint || cfg.STT.Language != "ru-RU" {
		t.Fatalf("unexpected STT defaults: %+v", cfg.STT)
	}
	if cfg.GPT.Endpoint != DefaultGPTEndpoint || cfg.GPT.Model != DefaultGPTModel {
		t.Fatalf("unexpected GPT defaults: %+v", cfg.GPT)
	}
	if cfg.TTS.Endpoint != DefaultTTSEndpoint || cfg.TTS.Voice != DefaultVoice {
		t.Fatalf("unexpected TTS defaults: %+v", cfg.TTS)
	}
	if cfg.TTS.Format != DefaultTTSFormat {
		t.Fatalf("unexpected TTS format: %q", cfg.TTS.Format)
	}
	if cfg.STT.Timeout.Std() != 30*time.Second {
		t.Fatalf("unexpected STT timeout: %s", cfg.STT.Timeout.Std())
	}
	if cfg.GPT.Timeout.Std() != 60*time.Second {
		t.Fatalf("unexpected GPT timeout: %s", cfg.GPT.Timeout.Std())
	}
	if cfg.TTS.Timeout.Std() != 30*time.Second {
		t.Fatalf("unexpected TTS timeout: %s", cfg.TTS.Timeout.Std())
	}
	if cfg.SystemPrompt == "" {
		t.Fatal("expected default system prompt")
	}
	if cfg.SessionTTL.Std() != 30*time.Minute {
		t.Fatalf("unexpected session TTL: %s", cfg.SessionTTL.Std())
	}
	if cfg.HistoryLimit <= 0 {
		t.Fatal("expected positive history limit")
	}
	if cfg.MaxAudioBytes <= 0 || cfg.MaxAnswerBytes <= 0 {
		t.Fatal("expected positive audio limits")
	}
}

func TestConfigValidate(t *testing.T) {
	valid := Config{Enabled: true, APIKey: "key", FolderID: "folder"}
	valid.ApplyDefaults()

	if err := valid.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "bad stt endpoint", mutate: func(c *Config) { c.STT.Endpoint = "ftp://example.com" }},
		{name: "empty stt language", mutate: func(c *Config) { c.STT.Language = " " }},
		{name: "zero stt timeout", mutate: func(c *Config) { c.STT.Timeout = 0 }},
		{name: "bad gpt endpoint", mutate: func(c *Config) { c.GPT.Endpoint = "ftp://example.com" }},
		{name: "empty gpt model", mutate: func(c *Config) { c.GPT.Model = " " }},
		{name: "zero gpt tokens", mutate: func(c *Config) { c.GPT.MaxTokens = 0 }},
		{name: "bad gpt temperature", mutate: func(c *Config) { c.GPT.Temperature = 1.5 }},
		{name: "zero gpt timeout", mutate: func(c *Config) { c.GPT.Timeout = 0 }},
		{name: "bad tts endpoint", mutate: func(c *Config) { c.TTS.Endpoint = "ftp://example.com" }},
		{name: "empty tts voice", mutate: func(c *Config) { c.TTS.Voice = " " }},
		{name: "empty tts language", mutate: func(c *Config) { c.TTS.Language = " " }},
		{name: "bad tts format", mutate: func(c *Config) { c.TTS.Format = "mp3" }},
		{name: "bad tts speed", mutate: func(c *Config) { c.TTS.Speed = 0.05 }},
		{name: "zero tts timeout", mutate: func(c *Config) { c.TTS.Timeout = 0 }},
		{name: "empty prompt", mutate: func(c *Config) { c.SystemPrompt = " " }},
		{name: "zero ttl", mutate: func(c *Config) { c.SessionTTL = 0 }},
		{name: "zero history", mutate: func(c *Config) { c.HistoryLimit = 0 }},
		{name: "zero max audio", mutate: func(c *Config) { c.MaxAudioBytes = 0 }},
		{name: "zero max answer", mutate: func(c *Config) { c.MaxAnswerBytes = 0 }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := valid
			tc.mutate(&cfg)

			if err := cfg.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestConfigDisabledSkipsValidation(t *testing.T) {
	cfg := Config{Enabled: false}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("disabled config should not be validated: %v", err)
	}
}

func TestCredentialsEnvOverride(t *testing.T) {
	t.Setenv("YANDEX_API_KEY", "env-key")
	t.Setenv("YANDEX_FOLDER_ID", "env-folder")

	cfg := Config{APIKey: "json-key", FolderID: "json-folder"}
	key, folder := cfg.Credentials()

	if key != "env-key" || folder != "env-folder" {
		t.Fatalf("expected env credentials, got %q/%q", key, folder)
	}
}

func TestCredentialsFromConfig(t *testing.T) {
	t.Setenv("YANDEX_API_KEY", "")
	t.Setenv("YANDEX_FOLDER_ID", "")

	cfg := Config{APIKey: "json-key", FolderID: "json-folder"}
	key, folder := cfg.Credentials()

	if key != "json-key" || folder != "json-folder" {
		t.Fatalf("expected JSON credentials, got %q/%q", key, folder)
	}
}

func TestDurationFromJSON(t *testing.T) {
	cfg := Config{STT: STTConfig{Timeout: settings.Duration(5 * time.Second)}}
	cfg.ApplyDefaults()

	if cfg.STT.Timeout.Std() != 5*time.Second {
		t.Fatalf("unexpected timeout: %s", cfg.STT.Timeout.Std())
	}
}
