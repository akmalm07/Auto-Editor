package alignment

import (
	"auto-editor/internal/asr"
	"auto-editor/internal/audio"
	"auto-editor/internal/ingest"
	"auto-editor/internal/project"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeTranscriber struct{ transcript asr.Transcript }

func (f fakeTranscriber) Transcribe(context.Context, string, string) (asr.Transcript, error) {
	return f.transcript, nil
}

func TestBuildCreatesTimedSegmentsAndWarnings(t *testing.T) {
	root := t.TempDir()
	write(t, root, "script.txt", "Hello, world. This is launch day.")
	write(t, root, "voice.wav", "fake audio")
	write(t, root, "assets/logo.png", "fake image")
	indexPath := buildIndex(t, root)

	result, err := Build(context.Background(), Options{IndexPath: indexPath, MinSimilarity: 0.95}, Dependencies{
		Transcriber: fakeTranscriber{transcript: asr.Transcript{
			Provider: "test", Model: "test", Text: "Hello world this is launch day now",
			Words: []asr.Word{
				{Text: "Hello", StartMS: 0, EndMS: 200}, {Text: "world", StartMS: 200, EndMS: 400},
				{Text: "this", StartMS: 500, EndMS: 650}, {Text: "is", StartMS: 650, EndMS: 750},
				{Text: "launch", StartMS: 800, EndMS: 1000}, {Text: "day", StartMS: 1000, EndMS: 1200}, {Text: "now", StartMS: 1200, EndMS: 1400},
			},
		}},
		Probe: func(string, string) (audio.Probe, error) {
			return audio.Probe{DurationMS: 1400, Codec: "pcm", SampleRate: 48000, Channels: 1}, nil
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Metrics.MatchedWords != 6 || result.Metrics.ExtraWords != 1 {
		t.Fatalf("unexpected metrics: %#v", result.Metrics)
	}
	if len(result.Segments) != 1 || result.Segments[0].Text != "Hello world This is launch day" {
		t.Fatalf("unexpected segments: %#v", result.Segments)
	}
	if len(result.Warnings) != 2 || !strings.Contains(result.Warnings[1].Message, "extra") {
		t.Fatalf("expected mismatch warning, got %#v", result.Warnings)
	}
}

func TestBuildRejectsInputsChangedAfterIngest(t *testing.T) {
	root := t.TempDir()
	write(t, root, "script.txt", "Original script")
	write(t, root, "voice.wav", "fake audio")
	write(t, root, "assets/logo.png", "fake image")
	indexPath := buildIndex(t, root)
	write(t, root, "script.txt", "Changed script")

	_, err := Build(context.Background(), Options{IndexPath: indexPath}, Dependencies{Transcriber: fakeTranscriber{}}, nil)
	if err == nil || !strings.Contains(err.Error(), "run ingest again") {
		t.Fatalf("expected stale-index error, got %v", err)
	}
}

func TestTranscriptTokensRejectsInvalidTimestamps(t *testing.T) {
	_, err := transcriptTokens(asr.Transcript{Text: "one two", Words: []asr.Word{{Text: "one", StartMS: 100, EndMS: 200}, {Text: "two", StartMS: 150, EndMS: 300}}})
	if err == nil {
		t.Fatal("expected timestamp validation error")
	}
}

func buildIndex(t *testing.T, root string) string {
	t.Helper()
	manifest := project.NewManifest("alignment-test")
	manifest.Inputs.Script = "script.txt"
	manifest.Inputs.Voiceover = "voice.wav"
	manifest.Inputs.Assets = []string{"assets"}
	manifestPath := filepath.Join(root, project.DefaultManifestName)
	if err := project.WriteManifest(manifestPath, manifest); err != nil {
		t.Fatal(err)
	}
	index, err := ingest.BuildIndex(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(root, ".auto-editor", "index.json")
	if err := ingest.WriteIndex(indexPath, index); err != nil {
		t.Fatal(err)
	}
	return indexPath
}

func write(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
