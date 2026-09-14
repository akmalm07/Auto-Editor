package projectexport

import (
	"auto-editor/internal/alignment"
	"auto-editor/internal/analyzeassets"
	"auto-editor/internal/artifact"
	"auto-editor/internal/audio"
	"auto-editor/internal/ingest"
	"auto-editor/internal/plan"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestExportCopiesSourcesAndWritesFCPXML(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, ".auto-editor"), 0o755)
	voice := filepath.Join(root, "voice.wav")
	image := filepath.Join(root, "hero.jpg")
	cue := filepath.Join(root, "pop.wav")
	for path, data := range map[string]string{voice: "voice", image: "image", cue: "cue"} {
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	hashOf := func(path string) string {
		value, err := ingest.FileSHA256(path)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	voiceHash, imageHash, cueHash := hashOf(voice), hashOf(image), hashOf(cue)
	if err := artifact.Write(filepath.Join(root, ".auto-editor", "assets.json"), analyzeassets.Result{Assets: []analyzeassets.Asset{{AssetID: "image", Path: "hero.jpg", Kind: "image", SHA256: imageHash}, {AssetID: "cue", Path: "pop.wav", Kind: "audio", SHA256: cueHash}}}); err != nil {
		t.Fatal(err)
	}
	if err := artifact.Write(filepath.Join(root, ".auto-editor", "alignment.json"), alignment.Result{Voiceover: alignment.Voiceover{ID: "voice", Path: "voice.wav", SHA256: voiceHash, Audio: audio.Probe{DurationMS: 1000}}}); err != nil {
		t.Fatal(err)
	}
	p := plan.Result{Version: plan.Version, ProjectName: "demo", Format: plan.Format{DurationMS: 1000}, Sources: plan.Sources{Assets: ".auto-editor/assets.json"}, Voiceover: plan.Voiceover{AssetID: "voice", Path: "voice.wav", SHA256: voiceHash}, Visuals: []plan.Visual{{ID: "visual_1", AssetID: "image", StartMS: 0, EndMS: 1000, SourceEndMS: 1000}}, AudioCues: []plan.AudioCue{{AssetID: "cue", Path: "pop.wav", SHA256: cueHash, StartMS: 200, DurationMS: 300, VolumeDB: -3}}}
	planPath := filepath.Join(root, ".auto-editor", "plan.json")
	if err := artifact.Write(planPath, p); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "export")
	result, err := Export(Options{PlanPath: planPath, OutputDir: out, CopyAssets: true, Width: 1920, Height: 1080, FPS: 30})
	if err != nil {
		t.Fatal(err)
	}
	if result.Copied != 3 {
		t.Fatalf("copied = %d, want 3", result.Copied)
	}
	xml, err := os.ReadFile(result.XMLPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(xml)
	if !strings.Contains(text, "<fcpxml version=\"1.9\">") || !strings.Contains(text, "name=\"cue\" ref=") || !strings.Contains(text, "lane=\"3\"") || !strings.Contains(text, "name=\"voiceover\"") {
		t.Fatalf("unexpected FCPXML: %s", text)
	}
	if _, err := os.Stat(filepath.Join(out, "assets", "cue.wav")); err != nil {
		t.Fatal(err)
	}
	kdenliveOut := filepath.Join(root, "kdenlive")
	kdenliveResult, err := Export(Options{PlanPath: planPath, OutputDir: kdenliveOut, Format: "kdenlive", CopyAssets: false, Width: 1920, Height: 1080, FPS: 30})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(kdenliveResult.XMLPath) != "auto-editor.kdenlive" {
		t.Fatalf("Kdenlive output = %s", kdenliveResult.XMLPath)
	}
	kdenliveXML, err := os.ReadFile(kdenliveResult.XMLPath)
	if err != nil {
		t.Fatal(err)
	}
	kdenliveText := string(kdenliveXML)
	if !strings.Contains(kdenliveText, "producer=\"main_bin\"") || !strings.Contains(kdenliveText, "<playlist id=\"main_bin\"") || !strings.Contains(kdenliveText, "kdenlive:projectTractor") {
		t.Fatalf("unexpected Kdenlive project: %s", kdenliveText)
	}
	if !strings.Contains(kdenliveText, "<property name=\"kdenlive:duration\">00:00:01:00</property>") {
		t.Fatalf("Kdenlive duration must be timecode, got: %s", kdenliveText)
	}
	if !strings.Contains(kdenliveText, "<playlist id=\"playlist1\">\n</playlist>") || !strings.Contains(kdenliveText, "producer=\"playlist0\" hide=\"audio\"/><track producer=\"playlist1\" hide=\"audio\"") {
		t.Fatalf("Kdenlive video track is missing its required paired playlist: %s", kdenliveText)
	}
	if !strings.Contains(kdenliveText, "<property name=\"kdenlive:track_name\">Voiceover</property><property name=\"kdenlive:audio_track\">1</property>") || !strings.Contains(kdenliveText, "<property name=\"kdenlive:track_name\">Sound Effects</property><property name=\"kdenlive:audio_track\">1</property>") {
		t.Fatalf("Kdenlive audio tractors must be marked as audio tracks: %s", kdenliveText)
	}
	if strings.Contains(kdenliveText, "<property name=\"kdenlive:id\">visual_1</property>") || strings.Contains(kdenliveText, "<property name=\"kdenlive:id\">cue</property>") {
		t.Fatalf("timeline entries must use their numeric Kdenlive bin IDs: %s", kdenliveText)
	}
	if !strings.Contains(kdenliveText, "producer0\" in=\"0\" out=\"29\"><property name=\"length\">30</property>") || !strings.Contains(kdenliveText, "<property name=\"kdenlive:clip_type\">1</property>") || !strings.Contains(kdenliveText, "<property name=\"kdenlive:clip_type\">5</property>") {
		t.Fatalf("Kdenlive bin producers must preserve audio and image media types: %s", kdenliveText)
	}
	visual := strings.Index(kdenliveText, "<playlist id=\"playlist0\">")
	first := strings.Index(kdenliveText, "producer=\"producer1\"")
	if visual < 0 || first < visual || strings.Index(kdenliveText[first:], "producer=\"producer2\"") < 0 {
		t.Fatalf("visual playlist must keep the plan order: %s", kdenliveText)
	}
	uuid := regexp.MustCompile(`<property name="kdenlive:uuid">\{[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\}</property>`)
	if !uuid.MatchString(kdenliveText) {
		t.Fatalf("Kdenlive sequence UUID is invalid: %s", kdenliveText)
	}
}
