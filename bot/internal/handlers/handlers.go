package handlers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const (
	startText = `Привет! Я SophyGo — твой гид по интересным местам! 🧭
Меня придумала и развивает София.
Поделись геопозицией или запиши голосовое сообщение — и давай исследовать мир вместе!`

	btnLocation = "📍 Рассказать о месте"

	invalidCoords      = "Пожалуйста, отправьте корректную геопозицию."
	backendUnavailable = "Сервер временно недоступен. Попробуйте позже."
	backendError       = "Произошла ошибка при обработке запроса. Попробуйте ещё раз."

	voiceDownloadError = "Не удалось загрузить голосовое сообщение. Попробуйте ещё раз."
	voiceUnavailable   = "Не удалось получить голосовой ответ. Попробуйте позже."
	voiceEmptyAnswer   = "Не получилось подготовить ответ. Попробуйте переформулировать вопрос."

	defaultMaxVoiceBytes = 10 << 20
	voiceDownloadTimeout = 30 * time.Second
)

// TelegramAPI is the subset of the Telegram bot API used by Handler.
type TelegramAPI interface {
	Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
	GetFileDirectURL(fileID string) (string, error)
}

type Handler struct {
	bot            TelegramAPI
	backend        BackendClient
	log            *slog.Logger
	maxVoiceBytes  int64
	downloadClient *http.Client
}

type BackendClient interface {
	SendLocation(lat, lon float64) (string, error)
	StartAISession(userID int64) error
	StopAISession(userID int64) error
	AskVoice(userID int64, audio []byte) ([]byte, string, string, error)
}

type Options struct {
	MaxVoiceBytes int64
}

func New(bot TelegramAPI, backend BackendClient, log *slog.Logger, opts ...Options) *Handler {
	maxVoiceBytes := int64(defaultMaxVoiceBytes)
	if len(opts) > 0 && opts[0].MaxVoiceBytes > 0 {
		maxVoiceBytes = opts[0].MaxVoiceBytes
	}

	return &Handler{
		bot:           bot,
		backend:       backend,
		log:           log,
		maxVoiceBytes: maxVoiceBytes,
		downloadClient: &http.Client{
			Timeout: voiceDownloadTimeout,
		},
	}
}

func (h *Handler) HandleUpdate(update tgbotapi.Update) {
	if update.Message == nil {
		return
	}

	msg := update.Message

	if msg.IsCommand() {
		h.handleCommand(msg)
		return
	}

	if msg.Location != nil {
		h.handleLocation(msg)
		return
	}

	if msg.Voice != nil {
		go h.handleVoice(msg)
		return
	}

	h.sendText(msg.Chat.ID, "Пришли мне геопозицию, голосовое сообщение или нажми /start.")
}

func (h *Handler) handleCommand(msg *tgbotapi.Message) {
	switch msg.Command() {
	case "start":
		userID := h.userID(msg)
		h.sendWelcome(msg.Chat.ID)
		go h.startAISession(userID)
	default:
		h.sendText(msg.Chat.ID, "Неизвестная команда. Используйте /start.")
	}
}

func (h *Handler) startAISession(userID int64) {
	if err := h.backend.StartAISession(userID); err != nil {
		h.log.Warn("failed to start AI session", "error", err, "user_id", userID)
	}
}

func (h *Handler) sendWelcome(chatID int64) {
	keyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButtonLocation(btnLocation),
		),
	)
	keyboard.ResizeKeyboard = true

	msg := tgbotapi.NewMessage(chatID, startText)
	msg.ReplyMarkup = keyboard

	if _, err := h.bot.Send(msg); err != nil {
		h.log.Error("failed to send welcome message", "error", err, "chat_id", chatID)
	}
}

func (h *Handler) handleLocation(msg *tgbotapi.Message) {
	lat := msg.Location.Latitude
	lon := msg.Location.Longitude

	if !isValidCoord(lat, lon) {
		h.sendText(msg.Chat.ID, invalidCoords)
		return
	}

	message, err := h.backend.SendLocation(lat, lon)
	if err != nil {
		h.log.Error("backend request failed", "error", err, "chat_id", msg.Chat.ID)
		h.sendText(msg.Chat.ID, backendUnavailable)
		return
	}

	h.sendText(msg.Chat.ID, message)
}

func (h *Handler) handleVoice(msg *tgbotapi.Message) {
	userID := h.userID(msg)

	audio, err := h.downloadVoice(msg.Voice.FileID)
	if err != nil {
		h.log.Error("failed to download voice message", "error", err, "chat_id", msg.Chat.ID)
		h.sendText(msg.Chat.ID, voiceDownloadError)
		return
	}

	answerAudio, answerText, _, err := h.backend.AskVoice(userID, audio)
	if err != nil {
		h.log.Error("voice request failed", "error", err, "user_id", userID)
		h.sendText(msg.Chat.ID, voiceUnavailable)
		return
	}

	if len(answerAudio) > 0 {
		if err := h.sendVoice(msg.Chat.ID, answerAudio, answerText); err != nil {
			h.log.Error("failed to send voice answer", "error", err, "chat_id", msg.Chat.ID)
			if answerText != "" {
				h.sendText(msg.Chat.ID, answerText)
			}
			return
		}
		return
	}

	if answerText != "" {
		h.sendText(msg.Chat.ID, answerText)
		return
	}

	h.sendText(msg.Chat.ID, voiceEmptyAnswer)
}

func (h *Handler) downloadVoice(fileID string) ([]byte, error) {
	url, err := h.bot.GetFileDirectURL(fileID)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), voiceDownloadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := h.downloadClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("telegram file download failed with status " + resp.Status)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, h.maxVoiceBytes+1))
	if err != nil {
		return nil, err
	}

	if int64(len(data)) > h.maxVoiceBytes {
		return nil, errors.New("voice message is too large")
	}

	if len(data) == 0 {
		return nil, errors.New("voice message is empty")
	}

	return data, nil
}

func (h *Handler) sendVoice(chatID int64, audio []byte, text string) error {
	voice := tgbotapi.NewVoice(chatID, tgbotapi.FileBytes{
		Name:  "answer.ogg",
		Bytes: audio,
	})
	voice.Caption = truncateText(text, 1024)

	_, err := h.bot.Send(voice)
	return err
}

func (h *Handler) userID(msg *tgbotapi.Message) int64 {
	if msg.From != nil && msg.From.ID != 0 {
		return msg.From.ID
	}

	return msg.Chat.ID
}

func (h *Handler) sendText(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	if _, err := h.bot.Send(msg); err != nil {
		h.log.Error("failed to send message", "error", err, "chat_id", chatID)
	}
}

func truncateText(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}

	return string(runes[:limit])
}

func isValidCoord(lat, lon float64) bool {
	return lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180
}
