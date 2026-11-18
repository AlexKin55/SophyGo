package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"travelbuddy/backend/internal/ai"
)

type fakeAIService struct {
	enabled   bool
	maxAudio  int64
	startErr  error
	stopErr   error
	askErr    error
	startedID int64
	stoppedID int64
	askedID   int64
	asked     []byte
	answer    ai.VoiceAnswer
}

func (f *fakeAIService) Enabled() bool { return f.enabled }

func (f *fakeAIService) MaxAudioBytes() int64 {
	if f.maxAudio > 0 {
		return f.maxAudio
	}

	return 1 << 20
}

func (f *fakeAIService) Start(_ context.Context, userID int64) error {
	f.startedID = userID
	return f.startErr
}

func (f *fakeAIService) Stop(userID int64) error {
	f.stoppedID = userID
	return f.stopErr
}

func (f *fakeAIService) Ask(_ context.Context, userID int64, ogg []byte) (ai.VoiceAnswer, error) {
	f.askedID = userID
	f.asked = ogg

	return f.answer, f.askErr
}

func TestAISessionStartHandler(t *testing.T) {
	svc := &fakeAIService{enabled: true}
	handler := AISessionStartHandler(svc, discardLogger())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/session", strings.NewReader(`{"user_id":42}`))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if svc.startedID != 42 {
		t.Fatalf("expected user 42, got %d", svc.startedID)
	}
}

func TestAISessionStartHandlerRejectsInvalidRequest(t *testing.T) {
	svc := &fakeAIService{enabled: true}
	handler := AISessionStartHandler(svc, discardLogger())

	tests := []string{
		`{}`,
		`{"user_id":0}`,
		`{"user_id":1,"extra":true}`,
		`{`,
	}

	for _, body := range tests {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/session", strings.NewReader(body))
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("body %q: expected 400, got %d", body, w.Code)
		}
	}
}

func TestAISessionStartHandlerDisabled(t *testing.T) {
	svc := &fakeAIService{enabled: false, startErr: ai.ErrDisabled}
	handler := AISessionStartHandler(svc, discardLogger())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/session", strings.NewReader(`{"user_id":1}`))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestAISessionStopHandler(t *testing.T) {
	svc := &fakeAIService{enabled: true}
	handler := AISessionStopHandler(svc, discardLogger())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/session/stop", strings.NewReader(`{"user_id":7}`))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if svc.stoppedID != 7 {
		t.Fatalf("expected user 7, got %d", svc.stoppedID)
	}
}

func TestAIVoiceHandler(t *testing.T) {
	svc := &fakeAIService{
		enabled: true,
		answer: ai.VoiceAnswer{
			Audio:      []byte("ogg-answer"),
			Text:       "hello",
			Transcript: "question",
		},
	}
	handler := AIVoiceHandler(svc, discardLogger())

	audio := base64.StdEncoding.EncodeToString([]byte("ogg-question"))
	body := `{"user_id":7,"audio":"` + audio + `","format":"ogg"}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/voice", strings.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if svc.askedID != 7 || string(svc.asked) != "ogg-question" {
		t.Fatalf("unexpected ask call: %d, %q", svc.askedID, svc.asked)
	}

	var resp aiVoiceResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	out, err := base64.StdEncoding.DecodeString(resp.Audio)
	if err != nil || string(out) != "ogg-answer" {
		t.Fatalf("unexpected answer audio: %q, %v", out, err)
	}
	if resp.Text != "hello" || resp.Transcript != "question" || resp.Format != "ogg" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestAIVoiceHandlerRejectsInvalidInput(t *testing.T) {
	svc := &fakeAIService{enabled: true}
	handler := AIVoiceHandler(svc, discardLogger())

	tests := []struct {
		name string
		body string
	}{
		{name: "missing user", body: `{"audio":"b2dn"}`},
		{name: "missing audio", body: `{"user_id":1}`},
		{name: "bad base64", body: `{"user_id":1,"audio":"!!!"}`},
		{name: "bad format", body: `{"user_id":1,"audio":"b2dn","format":"mp3"}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/voice", strings.NewReader(tc.body))
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", w.Code)
			}
		})
	}
}

func TestAIVoiceHandlerTooLarge(t *testing.T) {
	svc := &fakeAIService{enabled: true, maxAudio: 4}
	handler := AIVoiceHandler(svc, discardLogger())

	audio := base64.StdEncoding.EncodeToString([]byte("12345"))
	body := `{"user_id":1,"audio":"` + audio + `"}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/voice", strings.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", w.Code)
	}
}

func TestAIVoiceHandlerDisabled(t *testing.T) {
	svc := &fakeAIService{enabled: false, askErr: ai.ErrDisabled}
	handler := AIVoiceHandler(svc, discardLogger())

	audio := base64.StdEncoding.EncodeToString([]byte("ogg"))
	body := `{"user_id":1,"audio":"` + audio + `"}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/voice", strings.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestAIVoiceHandlerTimeout(t *testing.T) {
	svc := &fakeAIService{enabled: true, askErr: context.DeadlineExceeded}
	handler := AIVoiceHandler(svc, discardLogger())

	audio := base64.StdEncoding.EncodeToString([]byte("ogg"))
	body := `{"user_id":1,"audio":"` + audio + `"}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/voice", strings.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected 504, got %d", w.Code)
	}
}

func TestAIHandlersRejectOtherMethods(t *testing.T) {
	svc := &fakeAIService{enabled: true}

	for _, handler := range []http.HandlerFunc{
		AISessionStartHandler(svc, discardLogger()),
		AISessionStopHandler(svc, discardLogger()),
		AIVoiceHandler(svc, discardLogger()),
	} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", w.Code)
		}
	}
}

var _ = io.Discard
var _ = errors.New
