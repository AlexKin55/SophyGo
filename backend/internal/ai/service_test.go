package ai

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeSpeech struct {
	transcript string
	audio      []byte
	recErr     error
	ttsErr     error

	recCalls int
	ttsCalls int
	gotAudio []byte
	gotText  string
}

func (f *fakeSpeech) Recognize(_ context.Context, ogg []byte) (string, error) {
	f.recCalls++
	f.gotAudio = ogg

	return f.transcript, f.recErr
}

func (f *fakeSpeech) Synthesize(_ context.Context, text string) ([]byte, error) {
	f.ttsCalls++
	f.gotText = text

	return f.audio, f.ttsErr
}

type fakeChat struct {
	answer string
	err    error

	calls int
	got   []Message
}

func (f *fakeChat) Complete(_ context.Context, messages []Message) (string, error) {
	f.calls++
	f.got = append([]Message(nil), messages...)

	return f.answer, f.err
}

func newTestService(t *testing.T) (*Service, *fakeSpeech, *fakeChat) {
	t.Helper()

	t.Setenv("YANDEX_API_KEY", "")
	t.Setenv("YANDEX_FOLDER_ID", "")

	cfg := Config{
		Enabled:       true,
		APIKey:        "key",
		FolderID:      "folder",
		MaxAudioBytes: 100,
	}
	cfg.ApplyDefaults()

	svc := New(cfg, discardLogger())

	speech := &fakeSpeech{
		transcript: "вопрос",
		audio:      []byte("ogg-answer"),
	}
	chat := &fakeChat{answer: "ответ"}

	svc.speech = speech
	svc.chat = chat

	t.Cleanup(svc.Close)

	return svc, speech, chat
}

func TestServiceStartAskStop(t *testing.T) {
	svc, speech, chat := newTestService(t)
	ctx := context.Background()

	if err := svc.Start(ctx, 42); err != nil {
		t.Fatalf("start: %v", err)
	}
	if svc.SessionCount() != 1 {
		t.Fatalf("expected 1 session, got %d", svc.SessionCount())
	}

	answer, err := svc.Ask(ctx, 42, []byte("ogg-question"))
	if err != nil {
		t.Fatalf("ask: %v", err)
	}

	if speech.recCalls != 1 || string(speech.gotAudio) != "ogg-question" {
		t.Fatalf("unexpected recognize call: %d, %q", speech.recCalls, speech.gotAudio)
	}
	if speech.ttsCalls != 1 || speech.gotText != "ответ" {
		t.Fatalf("unexpected synthesize call: %d, %q", speech.ttsCalls, speech.gotText)
	}
	if chat.calls != 1 {
		t.Fatalf("expected 1 GPT call, got %d", chat.calls)
	}

	if string(answer.Audio) != "ogg-answer" || answer.Text != "ответ" || answer.Transcript != "вопрос" {
		t.Fatalf("unexpected answer: %+v", answer)
	}

	if len(chat.got) != 2 {
		t.Fatalf("expected system + user messages, got %d", len(chat.got))
	}
	if chat.got[0].Role != "system" || chat.got[1].Role != "user" || chat.got[1].Text != "вопрос" {
		t.Fatalf("unexpected GPT messages: %+v", chat.got)
	}

	if err := svc.Stop(42); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if svc.SessionCount() != 0 {
		t.Fatalf("expected no sessions, got %d", svc.SessionCount())
	}
}

func TestServiceKeepsDialogueContext(t *testing.T) {
	svc, _, chat := newTestService(t)
	ctx := context.Background()

	if _, err := svc.Ask(ctx, 1, []byte("ogg-1")); err != nil {
		t.Fatalf("first ask: %v", err)
	}
	if _, err := svc.Ask(ctx, 1, []byte("ogg-2")); err != nil {
		t.Fatalf("second ask: %v", err)
	}

	if len(chat.got) != 4 {
		t.Fatalf("expected system,user,assistant,user, got %d messages", len(chat.got))
	}
	if chat.got[0].Role != "system" || chat.got[1].Role != "user" ||
		chat.got[2].Role != "assistant" || chat.got[3].Role != "user" {
		t.Fatalf("unexpected history: %+v", chat.got)
	}
}

