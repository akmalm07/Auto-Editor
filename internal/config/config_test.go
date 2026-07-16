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
