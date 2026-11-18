package handlers

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type sentMessage struct {
	chatID int64
	text   string
	markup interface{}
}

type sentVoice struct {
	chatID  int64
	caption string
	size    int
}

type fakeSender struct {
	sent    []sentMessage
	voices  []sentVoice
	err     error
	fileURL string
	fileErr error
}

func (f *fakeSender) Send(c tgbotapi.Chattable) (tgbotapi.Message, error) {
	switch msg := c.(type) {
	case tgbotapi.MessageConfig:
		f.sent = append(f.sent, sentMessage{
			chatID: msg.ChatID,
			text:   msg.Text,
			markup: msg.ReplyMarkup,
		})
	case tgbotapi.VoiceConfig:
		size := 0
		if file, ok := msg.File.(tgbotapi.FileBytes); ok {
			size = len(file.Bytes)
		}
		f.voices = append(f.voices, sentVoice{
			chatID:  msg.ChatID,
			caption: msg.Caption,
			size:    size,
		})
	default:
		return tgbotapi.Message{}, errors.New("unexpected chattable type")
	}

	return tgbotapi.Message{}, f.err
}

func (f *fakeSender) GetFileDirectURL(string) (string, error) {
	return f.fileURL, f.fileErr
}

type fakeBackend struct {
	message string
	err     error
	calls   int
	lat     float64
	lon     float64

	started    chan int64
	startCalls int
	stopCalls  int
	aiErr      error

	voiceCalls      int
	voiceUserID     int64
	voiceAudio      []byte
	voiceAnswer     []byte
	voiceText       string
	voiceTranscript string
	voiceErr        error
}

func (f *fakeBackend) SendLocation(lat, lon float64) (string, error) {
	f.calls++
	f.lat = lat
	f.lon = lon

	return f.message, f.err
}

func (f *fakeBackend) StartAISession(userID int64) error {
	f.startCalls++
	if f.started != nil {
		select {
		case f.started <- userID:
		default:
		}
	}

	return f.aiErr
}

func (f *fakeBackend) StopAISession(int64) error {
	f.stopCalls++
	return f.aiErr
}

func (f *fakeBackend) AskVoice(userID int64, audio []byte) ([]byte, string, string, error) {
	f.voiceCalls++
	f.voiceUserID = userID
	f.voiceAudio = audio

	return f.voiceAnswer, f.voiceText, f.voiceTranscript, f.voiceErr
}

