package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"net/http"
)

const (
	maxLocationBodyBytes = 64 << 10

	// stubLocationMessage is a temporary response used until attraction search
	// and LLM integration are implemented. It must not be presented as a real
	// AI answer.
	stubLocationMessage = "Привет, путешественник! Я получил твои координаты. Скоро расскажу, что интересного находится вокруг."
)

// LocationRequest is the payload sent by the Telegram bot when a user shares
// their location.
type LocationRequest struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// LocationResponse is returned by the guide location endpoint.
type LocationResponse struct {
	Message string `json:"message"`
}

// ErrorResponse is a safe error payload that never exposes internal details.
type ErrorResponse struct {
	Error string `json:"error"`
}

// LocationHandler handles POST /api/v1/guide/location with the default body
// size limit.
func LocationHandler(log *slog.Logger) http.HandlerFunc {
	return LocationHandlerWithLimit(log, maxLocationBodyBytes)
}

// LocationHandlerWithLimit handles POST /api/v1/guide/location.
//
// For now it validates the coordinates and returns a temporary stub message;
// it does not perform attraction search or call an LLM.
func LocationHandlerWithLimit(log *slog.Logger, maxBodyBytes int64) http.HandlerFunc {
	if maxBodyBytes <= 0 {
		maxBodyBytes = maxLocationBodyBytes
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"}, log)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		defer r.Body.Close()

		var req LocationRequest

		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()

		if err := decoder.Decode(&req); err != nil {
			status := http.StatusBadRequest

			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				status = http.StatusRequestEntityTooLarge
			}

			log.Debug("invalid location request body", "error", err)
			writeJSON(w, status, ErrorResponse{Error: "invalid request body"}, log)
			return
		}

		// Reject trailing JSON values such as `{"latitude":1}{"longitude":2}`.
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			log.Debug("unexpected trailing data in location request")
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"}, log)
			return
		}

		if !ValidCoordinates(req.Latitude, req.Longitude) {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid coordinates"}, log)
			return
		}

		writeJSON(w, http.StatusOK, LocationResponse{Message: stubLocationMessage}, log)
	}
}

// ValidCoordinates reports whether lat and lon are finite values within their
// geographical ranges: latitude -90..90, longitude -180..180.
func ValidCoordinates(lat, lon float64) bool {
	if math.IsNaN(lat) || math.IsInf(lat, 0) || math.IsNaN(lon) || math.IsInf(lon, 0) {
		return false
	}

	return lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180
}
