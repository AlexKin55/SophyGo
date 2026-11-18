package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testProviderConfig(endpoint string) Config {
	cfg := Config{
		Enabled:  true,
		APIKey:   "key",
		FolderID: "folder",
	}
	cfg.ApplyDefaults()
	cfg.STT.Endpoint = endpoint
	cfg.GPT.Endpoint = endpoint
	cfg.TTS.Endpoint = endpoint
	cfg.MaxAnswerBytes = 1024

	return cfg
}

func TestRecognize(t *testing.T) {
	t.Setenv("YANDEX_API_KEY", "")
	t.Setenv("YANDEX_FOLDER_ID", "")

	var (
		gotQuery map[string]string
		gotAuth  string
		gotBody  []byte
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = map[string]string{
			"folderId": r.URL.Query().Get("folderId"),
			"lang":     r.URL.Query().Get("lang"),
			"format":   r.URL.Query().Get("format"),
			"topic":    r.URL.Query().Get("topic"),
		}
		gotAuth = r.Header.Get("Authorization")
		gotBody, _ = io.ReadAll(r.Body)

		_, _ = io.WriteString(w, `{"result":"привет"}`)
	}))
	defer server.Close()

	provider := newYandexProvider(testProviderConfig(server.URL))
	transcript, err := provider.Recognize(context.Background(), []byte("ogg"))
	if err != nil {
		t.Fatalf("recognize: %v", err)
	}

	if transcript != "привет" {
		t.Fatalf("unexpected transcript: %q", transcript)
	}
	if string(gotBody) != "ogg" {
		t.Fatalf("unexpected body: %q", gotBody)
	}
	if gotAuth != "Api-Key key" {
		t.Fatalf("unexpected auth: %q", gotAuth)
	}
	if gotQuery["folderId"] != "folder" || gotQuery["lang"] != "ru-RU" ||
		gotQuery["format"] != "oggopus" || gotQuery["topic"] != "general" {
		t.Fatalf("unexpected query: %+v", gotQuery)
	}
}

func TestRecognizeErrorStatus(t *testing.T) {
	t.Setenv("YANDEX_API_KEY", "")
	t.Setenv("YANDEX_FOLDER_ID", "")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	provider := newYandexProvider(testProviderConfig(server.URL))

	_, err := provider.Recognize(context.Background(), []byte("ogg"))
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected 401 error, got %v", err)
	}
}

func TestSynthesize(t *testing.T) {
	t.Setenv("YANDEX_API_KEY", "")
	t.Setenv("YANDEX_FOLDER_ID", "")

	var gotForm map[string]string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}

		gotForm = map[string]string{
			"text":     r.PostForm.Get("text"),
			"lang":     r.PostForm.Get("lang"),
			"voice":    r.PostForm.Get("voice"),
			"format":   r.PostForm.Get("format"),
			"folderId": r.PostForm.Get("folderId"),
			"speed":    r.PostForm.Get("speed"),
		}

		_, _ = w.Write([]byte("ogg-answer"))
	}))
	defer server.Close()

	provider := newYandexProvider(testProviderConfig(server.URL))
	audio, err := provider.Synthesize(context.Background(), "ответ")
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}

	if string(audio) != "ogg-answer" {
		t.Fatalf("unexpected audio: %q", audio)
	}
	if gotForm["text"] != "ответ" || gotForm["lang"] != "ru-RU" ||
		gotForm["voice"] != "alena" || gotForm["format"] != "oggopus" ||
		gotForm["folderId"] != "folder" {
		t.Fatalf("unexpected form: %+v", gotForm)
	}
}

func TestSynthesizeTooLarge(t *testing.T) {
	t.Setenv("YANDEX_API_KEY", "")
	t.Setenv("YANDEX_FOLDER_ID", "")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("12345"))
	}))
	defer server.Close()

	cfg := testProviderConfig(server.URL)
	cfg.MaxAnswerBytes = 4

	provider := newYandexProvider(cfg)

	if _, err := provider.Synthesize(context.Background(), "text"); err != ErrTooLarge {
		t.Fatalf("expected ErrTooLarge, got %v", err)
	}
}

func TestComplete(t *testing.T) {
	t.Setenv("YANDEX_API_KEY", "")
	t.Setenv("YANDEX_FOLDER_ID", "")

	var got gptRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gotAuth := r.Header.Get("Authorization"); gotAuth != "Api-Key key" {
			t.Errorf("unexpected auth: %q", gotAuth)
		}

		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}

		_, _ = io.WriteString(w, `{"result":{"alternatives":[{"message":{"text":"ответ"}}]}}`)
	}))
	defer server.Close()

	provider := newYandexProvider(testProviderConfig(server.URL))
	messages := []Message{
		{Role: "system", Text: "prompt"},
		{Role: "user", Text: "вопрос"},
	}

	answer, err := provider.Complete(context.Background(), messages)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}

	if answer != "ответ" {
		t.Fatalf("unexpected answer: %q", answer)
	}
	if got.ModelURI != "gpt://folder/yandexgpt/latest" {
		t.Fatalf("unexpected model URI: %q", got.ModelURI)
	}
	if got.CompletionOptions.MaxTokens != "1000" {
		t.Fatalf("unexpected max tokens: %q", got.CompletionOptions.MaxTokens)
	}
	if len(got.Messages) != 2 || got.Messages[1].Text != "вопрос" {
		t.Fatalf("unexpected messages: %+v", got.Messages)
	}
}

func TestCompleteErrorStatus(t *testing.T) {
	t.Setenv("YANDEX_API_KEY", "")
	t.Setenv("YANDEX_FOLDER_ID", "")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer server.Close()

	provider := newYandexProvider(testProviderConfig(server.URL))

	if _, err := provider.Complete(context.Background(), []Message{{Role: "user", Text: "x"}}); err == nil ||
		!strings.Contains(err.Error(), "403") {
		t.Fatalf("expected 403 error, got %v", err)
	}
}
