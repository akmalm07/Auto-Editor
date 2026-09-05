package artifact

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAndRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "artifact.json")
	want := struct {
		Name string `json:"name"`
	}{Name: "launch"}

	if err := Write(path, want); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "{\n  \"name\": \"launch\"\n}\n" {
		t.Fatalf("written JSON = %q", data)
	}
	got, err := Read[struct {
		Name string `json:"name"`
	}](path)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if got != want {
		t.Fatalf("Read() = %#v, want %#v", got, want)
	}
}

func TestPaths(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "assets", "image.png")
	if got := ResolvePath(root, "assets/image.png"); got != path {
		t.Fatalf("ResolvePath() = %q, want %q", got, path)
	}
	if got := DisplayPath(root, path); got != "assets/image.png" {
		t.Fatalf("DisplayPath() = %q, want relative path", got)
	}
}