func newTestHandler(sender *fakeSender, backend *fakeBackend) *Handler {
	return New(sender, backend, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func startCommandUpdate(chatID int64) tgbotapi.Update {
	return tgbotapi.Update{
		Message: &tgbotapi.Message{
			From: &tgbotapi.User{ID: chatID},
			Chat: &tgbotapi.Chat{ID: chatID},
			Text: "/start",
			Entities: []tgbotapi.MessageEntity{
				{Type: "bot_command", Offset: 0, Length: len("/start")},
			},
		},
	}
}

func waitForStart(t *testing.T, backend *fakeBackend) int64 {
	t.Helper()

	select {
	case id := <-backend.started:
		return id
	case <-time.After(time.Second):
		t.Fatal("AI session was not started")
		return 0
	}
}

func TestHandleUpdateStartSendsWelcome(t *testing.T) {
	sender := &fakeSender{}
	backend := &fakeBackend{started: make(chan int64, 1)}
	handler := newTestHandler(sender, backend)

	handler.HandleUpdate(startCommandUpdate(42))

	if len(sender.sent) != 1 {
		t.Fatalf("expected 1 message, got %d", len(sender.sent))
	}

	if sender.sent[0].chatID != 42 {
		t.Fatalf("expected chat id 42, got %d", sender.sent[0].chatID)
	}

	if !strings.Contains(sender.sent[0].text, "SophyGo") {
		t.Fatalf("welcome message should mention SophyGo, got %q", sender.sent[0].text)
	}

	markup, ok := sender.sent[0].markup.(tgbotapi.ReplyKeyboardMarkup)
	if !ok {
		t.Fatalf("expected reply keyboard, got %T", sender.sent[0].markup)
	}

	if len(markup.Keyboard) != 1 || len(markup.Keyboard[0]) != 1 {
		t.Fatalf("expected a single location button, got %+v", markup.Keyboard)
	}

	if !markup.Keyboard[0][0].RequestLocation {
		t.Fatal("expected location request button")
	}

	if backend.calls != 0 {
		t.Fatalf("location backend should not be called, got %d calls", backend.calls)
	}

	if id := waitForStart(t, backend); id != 42 {
		t.Fatalf("expected AI session for user 42, got %d", id)
	}
}

func TestHandleUpdateUnknownCommand(t *testing.T) {
	sender := &fakeSender{}
	handler := newTestHandler(sender, &fakeBackend{})

	update := tgbotapi.Update{
		Message: &tgbotapi.Message{
			Chat: &tgbotapi.Chat{ID: 1},
			Text: "/unknown",
			Entities: []tgbotapi.MessageEntity{
				{Type: "bot_command", Offset: 0, Length: len("/unknown")},
			},
		},
	}

	handler.HandleUpdate(update)

	if len(sender.sent) != 1 {
		t.Fatalf("expected 1 message, got %d", len(sender.sent))
	}

	if !strings.Contains(sender.sent[0].text, "/start") {
		t.Fatalf("expected hint about /start, got %q", sender.sent[0].text)
	}
}

func TestHandleUpdateLocationCallsBackend(t *testing.T) {
	sender := &fakeSender{}
	backend := &fakeBackend{message: "Рассказ о месте"}
	handler := newTestHandler(sender, backend)

	update := tgbotapi.Update{
		Message: &tgbotapi.Message{
			Chat:     &tgbotapi.Chat{ID: 7},
			Location: &tgbotapi.Location{Latitude: 52.5163, Longitude: 13.3777},
		},
	}

	handler.HandleUpdate(update)

	if backend.calls != 1 {
		t.Fatalf("expected 1 backend call, got %d", backend.calls)
	}

	if backend.lat != 52.5163 || backend.lon != 13.3777 {
		t.Fatalf("unexpected coordinates: %v, %v", backend.lat, backend.lon)
	}

	if len(sender.sent) != 1 {
		t.Fatalf("expected 1 message, got %d", len(sender.sent))
	}

	if sender.sent[0].text != "Рассказ о месте" {
		t.Fatalf("expected backend message, got %q", sender.sent[0].text)
	}
}

func TestHandleUpdateLocationRejectsInvalidCoordinates(t *testing.T) {
	sender := &fakeSender{}
	backend := &fakeBackend{}
	handler := newTestHandler(sender, backend)

	update := tgbotapi.Update{
		Message: &tgbotapi.Message{
			Chat:     &tgbotapi.Chat{ID: 7},
			Location: &tgbotapi.Location{Latitude: 91, Longitude: 13.3777},
		},
	}

	handler.HandleUpdate(update)

	if backend.calls != 0 {
		t.Fatalf("backend should not be called, got %d calls", backend.calls)
	}

	if len(sender.sent) != 1 || sender.sent[0].text != invalidCoords {
		t.Fatalf("expected invalid coordinates message, got %+v", sender.sent)
	}
}

func TestHandleUpdateLocationBackendError(t *testing.T) {
	sender := &fakeSender{}
	backend := &fakeBackend{err: errors.New("backend is down")}
	handler := newTestHandler(sender, backend)

	update := tgbotapi.Update{
		Message: &tgbotapi.Message{
			Chat:     &tgbotapi.Chat{ID: 7},
			Location: &tgbotapi.Location{Latitude: 52.5163, Longitude: 13.3777},
		},
	}

	handler.HandleUpdate(update)

	if len(sender.sent) != 1 || sender.sent[0].text != backendUnavailable {
		t.Fatalf("expected backend unavailable message, got %+v", sender.sent)
	}
}

func TestHandleUpdateIgnoresNilMessage(t *testing.T) {
	sender := &fakeSender{}
	handler := newTestHandler(sender, &fakeBackend{})

	handler.HandleUpdate(tgbotapi.Update{})

	if len(sender.sent) != 0 {
		t.Fatalf("expected no messages, got %d", len(sender.sent))
	}
}

func TestHandleUpdatePlainText(t *testing.T) {
	sender := &fakeSender{}
	handler := newTestHandler(sender, &fakeBackend{})

	handler.HandleUpdate(tgbotapi.Update{
		Message: &tgbotapi.Message{
			Chat: &tgbotapi.Chat{ID: 5},
			Text: "привет",
		},
	})

	if len(sender.sent) != 1 {
		t.Fatalf("expected 1 message, got %d", len(sender.sent))
	}

	if !strings.Contains(sender.sent[0].text, "/start") {
		t.Fatalf("expected hint about /start, got %q", sender.sent[0].text)
	}
}

func TestHandleVoiceSendsVoiceAnswer(t *testing.T) {
	fileServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ogg-question"))
	}))
	defer fileServer.Close()

	sender := &fakeSender{fileURL: fileServer.URL}
	backend := &fakeBackend{
		voiceAnswer: []byte("ogg-answer"),
		voiceText:   "Привет! Я SophyGo.",
	}
	handler := newTestHandler(sender, backend)

	handler.handleVoice(&tgbotapi.Message{
		From:  &tgbotapi.User{ID: 99},
		Chat:  &tgbotapi.Chat{ID: 99},
		Voice: &tgbotapi.Voice{FileID: "file-id"},
	})

	if backend.voiceCalls != 1 {
		t.Fatalf("expected 1 voice call, got %d", backend.voiceCalls)
	}

	if backend.voiceUserID != 99 {
		t.Fatalf("expected user id 99, got %d", backend.voiceUserID)
	}

	if string(backend.voiceAudio) != "ogg-question" {
		t.Fatalf("unexpected downloaded audio: %q", backend.voiceAudio)
	}

	if len(sender.voices) != 1 {
		t.Fatalf("expected 1 voice answer, got %d", len(sender.voices))
	}

	if sender.voices[0].size != len("ogg-answer") {
		t.Fatalf("unexpected answer size: %d", sender.voices[0].size)
	}

	if sender.voices[0].caption != "Привет! Я SophyGo." {
		t.Fatalf("unexpected caption: %q", sender.voices[0].caption)
	}
}

