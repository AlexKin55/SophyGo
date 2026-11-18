package backendclient

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testLocationPath = "/api/v1/guide/location"

func testOptions() Options {
	return Options{
		LocationPath:      testLocationPath,
		Timeout:           2 * time.Second,
		Retries:           0,
		RetryBackoff:      0,
		MaxResponseBytes:  1 << 20,
		MaxErrorBodyBytes: 4 << 10,
	}
}

func TestSendLocationSuccess(t *testing.T) {
	var (
		gotPath   string
		gotMethod string
		gotReq    LocationRequest
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method

		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected content-type application/json, got %q", ct)
		}

		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"message":"привет, путешественник"}`)
	}))
	defer server.Close()

	client := New(server.URL+"/", testOptions())
	msg, err := client.SendLocation(52.5163, 13.3777)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msg != "привет, путешественник" {
		t.Fatalf("unexpected message: %q", msg)
	}

	if gotMethod != http.MethodPost {
		t.Fatalf("expected POST, got %s", gotMethod)
	}

	if gotPath != testLocationPath {
		t.Fatalf("expected path %q, got %q", testLocationPath, gotPath)
	}

	if gotReq.Latitude != 52.5163 || gotReq.Longitude != 13.3777 {
		t.Fatalf("unexpected coordinates: %+v", gotReq)
	}
}

func TestSendLocationNon2xxStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	_, err := New(server.URL, testOptions()).SendLocation(52.5163, 13.3777)
	if err == nil {
		t.Fatal("expected error for non-2xx response")
	}

	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected status in error, got %q", err.Error())
	}
}

func TestSendLocationMalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{`)
	}))
	defer server.Close()

	_, err := New(server.URL, testOptions()).SendLocation(52.5163, 13.3777)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestSendLocationEmptyMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"message":""}`)
	}))
	defer server.Close()

	_, err := New(server.URL, testOptions()).SendLocation(52.5163, 13.3777)
	if err == nil {
		t.Fatal("expected error for empty message")
	}
}

func TestSendLocationUnreachableBackend(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := server.URL
	server.Close()

	_, err := New(url, testOptions()).SendLocation(52.5163, 13.3777)
	if err == nil {
		t.Fatal("expected error for unreachable backend")
	}
}

func TestSendLocationRetriesOnServerError(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}

		_, _ = io.WriteString(w, `{"message":"ok"}`)
	}))
	defer server.Close()

	opts := testOptions()
	opts.Retries = 1

	msg, err := New(server.URL, opts).SendLocation(1, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msg != "ok" {
		t.Fatalf("unexpected message: %q", msg)
	}

	if got := attempts.Load(); got != 2 {
		t.Fatalf("expected 2 attempts, got %d", got)
	}
}

func TestSendLocationDoesNotRetryClientError(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer server.Close()

	opts := testOptions()
	opts.Retries = 3

	_, err := New(server.URL, opts).SendLocation(1, 2)
	if err == nil {
		t.Fatal("expected error for 400 response")
	}

	if got := attempts.Load(); got != 1 {
		t.Fatalf("expected 1 attempt, got %d", got)
	}
}

func TestSendLocationRetriesExhausted(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer server.Close()

	opts := testOptions()
	opts.Retries = 2

	_, err := New(server.URL, opts).SendLocation(1, 2)
	if err == nil {
		t.Fatal("expected error after retries are exhausted")
	}

	if got := attempts.Load(); got != 3 {
		t.Fatalf("expected 3 attempts, got %d", got)
	}
}

func aiTestOptions() Options {
	opts := testOptions()
	opts.AISessionPath = "/api/v1/ai/session"
	opts.AISessionStopPath = "/api/v1/ai/session/stop"
	opts.AIVoicePath = "/api/v1/ai/voice"
	opts.AITimeout = 2 * time.Second

	return opts
}

func TestStartAndStopAISession(t *testing.T) {
	var (
		paths  []string
		bodies []aiSessionRequest
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)

		var req aiSessionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}
		bodies = append(bodies, req)

		_, _ = io.WriteString(w, `{"status":"ok"}`)
	}))
	defer server.Close()

	client := New(server.URL, aiTestOptions())

	if err := client.StartAISession(42); err != nil {
		t.Fatalf("start session: %v", err)
	}
	if err := client.StopAISession(42); err != nil {
		t.Fatalf("stop session: %v", err)
	}

	if len(paths) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(paths))
	}
	if paths[0] != "/api/v1/ai/session" || paths[1] != "/api/v1/ai/session/stop" {
		t.Fatalf("unexpected paths: %v", paths)
	}
	if len(bodies) != 2 || bodies[0].UserID != 42 || bodies[1].UserID != 42 {
		t.Fatalf("unexpected bodies: %+v", bodies)
	}
}

func TestAskVoice(t *testing.T) {
	answer := base64.StdEncoding.EncodeToString([]byte("ogg-answer"))

	var got aiVoiceRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		_, _ = io.WriteString(w, `{"audio":"`+answer+`","text":"hello","transcript":"question"}`)
	}))
	defer server.Close()

	out, text, transcript, err := New(server.URL, aiTestOptions()).AskVoice(7, []byte("ogg-question"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.UserID != 7 || got.Format != "ogg" {
		t.Fatalf("unexpected request: %+v", got)
	}

	decoded, err := base64.StdEncoding.DecodeString(got.Audio)
	if err != nil || string(decoded) != "ogg-question" {
		t.Fatalf("unexpected audio payload: %q, %v", decoded, err)
	}

	if string(out) != "ogg-answer" || text != "hello" || transcript != "question" {
		t.Fatalf("unexpected answer: %q, %q, %q", out, text, transcript)
	}
}

func TestAskVoiceRejectsInvalidBase64(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"audio":"!!!"}`)
	}))
	defer server.Close()

	if _, _, _, err := New(server.URL, aiTestOptions()).AskVoice(7, []byte("ogg-question")); err == nil {
		t.Fatal("expected error for invalid base64 answer")
	}
}
