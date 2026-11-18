// Package ai integrates the backend with Yandex AI Studio.
//
// Voice flow: SpeechKit STT accepts a Telegram voice message (OGG/Opus),
// YandexGPT generates a text answer and SpeechKit TTS returns OGG/Opus. No
// audio transcoding is required.
package ai

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"travelbuddy/internal/settings"
)

const (
	DefaultSTTEndpoint  = "https://stt.api.cloud.yandex.net/speech/v1/stt:recognize"
	DefaultGPTEndpoint  = "https://llm.api.cloud.yandex.net/foundationModels/v1/completion"
	DefaultTTSEndpoint  = "https://tts.api.cloud.yandex.net/speech/v1/tts:synthesize"
	DefaultGPTModel     = "yandexgpt/latest"
	DefaultTTSFormat    = "oggopus"
	DefaultVoice        = "alena"
	DefaultSystemPrompt = "Ты SophyGo — весёлый, добрый и немного озорной гид по интересным местам. Тебя придумала и развивает София. Отвечай голосом живо и кратко, без markdown, списков и эмодзи. Рассказывай интересные факты, но не выдумывай: если не уверен, честно скажи. Легенды и неподтверждённые версии обозначай отдельно и не выдавай за историю. Не угадывай, где находится пользователь, если тебе этого не сообщили. Не изображай известных персонажей и реальных людей. Тон — уважительный и безопасный для детей и взрослых."
)

// STTConfig configures SpeechKit synchronous recognition (OGG/Opus input).
type STTConfig struct {
	Endpoint string            `json:"endpoint"`
	Language string            `json:"language"`
	Topic    string            `json:"topic"`
	Timeout  settings.Duration `json:"timeout"`
}

// GPTConfig configures YandexGPT text completion.
type GPTConfig struct {
	Endpoint    string            `json:"endpoint"`
	Model       string            `json:"model"`
	Temperature float64           `json:"temperature"`
	MaxTokens   int               `json:"max_tokens"`
	Timeout     settings.Duration `json:"timeout"`
}

// TTSConfig configures SpeechKit synthesis (OGG/Opus output).
type TTSConfig struct {
	Endpoint string            `json:"endpoint"`
	Voice    string            `json:"voice"`
	Language string            `json:"language"`
	Format   string            `json:"format"`
	Speed    float64           `json:"speed"`
	Timeout  settings.Duration `json:"timeout"`
}

// Config is the "ai" section of backend/settings/backend.json.
type Config struct {
	Enabled  bool   `json:"enabled"`
	APIKey   string `json:"api_key"`
	FolderID string `json:"folder_id"`

	STT STTConfig `json:"stt"`
	GPT GPTConfig `json:"gpt"`
	TTS TTSConfig `json:"tts"`

	SystemPrompt   string            `json:"system_prompt"`
	SessionTTL     settings.Duration `json:"session_ttl"`
	HistoryLimit   int               `json:"history_limit"`
	MaxAudioBytes  int64             `json:"max_audio_bytes"`
	MaxAnswerBytes int64             `json:"max_answer_bytes"`
}

// ApplyDefaults fills empty optional fields with safe defaults.
func (c *Config) ApplyDefaults() {
	if c.STT.Endpoint == "" {
		c.STT.Endpoint = DefaultSTTEndpoint
	}
	if c.STT.Language == "" {
		c.STT.Language = "ru-RU"
	}
	if c.STT.Topic == "" {
		c.STT.Topic = "general"
	}
	if c.STT.Timeout.Std() <= 0 {
		c.STT.Timeout = settings.Duration(30 * time.Second)
	}

	if c.GPT.Endpoint == "" {
		c.GPT.Endpoint = DefaultGPTEndpoint
	}
	if c.GPT.Model == "" {
		c.GPT.Model = DefaultGPTModel
	}
	if c.GPT.MaxTokens <= 0 {
		c.GPT.MaxTokens = 1000
	}
	if c.GPT.Timeout.Std() <= 0 {
		c.GPT.Timeout = settings.Duration(60 * time.Second)
	}

	if c.TTS.Endpoint == "" {
		c.TTS.Endpoint = DefaultTTSEndpoint
	}
	if c.TTS.Voice == "" {
		c.TTS.Voice = DefaultVoice
	}
	if c.TTS.Language == "" {
		c.TTS.Language = "ru-RU"
	}
	if c.TTS.Format == "" {
		c.TTS.Format = DefaultTTSFormat
	}
	if c.TTS.Speed <= 0 {
		c.TTS.Speed = 1.0
	}
	if c.TTS.Timeout.Std() <= 0 {
		c.TTS.Timeout = settings.Duration(30 * time.Second)
	}

	if strings.TrimSpace(c.SystemPrompt) == "" {
		c.SystemPrompt = DefaultSystemPrompt
	}
	if c.SessionTTL.Std() <= 0 {
		c.SessionTTL = settings.Duration(30 * time.Minute)
	}
	if c.HistoryLimit <= 0 {
		c.HistoryLimit = 20
	}
	if c.MaxAudioBytes <= 0 {
		// SpeechKit synchronous recognition accepts up to 1 MB.
		c.MaxAudioBytes = 1 << 20
	}
	if c.MaxAnswerBytes <= 0 {
		c.MaxAnswerBytes = 10 << 20
	}
}