func TestHandleVoiceFallsBackToText(t *testing.T) {
	fileServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ogg-question"))
	}))
	defer fileServer.Close()

	sender := &fakeSender{fileURL: fileServer.URL}
	backend := &fakeBackend{voiceText: "Текстовый ответ"}
	handler := newTestHandler(sender, backend)

	handler.handleVoice(&tgbotapi.Message{
		Chat:  &tgbotapi.Chat{ID: 5},
		Voice: &tgbotapi.Voice{FileID: "file-id"},
	})

	if len(sender.voices) != 0 {
		t.Fatalf("expected no voice answers, got %d", len(sender.voices))
	}

	if len(sender.sent) != 1 || sender.sent[0].text != "Текстовый ответ" {
		t.Fatalf("expected text fallback, got %+v", sender.sent)
	}
}

func TestHandleVoiceDownloadError(t *testing.T) {
	sender := &fakeSender{fileErr: errors.New("download failed")}
	handler := newTestHandler(sender, &fakeBackend{})

	handler.handleVoice(&tgbotapi.Message{
		Chat:  &tgbotapi.Chat{ID: 5},
		Voice: &tgbotapi.Voice{FileID: "file-id"},
	})

	if len(sender.sent) != 1 || sender.sent[0].text != voiceDownloadError {
		t.Fatalf("expected download error message, got %+v", sender.sent)
	}
}

func TestHandleVoiceBackendError(t *testing.T) {
	fileServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ogg-question"))
	}))
	defer fileServer.Close()

	sender := &fakeSender{fileURL: fileServer.URL}
	backend := &fakeBackend{voiceErr: errors.New("ai is down")}
	handler := newTestHandler(sender, backend)

	handler.handleVoice(&tgbotapi.Message{
		Chat:  &tgbotapi.Chat{ID: 5},
		Voice: &tgbotapi.Voice{FileID: "file-id"},
	})

	if len(sender.sent) != 1 || sender.sent[0].text != voiceUnavailable {
		t.Fatalf("expected AI unavailable message, got %+v", sender.sent)
	}
}

func TestIsValidCoord(t *testing.T) {
	tests := []struct {
		name string
		lat  float64
		lon  float64
		want bool
	}{
		{name: "origin", lat: 0, lon: 0, want: true},
		{name: "upper bounds", lat: 90, lon: 180, want: true},
		{name: "lower bounds", lat: -90, lon: -180, want: true},
		{name: "berlin", lat: 52.5163, lon: 13.3777, want: true},
		{name: "latitude too high", lat: 90.1, lon: 0, want: false},
		{name: "latitude too low", lat: -90.1, lon: 0, want: false},
		{name: "longitude too high", lat: 0, lon: 180.1, want: false},
		{name: "longitude too low", lat: 0, lon: -180.1, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isValidCoord(tc.lat, tc.lon); got != tc.want {
				t.Fatalf("isValidCoord(%v, %v) = %v, want %v", tc.lat, tc.lon, got, tc.want)
			}
		})
	}
}
