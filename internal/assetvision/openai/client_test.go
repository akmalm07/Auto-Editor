package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestClientAnalyzesImageWithResponsesAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("unexpected request %s / %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"output_text":"{\"literal\":{\"summary\":\"a logo\",\"subjects\":\"logo\"},\"editorial_meaning\":{\"summary\":\"a branding cutaway\",\"intent\":\"brand recognition\",\"suggested_uses\":\"intro\"}}"}`))
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "frame.jpg")
	if err := os.WriteFile(path, []byte("image"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := (Client{APIKey: "test-key", Model: "test-model", BaseURL: server.URL, HTTP: server.Client()}).Analyze(context.Background(), []string{path})
	if err != nil {
		t.Fatal(err)
	}
	if result.Literal.Summary != "a logo" || len(result.Literal.Subjects) != 1 || result.Editorial.Intent != "brand recognition" {
		t.Fatalf("unexpected result: %#v", result)
	}
}
