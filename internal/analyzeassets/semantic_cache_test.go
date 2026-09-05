package analyzeassets

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

type countingSemanticAnalyzer struct{ calls atomic.Int32 }

func (a *countingSemanticAnalyzer) Analyze(context.Context, []string) (SemanticAnalysis, error) {
	a.calls.Add(1)
	return SemanticAnalysis{Literal: LiteralContent{Summary: "cached"}}, nil
}

func TestCachedSemanticAnalyzerReusesAnalysisForSameFrameAndScope(t *testing.T) {
	root := t.TempDir()
	frame := filepath.Join(root, "frame.jpg")
	if err := os.WriteFile(frame, []byte("frame"), 0o644); err != nil {
		t.Fatal(err)
	}
	cache, err := LoadSemanticCache(filepath.Join(root, "semantic-cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	analyzer := &countingSemanticAnalyzer{}
	cached := CachedSemanticAnalyzer{Analyzer: analyzer, Cache: cache, Scope: "openai/gpt-test/prompt-v2"}
	for range 2 {
		if _, err := cached.Analyze(context.Background(), []string{frame}); err != nil {
			t.Fatal(err)
		}
	}
	if analyzer.calls.Load() != 1 {
		t.Fatalf("provider calls = %d, want 1", analyzer.calls.Load())
	}

	reloaded, err := LoadSemanticCache(filepath.Join(root, "semantic-cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (CachedSemanticAnalyzer{Analyzer: analyzer, Cache: reloaded, Scope: "openai/gpt-test/prompt-v2"}).Analyze(context.Background(), []string{frame}); err != nil {
		t.Fatal(err)
	}
	if analyzer.calls.Load() != 1 {
		t.Fatalf("provider calls after reload = %d, want 1", analyzer.calls.Load())
	}
}
