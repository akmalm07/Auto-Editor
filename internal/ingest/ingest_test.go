package ingest

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"auto-editor/internal/project"
)

func TestBuildIndexExpandsDirectoriesAndRecordsWarnings(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "script.txt", "This is the script.")
	writeFile(t, root, "voice.wav", "fake audio")
	writeFile(t, root, "assets/logo.png", "fake image")
	writeFile(t, root, "assets/notes.bin", "unsupported")
	writeFile(t, root, "refs/shorts/example.mp4", "fake video")

	manifestPath := filepath.Join(root, project.DefaultManifestName)
	manifest := project.NewManifest("test-project")
	manifest.Inputs.Script = "script.txt"
	manifest.Inputs.Voiceover = "voice.wav"
	manifest.Inputs.Assets = []string{"assets"}
	manifest.Inputs.ReferenceSets = []project.ReferenceSet{
		{Name: "shorts", Paths: []string{"refs/shorts"}},
	}
	if err := project.WriteManifest(manifestPath, manifest); err != nil {
		t.Fatal(err)
	}

	index, err := BuildIndex(manifestPath)
	if err != nil {
		t.Fatalf("BuildIndex returned error: %v", err)
	}

	if index.Script == nil || index.Script.Kind != KindText {
		t.Fatalf("expected text script, got %#v", index.Script)
	}
	if index.Voiceover == nil || index.Voiceover.Kind != KindAudio {
		t.Fatalf("expected audio voiceover, got %#v", index.Voiceover)
	}
	if len(index.Assets) != 1 {
		t.Fatalf("expected one indexed asset, got %d", len(index.Assets))
	}
	if index.Assets[0].Path != "assets/logo.png" {
		t.Fatalf("expected relative asset path, got %q", index.Assets[0].Path)
	}
	if len(index.ReferenceSets) != 1 || len(index.ReferenceSets[0].Files) != 1 {
		t.Fatalf("expected one reference file, got %#v", index.ReferenceSets)
	}
	if len(index.Warnings) != 1 {
		t.Fatalf("expected one warning for unsupported directory file, got %#v", index.Warnings)
	}
}

func TestBuildIndexReportsMultipleValidationErrors(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, project.DefaultManifestName)
	manifest := project.NewManifest("")
	manifest.Inputs.Voiceover = "missing.wav"
	if err := project.WriteManifest(manifestPath, manifest); err != nil {
		t.Fatal(err)
	}

	_, err := BuildIndex(manifestPath)
	if err == nil {
		t.Fatal("expected validation error")
	}

	var validationError ValidationError
	if !errors.As(err, &validationError) {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	if len(validationError.Diagnostics) != 4 {
		t.Fatalf("expected four diagnostics, got %#v", validationError.Diagnostics)
	}
}

func TestBuildIndexRejectsExplicitUnsupportedAsset(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "script.txt", "This is the script.")
	writeFile(t, root, "voice.wav", "fake audio")
	writeFile(t, root, "asset.bin", "unsupported")

	manifestPath := filepath.Join(root, project.DefaultManifestName)
	manifest := project.NewManifest("test-project")
	manifest.Inputs.Script = "script.txt"
	manifest.Inputs.Voiceover = "voice.wav"
	manifest.Inputs.Assets = []string{"asset.bin"}
	if err := project.WriteManifest(manifestPath, manifest); err != nil {
		t.Fatal(err)
	}

	_, err := BuildIndex(manifestPath)
	if err == nil {
		t.Fatal("expected validation error")
	}

	var validationError ValidationError
	if !errors.As(err, &validationError) {
		t.Fatalf("expected ValidationError, got %T", err)
	}

	found := false
	for _, diagnostic := range validationError.Diagnostics {
		if diagnostic.Field == "inputs.assets[0]" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected asset diagnostic, got %#v", validationError.Diagnostics)
	}
}

func writeFile(t *testing.T, root, path, content string) {
	t.Helper()

	fullPath := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
