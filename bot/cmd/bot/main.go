package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"travelbuddy/bot/internal/backendclient"
	"travelbuddy/bot/internal/config"
	"travelbuddy/bot/internal/telegram"
	"travelbuddy/internal/settings"
)

func main() {
	envFile := strings.TrimSpace(os.Getenv("BOT_ENV_FILE"))
	if envFile == "" {
		envFile = "bot/.env.bot"
	}

	if err := settings.LoadEnvFile(envFile); err != nil {
		fmt.Fprintln(os.Stderr, "failed to load env file:", err)
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to load settings:", err)
		os.Exit(1)
	}

	log := settings.Logger(cfg.Log.Level)

	if cfg.TelegramBotToken == "" {
		log.Error("TELEGRAM_BOT_TOKEN is not set")
		os.Exit(1)
	}

	backend := backendclient.New(cfg.Backend.URL, backendclient.Options{
		LocationPath:       cfg.Backend.LocationPath,
		AISessionPath:      cfg.AI.SessionPath,
		AISessionStopPath:  cfg.AI.SessionStopPath,
		AIVoicePath:        cfg.AI.VoicePath,
		Timeout:            cfg.Backend.Timeout.Std(),
		AITimeout:          cfg.AI.Timeout.Std(),
		Retries:            cfg.Backend.Retries,
		RetryBackoff:       cfg.Backend.RetryBackoff.Std(),
		MaxResponseBytes:   cfg.Backend.MaxResponseBytes,
		AIMaxResponseBytes: cfg.AI.MaxVoiceBytes*4/3 + 1<<20,
		MaxErrorBodyBytes:  cfg.Backend.MaxErrorBodyBytes,
	})

	bot, err := telegram.New(cfg.TelegramBotToken, backend, log, cfg.AI.MaxVoiceBytes)
	if err != nil {
		log.Error("failed to create bot", "error", err)
		os.Exit(1)
	}

	log.Info("bot started", "username", bot.API().Self.UserName)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	u := tgbotapi.NewUpdate(0)
	u.Timeout = cfg.Telegram.UpdateTimeout

	updates := bot.API().GetUpdatesChan(u)

	for {
		select {
		case <-ctx.Done():
			log.Info("shutting down bot")
			bot.API().StopReceivingUpdates()
			return
		case update, ok := <-updates:
			if !ok {
				return
			}

			bot.Handler().HandleUpdate(update)
		}
	}
}
