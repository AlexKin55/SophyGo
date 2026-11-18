package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"travelbuddy/backend/internal/ai"
	"travelbuddy/backend/internal/api"
	"travelbuddy/backend/internal/config"
	"travelbuddy/internal/settings"
)

func main() {
	envFile := strings.TrimSpace(os.Getenv("BACKEND_ENV_FILE"))
	if envFile == "" {
		envFile = "backend/.env.backend"
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

	aiService := ai.New(cfg.AI, log)
	defer aiService.Close()

	mux := http.NewServeMux()
	mux.HandleFunc(cfg.API.HealthPath, api.HealthHandler(log))
	mux.HandleFunc(cfg.API.LocationPath, api.LocationHandlerWithLimit(log, cfg.API.MaxLocationBodyBytes))
	mux.HandleFunc(cfg.API.AISessionPath, api.AISessionStartHandler(aiService, log))
	mux.HandleFunc(cfg.API.AISessionStopPath, api.AISessionStopHandler(aiService, log))
	mux.HandleFunc(cfg.API.AIVoicePath, api.AIVoiceHandler(aiService, log))

	server := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           mux,
		ReadHeaderTimeout: cfg.Server.ReadHeaderTimeout.Std(),
	}

	idleConnsClosed := make(chan struct{})
	go func() {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt, syscall.SIGTERM)
		<-sigint

		log.Info("shutting down server")

		ctx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout.Std())
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			log.Error("server shutdown error", "error", err)
		}
		close(idleConnsClosed)
	}()

	log.Info("starting backend", "addr", cfg.Server.Addr)

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Error("server error", "error", err)
		os.Exit(1)
	}

	<-idleConnsClosed
	log.Info("server stopped")
}
