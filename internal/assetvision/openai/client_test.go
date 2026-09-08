package openai

import (
	"context"
	"encoding/json"
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
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request["store"] != false {
			t.Fatalf("store = %#v, want false", request["store"])
		}
		text := request["text"].(map[string]any)
		format := text["format"].(map[string]any)
		if text["verbosity"] != "medium" || format["type"] != "json_schema" || format["strict"] != true {
			t.Fatalf("unexpected output format: %#v", text)
		}
		input := request["input"].([]any)[0].(map[string]any)
		content := input["content"].([]any)
		image := content[1].(map[string]any)
		if image["detail"] != "auto" {
			t.Fatalf("image detail = %#v, want auto", image["detail"])
		}
		_, _ = w.Write([]byte(`{"output_text":"{\"literal\":{\"summary\":\"a logo\",\"subjects\":\"logo\"},\"editorial_meaning\":{\"summary\":\"a branding cutaway\",\"intent\":\"brand recognition\",\"suggested_uses\":\"intro\"}}"}`))
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "frame.jpg")
	if err := os.WriteFile(path, []byte("image"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := (Client{APIKey: "test-key", Model: "test-model", ImageDetail: "auto", BaseURL: server.URL, HTTP: server.Client()}).Analyze(context.Background(), []string{path})
	if err != nil {
		t.Fatal(err)
	}
	if result.Literal.Summary != "a logo" || len(result.Literal.Subjects) != 1 || result.Editorial.Intent != "brand recognition" {
		t.Fatalf("unexpected result: %#v", result)
	}
}
