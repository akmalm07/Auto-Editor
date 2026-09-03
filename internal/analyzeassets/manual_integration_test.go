package analyzeassets_test

import (
	"auto-editor/internal/analyzeassets"
	assetopenai "auto-editor/internal/assetvision/openai"
	"auto-editor/internal/ingest"
	"auto-editor/internal/project"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// This is an opt-in, billable integration test. It is skipped unless all
// AUTO_EDITOR_SEMANTIC_TEST_* environment variables are supplied.
func TestPipelineWithExternalSemanticProvider(t *testing.T) {
	key := os.Getenv("AUTO_EDITOR_SEMANTIC_TEST_KEY")
	root := os.Getenv("AUTO_EDITOR_SEMANTIC_TEST_ROOT")
	imagePath := os.Getenv("AUTO_EDITOR_SEMANTIC_TEST_IMAGE")
	videoPath := os.Getenv("AUTO_EDITOR_SEMANTIC_TEST_VIDEO")
	scriptPath := os.Getenv("AUTO_EDITOR_SEMANTIC_TEST_SCRIPT")
	voiceoverPath := os.Getenv("AUTO_EDITOR_SEMANTIC_TEST_VOICEOVER")
	if key == "" || root == "" || imagePath == "" || videoPath == "" || scriptPath == "" || voiceoverPath == "" {
		t.Skip("set AUTO_EDITOR_SEMANTIC_TEST_* variables to run the external semantic integration test")
	}
	manifest := project.NewManifest("semantic-pipeline-test")
	manifest.Inputs.Script = scriptPath
	manifest.Inputs.Voiceover = voiceoverPath
	manifest.Inputs.Assets = []string{imagePath, videoPath}
	manifestPath := filepath.Join(root, project.DefaultManifestName)
	if err := project.WriteManifest(manifestPath, manifest); err != nil {
		t.Fatal(err)
	}
	index, err := ingest.BuildIndex(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(root, ingest.DefaultIndexPath)
	if err := ingest.WriteIndex(indexPath, index); err != nil {
		t.Fatal(err)
	}
	result, err := analyzeassets.Build(context.Background(), analyzeassets.Options{IndexPath: indexPath, SemanticEnabled: true}, analyzeassets.Dependencies{
		SemanticAnalyzer: assetopenai.Client{APIKey: key, Model: "gpt-4.1-mini"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := analyzeassets.Write(filepath.Join(root, analyzeassets.DefaultAssetsPath), result); err != nil {
		t.Fatal(err)
	}
	if len(result.Assets) != 2 {
		t.Fatalf("expected two assets, got %d", len(result.Assets))
	}
	for _, asset := range result.Assets {
		if asset.Literal.Status != "complete" || asset.Editorial.Status != "complete" {
			t.Fatalf("semantic analysis did not complete for %s: %#v", asset.Path, asset)
		}
	}
}
