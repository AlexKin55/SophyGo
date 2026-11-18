package api

import (
	"log/slog"
	"net/http"
)

type HealthResponse struct {
	Status string `json:"status"`
}

func HealthHandler(log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		log.Debug("health check requested")

		writeJSON(w, http.StatusOK, HealthResponse{Status: "ok"}, log)
	}
}
