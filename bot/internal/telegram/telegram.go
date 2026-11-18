package telegram

import (
	"log/slog"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"travelbuddy/bot/internal/handlers"
)

type Bot struct {
	api     *tgbotapi.BotAPI
	handler *handlers.Handler
	log     *slog.Logger
}

func New(token string, backend handlers.BackendClient, log *slog.Logger, maxVoiceBytes int64) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, err
	}

	api.Debug = false

	handler := handlers.New(api, backend, log, handlers.Options{
		MaxVoiceBytes: maxVoiceBytes,
	})

	return &Bot{
		api:     api,
		handler: handler,
		log:     log,
	}, nil
}

func (b *Bot) API() *tgbotapi.BotAPI {
	return b.api
}

func (b *Bot) Handler() *handlers.Handler {
	return b.handler
}
