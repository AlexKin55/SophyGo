package ai

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestYandexIntegration performs real requests to Yandex AI Studio. It is
// skipped unless YANDEX_INTEGRATION=1 and the credentials are provided through
// the environment. Secrets are never written to the repository.
func TestYandexIntegration(t *testing.T) {
	if os.Getenv("YANDEX_INTEGRATION") != "1" {
		t.Skip("set YANDEX_INTEGRATION=1 to run live Yandex tests")
	}

	key := strings.TrimSpace(os.Getenv("YANDEX_API_KEY"))
	folder := strings.TrimSpace(os.Getenv("YANDEX_FOLDER_ID"))
	if key == "" || folder == "" {
		t.Skip("YANDEX_API_KEY and YANDEX_FOLDER_ID are required")
	}

	cfg := Config{Enabled: true, APIKey: key, FolderID: folder}
	cfg.ApplyDefaults()

	provider := newYandexProvider(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	t.Run("gpt", func(t *testing.T) {
		answer, err := provider.Complete(ctx, []Message{
			{Role: "system", Text: "Отвечай кратко и по фактам."},
			{Role: "user", Text: "Назови столицу Франции одним словом."},
		})
		if err != nil {
			t.Fatalf("complete: %v", err)
		}

		if !strings.Contains(strings.ToLower(answer), "париж") {
			t.Fatalf("unexpected answer: %q", answer)
		}
	})

	t.Run("tts_stt_roundtrip", func(t *testing.T) {
		audio, err := provider.Synthesize(ctx, "Привет! Это проверка голосового гида.")
		if err != nil {
			t.Fatalf("synthesize: %v", err)
		}

		if len(audio) == 0 {
			t.Fatal("expected non-empty OGG audio")
		}
		if !strings.HasPrefix(string(audio), "OggS") {
			t.Fatalf("expected OGG container, got %q", audio[:min(8, len(audio))])
		}

		transcript, err := provider.Recognize(ctx, audio)
		if err != nil {
			t.Fatalf("recognize: %v", err)
		}

		if !strings.Contains(strings.ToLower(transcript), "привет") {
			t.Fatalf("unexpected transcript: %q", transcript)
		}
	})
}

func min(a, b int) int {
	if a < b {
		return a
	}

	return b
}
