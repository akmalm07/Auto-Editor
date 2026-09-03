package plan

import (
	"auto-editor/internal/alignment"
	"auto-editor/internal/analyzeassets"
	"auto-editor/internal/audio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildCreatesDeterministicTimeline(t *testing.T) {
	root := t.TempDir()
	alignmentPath := filepath.Join(root, "alignment.json")
	assetsPath := filepath.Join(root, "assets.json")
	alignmentValue := alignment.Result{Version: 1, ProjectName: "test", Voiceover: alignment.Voiceover{ID: "voiceover_1", Path: "voice.mp3", Audio: audio.Probe{DurationMS: 4000}}, Segments: []alignment.Segment{{Index: 0, Text: "Hello world", ScriptWordFrom: 0, ScriptWordTo: 1, StartMS: 0, EndMS: 2000}, {Index: 1, Text: "Again", ScriptWordFrom: 2, ScriptWordTo: 2, StartMS: 2000, EndMS: 4000}}}
	assetsValue := analyzeassets.Result{Version: 2, ProjectName: "test", Assets: []analyzeassets.Asset{{AssetID: "asset_1", Kind: "image", Editorial: analyzeassets.EditorialMeaning{Intent: "reaction_cutaway"}}}}
	writeJSON(t, alignmentPath, alignmentValue)
	writeJSON(t, assetsPath, assetsValue)
	result, err := Build(nil, Options{AlignmentPath: alignmentPath, AssetsPath: assetsPath, StylePath: filepath.Join(root, "missing-style.json"), AspectRatio: "16:9"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Visuals) != 2 || result.Visuals[0].AssetID != "asset_1" || len(result.Captions) != 2 || len(result.Warnings) != 1 {
		t.Fatalf("unexpected plan: %#v", result)
	}
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
