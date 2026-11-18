package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestLocationHandlerAcceptsValidCoordinates(t *testing.T) {
	handler := LocationHandler(discardLogger())

	body := `{"latitude":52.5163,"longitude":13.3777}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/guide/location", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	if got := w.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("expected content-type application/json, got %q", got)
	}

	var resp LocationResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Message == "" {
		t.Fatal("expected non-empty message")
	}
}

func TestLocationHandlerRejectsInvalidRequests(t *testing.T) {
	handler := LocationHandler(discardLogger())

	tests := []struct {
		name string
		body string
	}{
		{name: "latitude too high", body: `{"latitude":90.1,"longitude":0}`},
		{name: "latitude too low", body: `{"latitude":-90.1,"longitude":0}`},
		{name: "longitude too high", body: `{"latitude":0,"longitude":180.1}`},
		{name: "longitude too low", body: `{"latitude":0,"longitude":-180.1}`},
		{name: "malformed json", body: `{"latitude":`},
		{name: "empty body", body: ``},
		{name: "unknown field", body: `{"latitude":1,"longitude":2,"extra":true}`},
		{name: "trailing data", body: `{"latitude":1,"longitude":2}{}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/guide/location", strings.NewReader(tc.body))
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected status %d, got %d", http.StatusBadRequest, w.Code)
			}

			var resp ErrorResponse
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to unmarshal error response: %v", err)
			}

			if resp.Error == "" {
				t.Fatal("expected non-empty error message")
			}
		})
	}
}

func TestLocationHandlerRejectsOversizedBody(t *testing.T) {
	handler := LocationHandler(discardLogger())

	body := `{"latitude":1,"longitude":2,"padding":"` + strings.Repeat("a", maxLocationBodyBytes) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/guide/location", strings.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected status %d, got %d", http.StatusRequestEntityTooLarge, w.Code)
	}
}

func TestLocationHandlerRejectsOtherMethods(t *testing.T) {
	handler := LocationHandler(discardLogger())

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		req := httptest.NewRequest(method, "/api/v1/guide/location", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("method %s: expected status %d, got %d", method, http.StatusMethodNotAllowed, w.Code)
		}

		if got := w.Header().Get("Allow"); got != http.MethodPost {
			t.Fatalf("method %s: expected Allow header %q, got %q", method, http.MethodPost, got)
		}
	}
}

func TestValidCoordinates(t *testing.T) {
	tests := []struct {
		name string
		lat  float64
		lon  float64
		want bool
	}{
		{name: "origin", lat: 0, lon: 0, want: true},
		{name: "north east bounds", lat: 90, lon: 180, want: true},
		{name: "south west bounds", lat: -90, lon: -180, want: true},
		{name: "berlin", lat: 52.5163, lon: 13.3777, want: true},
		{name: "latitude too high", lat: 90.1, lon: 0, want: false},
		{name: "latitude too low", lat: -90.1, lon: 0, want: false},
		{name: "longitude too high", lat: 0, lon: 180.1, want: false},
		{name: "longitude too low", lat: 0, lon: -180.1, want: false},
		{name: "latitude NaN", lat: math.NaN(), lon: 0, want: false},
		{name: "longitude Inf", lat: 0, lon: math.Inf(1), want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidCoordinates(tc.lat, tc.lon); got != tc.want {
				t.Fatalf("ValidCoordinates(%v, %v) = %v, want %v", tc.lat, tc.lon, got, tc.want)
			}
		})
	}
}
