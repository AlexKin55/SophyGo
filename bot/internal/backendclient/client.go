package backendclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultLocationPath      = "/api/v1/guide/location"
	defaultAISessionPath     = "/api/v1/ai/session"
	defaultAISessionStopPath = "/api/v1/ai/session/stop"
	defaultAIVoicePath       = "/api/v1/ai/voice"
	defaultTimeout           = 10 * time.Second
	defaultAITimeout         = 120 * time.Second
	defaultMaxResponseBytes  = 1 << 20
	defaultAIMaxResponse     = 32 << 20
	defaultMaxErrorBodyBytes = 4 << 10
)

// Options configures the backend HTTP client.
type Options struct {
	LocationPath       string
	AISessionPath      string
	AISessionStopPath  string
	AIVoicePath        string
	Timeout            time.Duration
	AITimeout          time.Duration
	Retries            int
	RetryBackoff       time.Duration
	MaxResponseBytes   int64
	AIMaxResponseBytes int64
	MaxErrorBodyBytes  int64
}

type LocationRequest struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type LocationResponse struct {
	Message string `json:"message"`
}

type aiSessionRequest struct {
	UserID int64 `json:"user_id"`
}

type aiVoiceRequest struct {
	UserID int64  `json:"user_id"`
	Audio  string `json:"audio"`
	Format string `json:"format"`
}

type aiVoiceResponse struct {
	Transcript string `json:"transcript"`
	Text       string `json:"text"`
	Audio      string `json:"audio"`
	Format     string `json:"format"`
}

type Client struct {
	baseURL    string
	options    Options
	httpClient *http.Client
}

// New creates a backend client. Zero or negative option values are replaced
// with safe defaults so that the client always has sane limits.
func New(baseURL string, options Options) *Client {
	if options.LocationPath == "" {
		options.LocationPath = defaultLocationPath
	}
	if options.AISessionPath == "" {
		options.AISessionPath = defaultAISessionPath
	}
	if options.AISessionStopPath == "" {
		options.AISessionStopPath = defaultAISessionStopPath
	}
	if options.AIVoicePath == "" {
		options.AIVoicePath = defaultAIVoicePath
	}
	if options.Timeout <= 0 {
		options.Timeout = defaultTimeout
	}
	if options.AITimeout <= 0 {
		options.AITimeout = defaultAITimeout
	}
	if options.Retries < 0 {
		options.Retries = 0
	}
	if options.RetryBackoff < 0 {
		options.RetryBackoff = 0
	}
	if options.MaxResponseBytes <= 0 {
		options.MaxResponseBytes = defaultMaxResponseBytes
	}
	if options.AIMaxResponseBytes <= 0 {
		options.AIMaxResponseBytes = defaultAIMaxResponse
	}
	if options.MaxErrorBodyBytes <= 0 {
		options.MaxErrorBodyBytes = defaultMaxErrorBodyBytes
	}

	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		options: options,
		// Per-request contexts define the timeouts: location requests and AI
		// requests need different limits.
		httpClient: &http.Client{},
	}
}

// SendLocation posts coordinates to the backend and returns the guide message.
// Network errors, 5xx and 429 responses are retried according to the configured
// retry policy; other errors are returned immediately.
func (c *Client) SendLocation(lat, lon float64) (string, error) {
	reqBody, err := json.Marshal(LocationRequest{
		Latitude:  lat,
		Longitude: lon,
	})
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	var lastErr error

	for attempt := 0; attempt <= c.options.Retries; attempt++ {
		if attempt > 0 && c.options.RetryBackoff > 0 {
			time.Sleep(c.options.RetryBackoff)
		}

		message, retryable, err := c.sendLocationOnce(reqBody)
		if err == nil {
			return message, nil
		}

		lastErr = err
		if !retryable {
			return "", err
		}
	}

	return "", fmt.Errorf("backend request failed after %d attempt(s): %w", c.options.Retries+1, lastErr)
}

func (c *Client) sendLocationOnce(reqBody []byte) (string, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.options.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+c.options.LocationPath, bytes.NewReader(reqBody))
	if err != nil {
		return "", false, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", true, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, c.options.MaxErrorBodyBytes))
		err := fmt.Errorf("backend returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		retryable := resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests

		return "", retryable, err
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, c.options.MaxResponseBytes))
	if err != nil {
		return "", false, fmt.Errorf("read response: %w", err)
	}

	var locationResp LocationResponse
	if err := json.Unmarshal(body, &locationResp); err != nil {
		return "", false, fmt.Errorf("unmarshal response: %w", err)
	}

	if locationResp.Message == "" {
		return "", false, errors.New("backend returned empty message")
	}

	return locationResp.Message, false, nil
}

// StartAISession opens a Yandex Realtime session for the user.
func (c *Client) StartAISession(userID int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.options.AITimeout)
	defer cancel()

	return c.postJSON(ctx, c.options.AISessionPath, aiSessionRequest{UserID: userID}, nil)
}

// StopAISession closes the user's Yandex Realtime session.
func (c *Client) StopAISession(userID int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.options.AITimeout)
	defer cancel()

	return c.postJSON(ctx, c.options.AISessionStopPath, aiSessionRequest{UserID: userID}, nil)
}

// AskVoice sends an OGG/Opus voice message and returns the synthesized OGG
// answer plus the answer text and the recognized question.
func (c *Client) AskVoice(userID int64, audio []byte) ([]byte, string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.options.AITimeout)
	defer cancel()

	var resp aiVoiceResponse

	err := c.postJSON(ctx, c.options.AIVoicePath, aiVoiceRequest{
		UserID: userID,
		Audio:  base64.StdEncoding.EncodeToString(audio),
		Format: "ogg",
	}, &resp)
	if err != nil {
		return nil, "", "", err
	}

	out, err := base64.StdEncoding.DecodeString(resp.Audio)
	if err != nil {
		return nil, "", "", fmt.Errorf("decode answer audio: %w", err)
	}

	return out, resp.Text, resp.Transcript, nil
}

func (c *Client) postJSON(ctx context.Context, path string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, c.options.MaxErrorBodyBytes))
		return fmt.Errorf("backend returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if out == nil {
		return nil
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, c.options.AIMaxResponseBytes))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("unmarshal response: %w", err)
	}

	return nil
}
