package project

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const DefaultManifestName = "auto-editor.json"

type Manifest struct {
	Name        string    `json:"name"`
	AspectRatio string    `json:"aspect_ratio"`
	Language    string    `json:"language"`
	Runtime     string    `json:"runtime"`
	CreatedAt   time.Time `json:"created_at"`
	SourceVideo string    `json:"source_video,omitempty"`
}

func NewManifest(name string) Manifest {
	return Manifest{
		Name:        name,
		AspectRatio: "16:9",
		Language:    "en",
		Runtime:     "60s",
		CreatedAt:   time.Now().UTC(),
	}
}

func WriteManifest(path string, manifest Manifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func ReadManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}

	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, err
	}

	return manifest, nil
}
