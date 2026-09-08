package plan

import (
	"auto-editor/internal/alignment"
	"auto-editor/internal/analyzeassets"
	"auto-editor/internal/analyzestyle"
	"auto-editor/internal/asr"
	"auto-editor/internal/audio"
	"context"
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
	if result.Version != 4 || result.Selection.StyleStatus != "unavailable" || len(result.Selection.Ranking) != 1 {
		t.Fatalf("unexpected selection audit: %#v", result.Selection)
	}
	if result.Visuals[0].SourceFill != "hold_last_frame" {
		t.Fatalf("unexpected render fill policy: %#v", result.Visuals[0])
	}
}

func TestBuildSoftRanksAssetsUsingSemanticStylePreferences(t *testing.T) {
	root := t.TempDir()
	alignmentPath := filepath.Join(root, "alignment.json")
	assetsPath := filepath.Join(root, "assets.json")
	stylePath := filepath.Join(root, "style.json")
	writeJSON(t, alignmentPath, alignment.Result{ProjectName: "test", Voiceover: alignment.Voiceover{Audio: audio.Probe{DurationMS: 2000}}, Segments: []alignment.Segment{{Text: "A beat", StartMS: 0, EndMS: 2000}}})
	writeJSON(t, assetsPath, analyzeassets.Result{Assets: []analyzeassets.Asset{
		{AssetID: "asset_neutral", Kind: "image", Editorial: analyzeassets.EditorialMeaning{Status: "complete", Intent: "establishing shot"}},
		{AssetID: "asset_reaction", Kind: "video", Technical: analyzeassets.Technical{DurationMS: 750}, Editorial: analyzeassets.EditorialMeaning{Status: "complete", Intent: "reaction_cutaway"}},
	}})
	writeJSON(t, stylePath, analyzestyle.Result{Version: 2, ReferenceSets: []analyzestyle.ReferenceSet{{Name: "shorts", Semantic: analyzestyle.SemanticStyle{Status: "complete", Confidence: "high", PlanningPreferences: []string{"Favor reaction cutaways"}}}}})

	result, err := Build(nil, Options{AlignmentPath: alignmentPath, AssetsPath: assetsPath, StylePath: stylePath, AspectRatio: "16:9"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Visuals[0].AssetID != "asset_reaction" {
		t.Fatalf("first visual = %q, want style-ranked reaction asset", result.Visuals[0].AssetID)
	}
	if result.Visuals[0].StartMS != 0 || result.Visuals[0].EndMS != 2000 || result.Visuals[0].SourceEndMS != 750 {
		t.Fatalf("style ranking changed hard timing constraints: %#v", result.Visuals[0])
	}
	if result.Selection.StyleStatus != "applied" || len(result.Selection.Preferences) != 1 {
		t.Fatalf("unexpected selection status: %#v", result.Selection)
	}
	winner := result.Selection.Ranking[0]
	if winner.AssetID != "asset_reaction" || winner.BaseOrder != 1 || winner.StyleScore != 3 || len(winner.Matches) != 1 {
		t.Fatalf("unexpected winning audit: %#v", winner)
	}
	if got := winner.Matches[0].MatchedTerms; len(got) != 2 || got[0] != "cutaway" || got[1] != "reaction" {
		t.Fatalf("matched terms = %#v", got)
	}
}

func TestBuildPreservesBaseOrderWhenStylePreferencesDoNotMatch(t *testing.T) {
	root := t.TempDir()
	alignmentPath := filepath.Join(root, "alignment.json")
	assetsPath := filepath.Join(root, "assets.json")
	stylePath := filepath.Join(root, "style.json")
	writeJSON(t, alignmentPath, alignment.Result{Segments: []alignment.Segment{{Text: "A beat", StartMS: 0, EndMS: 1000}}})
	writeJSON(t, assetsPath, analyzeassets.Result{Assets: []analyzeassets.Asset{{AssetID: "first", Kind: "image"}, {AssetID: "second", Kind: "image"}}})
	writeJSON(t, stylePath, analyzestyle.Result{ReferenceSets: []analyzestyle.ReferenceSet{{Name: "shorts", Semantic: analyzestyle.SemanticStyle{Status: "complete", Confidence: "medium", PlanningPreferences: []string{"Prefer energetic dance sequences"}}}}})

	result, err := Build(nil, Options{AlignmentPath: alignmentPath, AssetsPath: assetsPath, StylePath: stylePath}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Visuals[0].AssetID != "first" || result.Selection.StyleStatus != "no_matches" {
		t.Fatalf("unmatched preference changed base order: %#v", result)
	}
	if result.Selection.Ranking[0].StyleScore != 0 || result.Selection.Ranking[1].StyleScore != 0 {
		t.Fatalf("unmatched preference added a score: %#v", result.Selection.Ranking)
	}
}

func TestBuildUsesValidatedSemanticBeats(t *testing.T) {
	root := t.TempDir()
	alignmentPath, assetsPath := filepath.Join(root, "alignment.json"), filepath.Join(root, "assets.json")
	writeJSON(t, alignmentPath, alignment.Result{Voiceover: alignment.Voiceover{Audio: audio.Probe{DurationMS: 3000}}, Transcript: asr.Transcript{Words: []asr.Word{{Text: "First", StartMS: 0, EndMS: 500}, {Text: "scene", StartMS: 500, EndMS: 1000}, {Text: "Second", StartMS: 1000, EndMS: 1500}, {Text: "scene", StartMS: 1500, EndMS: 2000}, {Text: "Final", StartMS: 2000, EndMS: 2500}, {Text: "scene", StartMS: 2500, EndMS: 3000}}}})
	writeJSON(t, assetsPath, analyzeassets.Result{Assets: []analyzeassets.Asset{{AssetID: "first", Kind: "image", SHA256: "a"}, {AssetID: "second", Kind: "image", SHA256: "b"}, {AssetID: "final", Kind: "image", SHA256: "c"}}})
	result, err := BuildWithDependencies(context.Background(), Options{AlignmentPath: alignmentPath, AssetsPath: assetsPath, StylePath: filepath.Join(root, "none.json"), AspectRatio: "16:9", SemanticEnabled: true}, Dependencies{SemanticPlanner: fakeSemanticPlanner{}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Semantic.Status != "complete" || len(result.Visuals) != 3 || result.Visuals[0].AssetID != "first" || result.Visuals[1].AssetID != "second" || result.Visuals[2].AssetID != "final" {
		t.Fatalf("unexpected semantic plan: %#v", result)
	}
}

type fakeSemanticPlanner struct{}

func (fakeSemanticPlanner) Plan(context.Context, SemanticRequest) ([]SemanticBeat, error) {
	return []SemanticBeat{{WordFrom: 0, WordTo: 1, AssetID: "first", Role: "opening", Rationale: "first scene"}, {WordFrom: 2, WordTo: 3, AssetID: "second", Role: "change", Rationale: "second scene"}, {WordFrom: 4, WordTo: 5, AssetID: "final", Role: "ending", Rationale: "final scene"}}, nil
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