func TestServiceStartResetsHistory(t *testing.T) {
	svc, _, chat := newTestService(t)
	ctx := context.Background()

	if _, err := svc.Ask(ctx, 1, []byte("ogg-1")); err != nil {
		t.Fatalf("first ask: %v", err)
	}

	if err := svc.Start(ctx, 1); err != nil {
		t.Fatalf("start: %v", err)
	}

	if _, err := svc.Ask(ctx, 1, []byte("ogg-2")); err != nil {
		t.Fatalf("second ask: %v", err)
	}

	if len(chat.got) != 2 || chat.got[0].Role != "system" || chat.got[1].Role != "user" {
		t.Fatalf("expected reset history, got %+v", chat.got)
	}
}

func TestServiceTrimsHistory(t *testing.T) {
	svc, _, chat := newTestService(t)
	svc.cfg.HistoryLimit = 4
	ctx := context.Background()

	for i := 0; i < 4; i++ {
		if _, err := svc.Ask(ctx, 1, []byte("ogg")); err != nil {
			t.Fatalf("ask %d: %v", i, err)
		}
	}

	if len(chat.got) > 4 {
		t.Fatalf("history was not trimmed: %d messages", len(chat.got))
	}
	if chat.got[0].Role != "system" {
		t.Fatalf("system prompt was lost: %+v", chat.got)
	}
}

func TestServiceDisabled(t *testing.T) {
	t.Setenv("YANDEX_API_KEY", "")
	t.Setenv("YANDEX_FOLDER_ID", "")

	cfg := Config{Enabled: false}
	cfg.ApplyDefaults()

	svc := New(cfg, discardLogger())
	defer svc.Close()

	if svc.Enabled() {
		t.Fatal("expected service to be disabled")
	}

	if err := svc.Start(context.Background(), 1); !errors.Is(err, ErrDisabled) {
		t.Fatalf("expected ErrDisabled, got %v", err)
	}
	if _, err := svc.Ask(context.Background(), 1, []byte("ogg")); !errors.Is(err, ErrDisabled) {
		t.Fatalf("expected ErrDisabled, got %v", err)
	}
}

func TestServiceRejectsTooLargeAudio(t *testing.T) {
	svc, _, _ := newTestService(t)

	_, err := svc.Ask(context.Background(), 1, make([]byte, 101))
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("expected ErrTooLarge, got %v", err)
	}
}

func TestServiceRollsBackHistoryOnErrors(t *testing.T) {
	svc, speech, chat := newTestService(t)
	ctx := context.Background()

	chat.err = errors.New("gpt is down")
	if _, err := svc.Ask(ctx, 1, []byte("ogg")); err == nil {
		t.Fatal("expected GPT error")
	}

	svc.mu.Lock()
	history := append([]Message(nil), svc.sessions[1].history...)
	svc.mu.Unlock()

	if len(history) != 1 || history[0].Role != "system" {
		t.Fatalf("expected rollback to system prompt, got %+v", history)
	}

	chat.err = nil
	speech.ttsErr = errors.New("tts is down")
	if _, err := svc.Ask(ctx, 1, []byte("ogg")); err == nil {
		t.Fatal("expected TTS error")
	}

	svc.mu.Lock()
	history = append([]Message(nil), svc.sessions[1].history...)
	svc.mu.Unlock()

	if len(history) != 2 || history[1].Role != "user" {
		t.Fatalf("expected user message to remain after TTS error, got %+v", history)
	}
}

func TestServiceCleanupIdleSessions(t *testing.T) {
	svc, _, _ := newTestService(t)

	if err := svc.Start(context.Background(), 5); err != nil {
		t.Fatalf("start: %v", err)
	}

	svc.mu.Lock()
	us := svc.sessions[5]
	svc.mu.Unlock()

	us.lastUsed.Store(time.Now().Add(-2 * svc.cfg.SessionTTL.Std()).UnixNano())

	svc.cleanup()

	if svc.SessionCount() != 0 {
		t.Fatalf("expected no sessions, got %d", svc.SessionCount())
	}
}
