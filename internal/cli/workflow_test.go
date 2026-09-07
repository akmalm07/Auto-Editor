package cli

import (
	"auto-editor/internal/app"
	"auto-editor/internal/ingest"
	"auto-editor/internal/project"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindProjectRootWalksAncestors(t *testing.T) {
	root := t.TempDir()
	if err := project.WriteManifest(filepath.Join(root, project.DefaultManifestName), project.NewManifest("test")); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "assets", "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	got, found, err := findProjectRoot(nested)
	if err != nil {
		t.Fatal(err)
	}
	if !found || got != root {
		t.Fatalf("findProjectRoot() = %q, %t; want %q, true", got, found, root)
	}
}

func TestRunStatusUsesProjectOverrideAndRestoresWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	if err := project.WriteManifest(filepath.Join(root, project.DefaultManifestName), project.NewManifest("test")); err != nil {
		t.Fatal(err)
	}
	before, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if err := Run([]string{"--project", root, "status"}, app.New("auto-editor", "test", &out, &stderr)); err != nil {
		t.Fatal(err)
	}
	after, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("working directory changed from %q to %q", before, after)
	}
	if !strings.Contains(out.String(), "✓ manifest: auto-editor.json") || !strings.Contains(out.String(), "next: auto-editor ingest") {
		t.Fatalf("unexpected status output: %s", out.String())
	}
}

func TestParsePipelineStage(t *testing.T) {
	stage, err := parsePipelineStage("analyze-assets")
	if err != nil || stage != stageAnalyzeAssets {
		t.Fatalf("parsePipelineStage() = %v, %v", stage, err)
	}
	if _, err := parsePipelineStage("render"); err == nil {
		t.Fatal("expected invalid stage error")
	}
}

func TestResolveAlignmentOutputPathDoesNotNestArtifactDirectory(t *testing.T) {
	root := t.TempDir()
	indexPath := filepath.Join(root, ".auto-editor", "index.json")
	if err := ingest.WriteIndex(indexPath, ingest.Index{Version: ingest.IndexVersion, ProjectRoot: root}); err != nil {
		t.Fatal(err)
	}
	got, err := resolveAlignmentOutputPath(indexPath, ".auto-editor/plan.json")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, ".auto-editor", "plan.json")
	if got != want {
		t.Fatalf("resolved output = %q, want %q", got, want)
	}
}
