package openai

import (
	"auto-editor/internal/analyzestyle"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestStyleClientAnalyzesReferenceSet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		format := request["text"].(map[string]any)["format"].(map[string]any)
		if format["name"] != "reference_style_analysis" || format["type"] != "json_schema" {
			t.Fatalf("unexpected schema format: %#v", format)
		}
		_, _ = w.Write([]byte(`{"output_text":"{\"summary\":\"quick reaction edits\",\"caption_conventions\":[\"centered text\"],\"humor_patterns\":[],\"emotional_pacing\":[],\"visual_roles\":[],\"editing_techniques\":[],\"planning_preferences\":[\"short cutaways\"],\"supporting_frame_ids\":[\"reference_0/start\"],\"confidence\":\"medium\"}"}`))
	}))
	defer server.Close()
	frame := filepath.Join(t.TempDir(), "frame.jpg")
	if err := os.WriteFile(frame, []byte("image"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := (StyleClient{Client: Client{APIKey: "test-key", Model: "test-model", BaseURL: server.URL, HTTP: server.Client()}}).AnalyzeStyle(context.Background(), analyzestyle.SemanticRequest{ReferenceSet: "shorts", Frames: []analyzestyle.SemanticFrame{{ID: "reference_0/start"}}, FramePaths: []string{frame}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary != "quick reaction edits" || result.Confidence != "medium" || len(result.PlanningPreferences) != 1 {
		t.Fatalf("unexpected style result: %#v", result)
	}
}
