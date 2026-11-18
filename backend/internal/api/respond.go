package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// writeJSON sends v as a JSON response with the given HTTP status code.
func writeJSON(w http.ResponseWriter, status int, v any, log *slog.Logger) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Error("failed to encode json response", "error", err)
	}
}
