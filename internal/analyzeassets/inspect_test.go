package analyzeassets

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestSystemInspectorReadsImageDimensions(t *testing.T) {
	data, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl8u0kAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "pixel.png")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	technical, err := (SystemInspector{}).Inspect(path, "image", "ffprobe")
	if err != nil {
		t.Fatal(err)
	}
	if technical.Width != 1 || technical.Height != 1 {
		t.Fatalf("unexpected dimensions: %#v", technical)
	}
}
