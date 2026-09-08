package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadMissingConfigReturnsDefault(t *testing.T) {
	cfg, err := Read(filepath.Join(t.TempDir(), DefaultConfigName))
	if err != nil {
		t.Fatal(err)
	}

	if cfg.UI.Color != "auto" {
		t.Fatalf("expected default color auto, got %q", cfg.UI.Color)
	}
	if !cfg.UI.Progress {
		t.Fatal("expected progress to default to true")
	}
	if !cfg.UI.Loading.Enabled {
		t.Fatal("expected loading to default to enabled")
	}
	if cfg.UI.Loading.Style != "ascii" {
		t.Fatalf("expected default loading style ascii, got %q", cfg.UI.Loading.Style)
	}
	if cfg.UI.Loading.IntervalMS != 80 {
		t.Fatalf("expected default loading interval 80, got %d", cfg.UI.Loading.IntervalMS)
	}
	if cfg.Alignment.Provider != "openai" || cfg.Alignment.Model != "whisper-1" || cfg.Alignment.MinSimilarity != 0.70 {
		t.Fatalf("unexpected alignment defaults: %#v", cfg.Alignment)
	}
	if cfg.Assets.FFprobe != "ffprobe" || cfg.Assets.FFmpeg != "ffmpeg" || cfg.Assets.SemanticProvider != "openai" || !cfg.Assets.SemanticCache || cfg.Assets.SemanticImageDetail != "auto" || cfg.Assets.SemanticMaxAttempts != 3 || cfg.Assets.SemanticTimeoutSeconds != 60 {
		t.Fatalf("unexpected asset defaults: %#v", cfg.Assets)
	}
	if cfg.Style.SemanticProvider != "openai" || !cfg.Style.SemanticCache || cfg.Style.SemanticImageDetail != "auto" || cfg.Style.SemanticMaxFrames != 12 {
		t.Fatalf("unexpected style defaults: %#v", cfg.Style)
	}
	if cfg.Planning.SemanticProvider != "openai" || cfg.Planning.SemanticModel != "gpt-4o-mini" || cfg.Planning.SemanticMaxAttempts != 3 {
		t.Fatalf("unexpected planning defaults: %#v", cfg.Planning)
	}
	if cfg.Render.FFmpeg != "ffmpeg" || cfg.Render.Width != 1920 || cfg.Render.Height != 1080 || cfg.Render.FPS != 30 || !cfg.Render.Captions.Enabled {
		t.Fatalf("unexpected render defaults: %#v", cfg.Render)
	}
}

func TestReadRejectsInvalidRenderSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), DefaultConfigName)
	if err := os.WriteFile(path, []byte(`{"render":{"width":0}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(path); err == nil {
		t.Fatal("expected invalid render configuration")
	}
}

func TestReadRejectsInvalidSemanticRequestSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), DefaultConfigName)
	if err := os.WriteFile(path, []byte(`{"assets":{"semantic_image_detail":"tiny","semantic_max_attempts":6}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(path); err == nil {
		t.Fatal("expected invalid semantic request settings")
	}
}

func TestReadAcceptsProviderSpecificImageDetail(t *testing.T) {
	path := filepath.Join(t.TempDir(), DefaultConfigName)
	if err := os.WriteFile(path, []byte(`{"assets":{"semantic_image_detail":"medium"},"style":{"semantic_image_detail":"original"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(path); err != nil {
		t.Fatalf("Read() error = %v", err)
	}
}

func TestReadRejectsUnsupportedAlignmentModel(t *testing.T) {
	path := filepath.Join(t.TempDir(), DefaultConfigName)
	if err := os.WriteFile(path, []byte(`{"alignment":{"model":"gpt-4o-transcribe"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(path); err == nil {
		t.Fatal("expected unsupported model error")
	}
}

func TestReadRejectsInvalidColorMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), DefaultConfigName)
	if err := os.WriteFile(path, []byte(`{"ui":{"color":"loud"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Read(path); err == nil {
		t.Fatal("expected invalid color mode error")
	}
}

func TestReadRejectsOpenAISemanticProviderWithoutModel(t *testing.T) {
	path := filepath.Join(t.TempDir(), DefaultConfigName)
	if err := os.WriteFile(path, []byte(`{"assets":{"semantic_provider":"openai","semantic_model":""}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(path); err == nil {
		t.Fatal("expected invalid semantic configuration")
	}
}
