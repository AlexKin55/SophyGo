package ai

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ErrDisabled is returned when the AI integration is disabled or credentials
// are missing.
var ErrDisabled = errors.New("AI service is disabled")

// ErrTooLarge is returned when an audio payload exceeds the configured limit.
var ErrTooLarge = errors.New("audio is too large")

// VoiceAnswer is the result of a voice-to-voice request.
type VoiceAnswer struct {
	// Audio is the answer encoded as OGG/Opus for Telegram.
	Audio []byte
	// Text is the answer text.
	Text string
	// Transcript is the recognized user question.
	Transcript string
}

// Service keeps a short dialogue history per Telegram user.
type Service struct {
	cfg     Config
	log     *slog.Logger
	enabled bool

	speech speechProvider
	chat   chatProvider

	mu       sync.Mutex
	sessions map[int64]*userSession

	stopCh    chan struct{}
	closeOnce sync.Once
}

type userSession struct {
	mu       sync.Mutex
	history  []Message
	lastUsed atomic.Int64
}

// New creates the AI service. When credentials are missing the service stays
// disabled: the backend keeps running and AI endpoints return ErrDisabled.
func New(cfg Config, log *slog.Logger) *Service {
	cfg.ApplyDefaults()

	provider := newYandexProvider(cfg)

	s := &Service{
		cfg:      cfg,
		log:      log,
		speech:   provider,
		chat:     provider,
		sessions: make(map[int64]*userSession),
		stopCh:   make(chan struct{}),
	}

	key, folder := cfg.Credentials()
	s.enabled = cfg.Enabled && key != "" && folder != ""

	switch {
	case !cfg.Enabled:
		log.Info("AI integration is disabled in settings")
	case !s.enabled:
		log.Warn("AI integration is enabled but Yandex credentials are missing; AI endpoints will return 503")
	}

	if s.enabled {
		go s.cleanupLoop()
	}

	return s
}

// Enabled reports whether the AI integration is ready to use.
func (s *Service) Enabled() bool {
	return s.enabled
}

// MaxAudioBytes is the maximum accepted voice payload size.
func (s *Service) MaxAudioBytes() int64 {
	return s.cfg.MaxAudioBytes
}

// Start opens a new dialogue for the user. It is called when the user sends
// /start; an existing history is replaced.
func (s *Service) Start(_ context.Context, userID int64) error {
	if !s.enabled {
		return ErrDisabled
	}
	if userID <= 0 {
		return errors.New("invalid user id")
	}

	s.mu.Lock()
	s.sessions[userID] = newUserSession(s.cfg.SystemPrompt)
	s.mu.Unlock()

	return nil
}

// Stop closes the user's dialogue.
func (s *Service) Stop(userID int64) error {
	s.mu.Lock()
	delete(s.sessions, userID)
	s.mu.Unlock()

	return nil
}

// Ask recognizes the voice question, generates a text answer and synthesizes
// it back to OGG/Opus.
func (s *Service) Ask(ctx context.Context, userID int64, ogg []byte) (VoiceAnswer, error) {
	if !s.enabled {
		return VoiceAnswer{}, ErrDisabled
	}
	if int64(len(ogg)) > s.cfg.MaxAudioBytes {
		return VoiceAnswer{}, ErrTooLarge
	}

	us, err := s.getOrCreate(userID)
	if err != nil {
		return VoiceAnswer{}, err
	}

	us.mu.Lock()
	defer us.mu.Unlock()

	us.lastUsed.Store(time.Now().UnixNano())

	transcript, err := s.speech.Recognize(ctx, ogg)
	if err != nil {
		return VoiceAnswer{}, fmt.Errorf("recognize speech: %w", err)
	}

	us.history = append(us.history, Message{Role: "user", Text: transcript})

	answer, err := s.chat.Complete(ctx, s.historySnapshot(us.history))
	if err != nil {
		us.history = us.history[:len(us.history)-1]
		return VoiceAnswer{}, fmt.Errorf("generate answer: %w", err)
	}

	answer = strings.TrimSpace(answer)
	if answer == "" {
		us.history = us.history[:len(us.history)-1]
		return VoiceAnswer{}, errors.New("yandexgpt returned an empty answer")
	}

	us.history = append(us.history, Message{Role: "assistant", Text: answer})
	s.trimHistory(us)

	audio, err := s.speech.Synthesize(ctx, answer)
	if err != nil {
		// Do not keep an answer the user never heard.
		us.history = us.history[:len(us.history)-1]
		return VoiceAnswer{}, fmt.Errorf("synthesize answer: %w", err)
	}

	return VoiceAnswer{
		Audio:      audio,
		Text:       answer,
		Transcript: transcript,
	}, nil
}

// Close stops the cleanup goroutine and drops all sessions.
func (s *Service) Close() {
	s.closeOnce.Do(func() {
		close(s.stopCh)
	})

	s.mu.Lock()
	s.sessions = make(map[int64]*userSession)
	s.mu.Unlock()
}

// SessionCount returns the number of open dialogues (used by tests/health).
func (s *Service) SessionCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.sessions)
}

func newUserSession(systemPrompt string) *userSession {
	us := &userSession{
		history: []Message{{Role: "system", Text: systemPrompt}},
	}
	us.lastUsed.Store(time.Now().UnixNano())

	return us
}

func (s *Service) getOrCreate(userID int64) (*userSession, error) {
	if userID <= 0 {
		return nil, errors.New("invalid user id")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if us := s.sessions[userID]; us != nil {
		us.lastUsed.Store(time.Now().UnixNano())
		return us, nil
	}

	us := newUserSession(s.cfg.SystemPrompt)
	s.sessions[userID] = us

	return us, nil
}

func (s *Service) historySnapshot(history []Message) []Message {
	limit := s.cfg.HistoryLimit
	if limit <= 0 || len(history) <= limit {
		return append([]Message(nil), history...)
	}

	out := make([]Message, 0, limit)
	out = append(out, history[0]) // system prompt
	out = append(out, history[len(history)-(limit-1):]...)

	return out
}

func (s *Service) trimHistory(us *userSession) {
	limit := s.cfg.HistoryLimit
	if limit <= 0 || len(us.history) <= limit {
		return
	}

	system := us.history[0]
	tail := us.history[len(us.history)-(limit-1):]

	trimmed := make([]Message, 0, limit)
	trimmed = append(trimmed, system)
	trimmed = append(trimmed, tail...)

	us.history = trimmed
}

func (s *Service) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.cleanup()
		}
	}
}

func (s *Service) cleanup() {
	ttl := s.cfg.SessionTTL.Std()
	now := time.Now()

	s.mu.Lock()
	for id, us := range s.sessions {
		if now.Sub(time.Unix(0, us.lastUsed.Load())) > ttl {
			delete(s.sessions, id)
		}
	}
	s.mu.Unlock()
}
