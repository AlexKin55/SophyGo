package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Message is one YandexGPT dialogue message.
type Message struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

type speechProvider interface {
	Recognize(ctx context.Context, ogg []byte) (string, error)
	Synthesize(ctx context.Context, text string) ([]byte, error)
}

type chatProvider interface {
	Complete(ctx context.Context, messages []Message) (string, error)
}

type yandexProvider struct {
	cfg    Config
	client *http.Client
}

func newYandexProvider(cfg Config) *yandexProvider {
	return &yandexProvider{
		cfg:    cfg,
		client: &http.Client{},
	}
}

// Recognize sends OGG/Opus audio to SpeechKit STT and returns the transcript.
func (p *yandexProvider) Recognize(ctx context.Context, ogg []byte) (string, error) {
	key, folder := p.cfg.Credentials()
	if key == "" || folder == "" {
		return "", errors.New("Yandex credentials are not set")
	}

	ctx, cancel := context.WithTimeout(ctx, p.cfg.STT.Timeout.Std())
	defer cancel()

	endpoint, err := url.Parse(p.cfg.STT.Endpoint)
	if err != nil {
		return "", fmt.Errorf("parse stt endpoint: %w", err)
	}

	query := endpoint.Query()
	query.Set("folderId", folder)
	query.Set("lang", p.cfg.STT.Language)
	query.Set("format", "oggopus")
	if p.cfg.STT.Topic != "" {
		query.Set("topic", p.cfg.STT.Topic)
	}
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(ogg))
	if err != nil {
		return "", fmt.Errorf("create stt request: %w", err)
	}
	req.Header.Set("Authorization", "Api-Key "+key)
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("send stt request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read stt response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("speechkit stt: status %d: %s", resp.StatusCode, truncateBody(body))
	}

	var result struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("unmarshal stt response: %w", err)
	}

	transcript := strings.TrimSpace(result.Result)
	if transcript == "" {
		return "", errors.New("speechkit stt returned an empty transcript")
	}

	return transcript, nil
}

// Synthesize sends text to SpeechKit TTS and returns OGG/Opus audio.
func (p *yandexProvider) Synthesize(ctx context.Context, text string) ([]byte, error) {
	key, folder := p.cfg.Credentials()
	if key == "" || folder == "" {
		return nil, errors.New("Yandex credentials are not set")
	}

	ctx, cancel := context.WithTimeout(ctx, p.cfg.TTS.Timeout.Std())
	defer cancel()

	form := url.Values{}
	form.Set("text", text)
	form.Set("lang", p.cfg.TTS.Language)
	form.Set("voice", p.cfg.TTS.Voice)
	form.Set("format", p.cfg.TTS.Format)
	form.Set("folderId", folder)
	form.Set("speed", strconv.FormatFloat(p.cfg.TTS.Speed, 'f', 2, 64))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.TTS.Endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create tts request: %w", err)
	}
	req.Header.Set("Authorization", "Api-Key "+key)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send tts request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("speechkit tts: status %d: %s", resp.StatusCode, truncateBody(body))
	}

	audio, err := io.ReadAll(io.LimitReader(resp.Body, p.cfg.MaxAnswerBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read tts response: %w", err)
	}

	if int64(len(audio)) > p.cfg.MaxAnswerBytes {
		return nil, ErrTooLarge
	}
	if len(audio) == 0 {
		return nil, errors.New("speechkit tts returned empty audio")
	}

	return audio, nil
}

type gptCompletionOptions struct {
	Stream      bool    `json:"stream"`
	Temperature float64 `json:"temperature"`
	MaxTokens   string  `json:"maxTokens"`
}

type gptRequest struct {
	ModelURI          string               `json:"modelUri"`
	CompletionOptions gptCompletionOptions `json:"completionOptions"`
	Messages          []Message            `json:"messages"`
}

type gptResponse struct {
	Result struct {
		Alternatives []struct {
			Message struct {
				Text string `json:"text"`
			} `json:"message"`
		} `json:"alternatives"`
	} `json:"result"`
}

// Complete sends the dialogue to YandexGPT and returns the answer text.
func (p *yandexProvider) Complete(ctx context.Context, messages []Message) (string, error) {
	key, folder := p.cfg.Credentials()
	if key == "" || folder == "" {
		return "", errors.New("Yandex credentials are not set")
	}

	ctx, cancel := context.WithTimeout(ctx, p.cfg.GPT.Timeout.Std())
	defer cancel()

	payload := gptRequest{
		ModelURI: "gpt://" + folder + "/" + p.cfg.GPT.Model,
		CompletionOptions: gptCompletionOptions{
			Stream:      false,
			Temperature: p.cfg.GPT.Temperature,
			MaxTokens:   strconv.Itoa(p.cfg.GPT.MaxTokens),
		},
		Messages: messages,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal gpt request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.GPT.Endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create gpt request: %w", err)
	}
	req.Header.Set("Authorization", "Api-Key "+key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("send gpt request: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read gpt response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("yandexgpt: status %d: %s", resp.StatusCode, truncateBody(responseBody))
	}

	var result gptResponse
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return "", fmt.Errorf("unmarshal gpt response: %w", err)
	}

	if len(result.Result.Alternatives) == 0 {
		return "", errors.New("yandexgpt returned no alternatives")
	}

	answer := strings.TrimSpace(result.Result.Alternatives[0].Message.Text)
	if answer == "" {
		return "", errors.New("yandexgpt returned an empty answer")
	}

	return answer, nil
}

func truncateBody(body []byte) string {
	const limit = 512

	text := strings.TrimSpace(string(body))
	if len(text) <= limit {
		return text
	}

	return text[:limit] + "..."
}
