package analyzestyle

import (
	"auto-editor/internal/analyzeassets"
	"auto-editor/internal/ingest"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeInspector struct {
	values map[string]analyzeassets.Technical
}

type fakeSceneDetector struct{ values map[string][]int64 }

type fakeStyleFrameExtractor struct{}

func (fakeStyleFrameExtractor) Extract(videoPath, outputPath, executable string, timestampMS int64) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(outputPath, []byte("frame"), 0o644)
}

type fakeStyleSemantic struct{ request SemanticRequest }

func (f *fakeStyleSemantic) AnalyzeStyle(ctx context.Context, request SemanticRequest) (SemanticStyle, error) {
	f.request = request
	return SemanticStyle{Summary: "fast meme-driven edits", Confidence: "high", SupportingFrameIDs: []string{request.Frames[0].ID, "unknown/frame"}}, nil
}

type countingStyleSemantic struct{ calls int }

func (f *countingStyleSemantic) AnalyzeStyle(ctx context.Context, request SemanticRequest) (SemanticStyle, error) {
	f.calls++
	return SemanticStyle{Summary: "cached"}, nil
}

func (f fakeSceneDetector) Detect(path, executable string) ([]int64, error) {
	return f.values[filepath.Base(path)], nil
}

func (f fakeInspector) Inspect(path, kind, executable string) (analyzeassets.Technical, error) {
	return f.values[filepath.Base(path)], nil
}

func TestBuildProfilesNamedReferenceSet(t *testing.T) {
	root := t.TempDir()
	first := writeReference(t, root, "first.mp4", "first")
	second := writeReference(t, root, "second.mp4", "second")
	indexPath := writeIndex(t, root, first, second)
	result, err := Build(context.Background(), Options{IndexPath: indexPath}, Dependencies{Inspector: fakeInspector{values: map[string]analyzeassets.Technical{"first.mp4": {Width: 1920, Height: 1080, DurationMS: 1000, HasAudio: true}, "second.mp4": {Width: 1080, Height: 1920, DurationMS: 3000}}}, SceneDetector: fakeSceneDetector{values: map[string][]int64{"first.mp4": {400}, "second.mp4": {1000, 2000}}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	profile := result.ReferenceSets[0].Profile
	if profile.VideosAnalyzed != 2 || profile.TotalDurationMS != 4000 || profile.MedianDurationMS != 3000 || profile.AudioVideoRatio != 0.5 || len(profile.AspectRatios) != 2 || profile.CutCount != 3 || profile.CutsPerMinute != 45 || profile.MedianShotDurationMS != 1000 {
		t.Fatalf("unexpected profile: %#v", profile)
	}
}

func TestBuildRejectsChangedReference(t *testing.T) {
	root := t.TempDir()
	first := writeReference(t, root, "first.mp4", "first")
	indexPath := writeIndex(t, root, first)
	if err := os.WriteFile(first, []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Build(context.Background(), Options{IndexPath: indexPath}, Dependencies{Inspector: fakeInspector{}}, nil)
	if err == nil || !strings.Contains(err.Error(), "run ingest again") {
		t.Fatalf("expected stale-index error, got %v", err)
	}
}

func TestBuildAddsBoundedSemanticStyleEvidence(t *testing.T) {
	root := t.TempDir()
	first := writeReference(t, root, "first.mp4", "first")
	second := writeReference(t, root, "second.mp4", "second")
	semantic := &fakeStyleSemantic{}
	result, err := Build(context.Background(), Options{IndexPath: writeIndex(t, root, first, second), SemanticEnabled: true, SemanticMaxFrames: 3, ThumbnailDir: filepath.Join(root, "frames")}, Dependencies{
		Inspector:        fakeInspector{values: map[string]analyzeassets.Technical{"first.mp4": {DurationMS: 1000}, "second.mp4": {DurationMS: 1000}}},
		SceneDetector:    fakeSceneDetector{},
		FrameExtractor:   fakeStyleFrameExtractor{},
		SemanticAnalyzer: semantic,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	style := result.ReferenceSets[0].Semantic
	if style.Status != "complete" || style.Summary == "" || len(style.SampledFrames) != 3 || len(style.SupportingFrameIDs) != 1 {
		t.Fatalf("unexpected semantic style: %#v", style)
	}
	if semantic.request.Frames[0].ID != "reference_0/start" || semantic.request.Frames[1].ID != "reference_1/start" {
		t.Fatalf("frames were not sampled round-robin: %#v", semantic.request.Frames)
	}
}

func TestCachedSemanticAnalyzerReusesStyleResult(t *testing.T) {
	root := t.TempDir()
	frame := filepath.Join(root, "frame.jpg")
	if err := os.WriteFile(frame, []byte("frame"), 0o644); err != nil {
		t.Fatal(err)
	}
	cache, err := LoadSemanticCache(filepath.Join(root, "style-semantic-cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	analyzer := &countingStyleSemantic{}
	cached := CachedSemanticAnalyzer{Analyzer: analyzer, Cache: cache, Scope: "openai/gpt-test/style-v1"}
	request := SemanticRequest{Technical: TechnicalStyle{CutsPerMinute: 30}, FramePaths: []string{frame}}
	for range 2 {
		if _, err := cached.AnalyzeStyle(context.Background(), request); err != nil {
			t.Fatal(err)
		}
	}
	if analyzer.calls != 1 {
		t.Fatalf("provider calls = %d, want 1", analyzer.calls)
	}
}

func writeReference(t *testing.T, root, name, content string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
func writeIndex(t *testing.T, root string, paths ...string) string {
	t.Helper()
	files := make([]ingest.FileRecord, 0, len(paths))
	for index, path := range paths {
		hash, err := ingest.FileSHA256(path)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, ingest.FileRecord{ID: fmt.Sprintf("reference_%d", index), Path: filepath.Base(path), Kind: ingest.KindVideo, SHA256: hash})
	}
	value := ingest.Index{Version: ingest.IndexVersion, ProjectName: "test", ProjectRoot: root, ReferenceSets: []ingest.ReferenceSetIndex{{Name: "shorts", Files: files}}}
	output := filepath.Join(root, ".auto-editor", "index.json")
	if err := ingest.WriteIndex(output, value); err != nil {
		t.Fatal(err)
	}
	return output
}