// Validate checks the AI settings. Missing credentials are allowed: the
// service stays disabled and its endpoints return 503 until they are set.
func (c *Config) Validate() error {
	if !c.Enabled {
		return nil
	}

	if !isHTTPURL(c.STT.Endpoint) {
		return errors.New("ai.stt.endpoint must start with http:// or https://")
	}
	if strings.TrimSpace(c.STT.Language) == "" {
		return errors.New("ai.stt.language is required")
	}
	if c.STT.Timeout.Std() <= 0 {
		return errors.New("ai.stt.timeout must be positive")
	}

	if !isHTTPURL(c.GPT.Endpoint) {
		return errors.New("ai.gpt.endpoint must start with http:// or https://")
	}
	if strings.TrimSpace(c.GPT.Model) == "" {
		return errors.New("ai.gpt.model is required")
	}
	if c.GPT.MaxTokens <= 0 {
		return errors.New("ai.gpt.max_tokens must be positive")
	}
	if c.GPT.Temperature < 0 || c.GPT.Temperature > 1 {
		return errors.New("ai.gpt.temperature must be between 0 and 1")
	}
	if c.GPT.Timeout.Std() <= 0 {
		return errors.New("ai.gpt.timeout must be positive")
	}

	if !isHTTPURL(c.TTS.Endpoint) {
		return errors.New("ai.tts.endpoint must start with http:// or https://")
	}
	if strings.TrimSpace(c.TTS.Voice) == "" {
		return errors.New("ai.tts.voice is required")
	}
	if strings.TrimSpace(c.TTS.Language) == "" {
		return errors.New("ai.tts.language is required")
	}
	if c.TTS.Format != DefaultTTSFormat {
		return fmt.Errorf("ai.tts.format must be %q for Telegram voice messages", DefaultTTSFormat)
	}
	if c.TTS.Speed < 0.1 || c.TTS.Speed > 3.0 {
		return errors.New("ai.tts.speed must be between 0.1 and 3.0")
	}
	if c.TTS.Timeout.Std() <= 0 {
		return errors.New("ai.tts.timeout must be positive")
	}

	if strings.TrimSpace(c.SystemPrompt) == "" {
		return errors.New("ai.system_prompt is required")
	}
	if c.SessionTTL.Std() <= 0 {
		return errors.New("ai.session_ttl must be positive")
	}
	if c.HistoryLimit <= 0 {
		return errors.New("ai.history_limit must be positive")
	}
	if c.MaxAudioBytes <= 0 {
		return errors.New("ai.max_audio_bytes must be positive")
	}
	if c.MaxAnswerBytes <= 0 {
		return errors.New("ai.max_answer_bytes must be positive")
	}

	return nil
}

func isHTTPURL(value string) bool {
	return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")
}

// Credentials returns the Yandex API key and folder id. Environment variables
// take precedence over the JSON settings.
func (c Config) Credentials() (string, string) {
	key := strings.TrimSpace(os.Getenv("YANDEX_API_KEY"))
	folder := strings.TrimSpace(os.Getenv("YANDEX_FOLDER_ID"))

	if key == "" {
		key = strings.TrimSpace(c.APIKey)
	}
	if folder == "" {
		folder = strings.TrimSpace(c.FolderID)
	}

	return key, folder
}
