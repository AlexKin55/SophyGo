package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"travelbuddy/backend/internal/ai"
)

// AIService is the subset of ai.Service used by HTTP handlers.
type AIService interface {
	Enabled() bool
	MaxAudioBytes() int64
	Start(ctx context.Context, userID int64) error
	Stop(userID int64) error
	Ask(ctx context.Context, userID int64, ogg []byte) (ai.VoiceAnswer, error)
}

const (
	maxAISessionBodyBytes = 16 << 10
	aiSessionStartTimeout = 30 * time.Second
)

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

type aiStatusResponse struct {
	Status string `json:"status"`
}

// AISessionStartHandler opens a Yandex Realtime session for the user.
func AISessionStartHandler(svc AIService, log *slog.Logger) http.HandlerFunc {
	return aiSessionHandler(svc, log, true)
}

// AISessionStopHandler closes the user's Realtime session.
func AISessionStopHandler(svc AIService, log *slog.Logger) http.HandlerFunc {
	return aiSessionHandler(svc, log, false)
}

func aiSessionHandler(svc AIService, log *slog.Logger, start bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"}, log)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxAISessionBodyBytes)
		defer r.Body.Close()

		var req aiSessionRequest
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()

		if err := decoder.Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"}, log)
			return
		}

		if req.UserID <= 0 {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "user_id is required"}, log)
			return
		}

		var err error
		if start {
			ctx, cancel := context.WithTimeout(r.Context(), aiSessionStartTimeout)
			defer cancel()

			err = svc.Start(ctx, req.UserID)
		} else {
			err = svc.Stop(req.UserID)
		}

		if err != nil {
			writeAIError(w, log, err)
			return
		}

		writeJSON(w, http.StatusOK, aiStatusResponse{Status: "ok"}, log)
	}
}

// AIVoiceHandler accepts a Telegram voice message (base64 OGG/Opus), sends it
// through the user's Realtime session and returns the synthesized OGG answer.
func AIVoiceHandler(svc AIService, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"}, log)
			return
		}

		maxAudioBytes := svc.MaxAudioBytes()
		maxBodyBytes := maxAudioBytes*4/3 + 64<<10

		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		defer r.Body.Close()

		var req aiVoiceRequest
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()

		if err := decoder.Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"}, log)
			return
		}

		if req.UserID <= 0 {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "user_id is required"}, log)
			return
		}

		switch req.Format {
		case "", "ogg", "opus":
		default:
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "unsupported audio format"}, log)
			return
		}

		audio, err := base64.StdEncoding.DecodeString(req.Audio)
		if err != nil || len(audio) == 0 {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "audio is required"}, log)
			return
		}

		if int64(len(audio)) > maxAudioBytes {
			writeAIError(w, log, ai.ErrTooLarge)
			return
		}

		answer, err := svc.Ask(r.Context(), req.UserID, audio)
		if err != nil {
			writeAIError(w, log, err)
			return
		}

		writeJSON(w, http.StatusOK, aiVoiceResponse{
			Transcript: answer.Transcript,
			Text:       answer.Text,
			Audio:      base64.StdEncoding.EncodeToString(answer.Audio),
			Format:     "ogg",
		}, log)
	}
}

func writeAIError(w http.ResponseWriter, log *slog.Logger, err error) {
	status := http.StatusBadGateway
	message := "AI service error"

	switch {
	case errors.Is(err, ai.ErrDisabled):
		status = http.StatusServiceUnavailable
		message = "AI service is disabled"
	case errors.Is(err, ai.ErrTooLarge):
		status = http.StatusRequestEntityTooLarge
		message = "audio is too large"
	case errors.Is(err, context.DeadlineExceeded):
		status = http.StatusGatewayTimeout
		message = "AI service timeout"
	}

	log.Error("AI request failed", "error", err)
	writeJSON(w, status, ErrorResponse{Error: message}, log)
}
