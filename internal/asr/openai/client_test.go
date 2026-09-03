package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestClientTranscribeRequestsWordTimestamps(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("unexpected authorization header")
		}
		if err := r.ParseMultipartForm(1024 * 1024); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("model") != "whisper-1" || r.FormValue("response_format") != "verbose_json" {
			t.Fatalf("unexpected form: %#v", r.MultipartForm.Value)
		}
		if len(r.MultipartForm.Value["timestamp_granularities[]"]) != 2 {
			t.Fatalf("missing timestamp granularities: %#v", r.MultipartForm.Value)
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if _, err := io.ReadAll(file); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"language":"english","text":"Hello world","words":[{"word":"Hello","start":0,"end":0.5},{"word":"world","start":0.5,"end":1}],"segments":[{"text":"Hello world","start":0,"end":1}]}`))
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "voice.wav")
	if err := os.WriteFile(path, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	transcript, err := (Client{APIKey: "test-key", BaseURL: server.URL, HTTP: server.Client()}).Transcribe(context.Background(), path, "en")
	if err != nil {
		t.Fatal(err)
	}
	if len(transcript.Words) != 2 || transcript.Words[1].EndMS != 1000 {
		t.Fatalf("unexpected transcript: %#v", transcript)
	}
}
