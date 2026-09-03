package analyzeassets

import (
	"auto-editor/internal/ingest"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeInspector struct{}

func (fakeInspector) Inspect(path, kind, executable string) (Technical, error) {
	return Technical{Width: 1920, Height: 1080, DurationMS: 5000, HasAudio: kind == ingest.KindVideo}, nil
}

type fakeSemantic struct{}

func (fakeSemantic) Analyze(context.Context, []string) (SemanticAnalysis, error) {
	return SemanticAnalysis{
		Literal:   LiteralContent{Summary: "A person runs outdoors.", Subjects: []string{"person"}, Actions: []string{"running"}},
		Editorial: EditorialMeaning{Summary: "An energetic b-roll clip.", Intent: "show forward motion", SuggestedUses: []string{"b-roll"}},
	}, nil
}

type fakeFrameExtractor struct{}

func (fakeFrameExtractor) Extract(videoPath, outputPath, executable string, timestampMS int64) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(outputPath, []byte("frame"), 0o644)
}

func TestBuildRunsDeterministicAndSemanticAnalysis(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "asset.jpg")
	if err := os.WriteFile(path, []byte("asset content"), 0o644); err != nil {
		t.Fatal(err)
	}
	indexPath := writeIndex(t, root, path)
	result, err := Build(context.Background(), Options{IndexPath: indexPath, SemanticEnabled: true}, Dependencies{Inspector: fakeInspector{}, SemanticAnalyzer: fakeSemantic{}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Assets) != 1 || result.Assets[0].Technical.Width != 1920 {
		t.Fatalf("deterministic analysis missing: %#v", result)
	}
	if result.Assets[0].Literal.Status != "complete" || result.Assets[0].Literal.Summary == "" || result.Assets[0].Editorial.Status != "complete" || result.Assets[0].Editorial.Intent == "" {
		t.Fatalf("semantic analysis missing: %#v", result.Assets[0])
	}
}

func TestBuildWarnsWhenSemanticAnalysisIsNotConfigured(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "asset.jpg")
	if err := os.WriteFile(path, []byte("asset content"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Build(context.Background(), Options{IndexPath: writeIndex(t, root, path)}, Dependencies{Inspector: fakeInspector{}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Assets[0].Literal.Status != "skipped" || result.Assets[0].Editorial.Status != "skipped" || len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0].Message, "not configured") {
		t.Fatalf("expected semantic warning, got %#v", result)
	}
}

func TestBuildExtractsFrameBeforeSemanticVideoAnalysis(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "asset.mp4")
	if err := os.WriteFile(path, []byte("asset content"), 0o644); err != nil {
		t.Fatal(err)
	}
	indexPath := writeTypedIndex(t, root, path, ingest.KindVideo)
	result, err := Build(context.Background(), Options{IndexPath: indexPath, SemanticEnabled: true}, Dependencies{Inspector: fakeInspector{}, SemanticAnalyzer: fakeSemantic{}, FrameExtractor: fakeFrameExtractor{}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	frames := result.Assets[0].Frames
	if len(frames) != 3 || frames[0].Position != "start" || frames[0].TimestampMS != 500 || frames[1].Position != "middle" || frames[1].TimestampMS != 2500 || frames[2].Position != "end" || frames[2].TimestampMS != 4500 || result.Assets[0].Editorial.Status != "complete" {
		t.Fatalf("expected generated frame and semantic result, got %#v", result.Assets[0])
	}
}

func TestBuildRejectsChangedAsset(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "asset.jpg")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	indexPath := writeIndex(t, root, path)
	if err := os.WriteFile(path, []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Build(context.Background(), Options{IndexPath: indexPath}, Dependencies{Inspector: fakeInspector{}}, nil)
	if err == nil || !strings.Contains(err.Error(), "run ingest again") {
		t.Fatalf("expected stale-input error, got %v", err)
	}
}

func writeIndex(t *testing.T, root, path string) string {
	t.Helper()
	hash, err := ingest.FileSHA256(path)
	if err != nil {
		t.Fatal(err)
	}
	index := ingest.Index{Version: ingest.IndexVersion, ProjectName: "test", ProjectRoot: root, Assets: []ingest.FileRecord{{ID: "asset_test", Path: "asset.jpg", Kind: ingest.KindImage, SHA256: hash}}}
	indexPath := filepath.Join(root, ".auto-editor", "index.json")
	if err := ingest.WriteIndex(indexPath, index); err != nil {
		t.Fatal(err)
	}
	return indexPath
}

func writeTypedIndex(t *testing.T, root, path, kind string) string {
	t.Helper()
	hash, err := ingest.FileSHA256(path)
	if err != nil {
		t.Fatal(err)
	}
	index := ingest.Index{Version: ingest.IndexVersion, ProjectName: "test", ProjectRoot: root, Assets: []ingest.FileRecord{{ID: "asset_test", Path: filepath.Base(path), Kind: kind, SHA256: hash}}}
	indexPath := filepath.Join(root, ".auto-editor", "index.json")
	if err := ingest.WriteIndex(indexPath, index); err != nil {
		t.Fatal(err)
	}
	return indexPath
}
