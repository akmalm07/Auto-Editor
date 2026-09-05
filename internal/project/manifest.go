package project

import (
	"auto-editor/internal/artifact"
	"time"
)

const DefaultManifestName = "auto-editor.json"

type Manifest struct {
	Name        string       `json:"name"`
	AspectRatio string       `json:"aspect_ratio"`
	Language    string       `json:"language"`
	Runtime     string       `json:"runtime"`
	CreatedAt   time.Time    `json:"created_at"`
	Inputs      ProjectInput `json:"inputs"`
}

type ProjectInput struct {
	Script        string         `json:"script,omitempty"`
	Voiceover     string         `json:"voiceover,omitempty"`
	Assets        []string       `json:"assets,omitempty"`
	ReferenceSets []ReferenceSet `json:"reference_sets,omitempty"`
}

type ReferenceSet struct {
	Name  string   `json:"name"`
	Paths []string `json:"paths"`
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
	return artifact.Write(path, manifest)
}

func ReadManifest(path string) (Manifest, error) {
	return artifact.Read[Manifest](path)
}
