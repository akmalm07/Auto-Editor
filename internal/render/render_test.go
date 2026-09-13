package render

import (
	"auto-editor/internal/alignment"
	"auto-editor/internal/analyzeassets"
	"auto-editor/internal/artifact"
	"auto-editor/internal/audio"
	"auto-editor/internal/config"
	"auto-editor/internal/ingest"
	"auto-editor/internal/plan"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildRendersVerifiedPlan(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".auto-editor"), 0o755); err != nil {
		t.Fatal(err)
	}
	voice := filepath.Join(root, "voice.wav")
	image := filepath.Join(root, "image.jpg")
	if err := os.WriteFile(voice, []byte("voice"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(image, []byte("image"), 0o644); err != nil {
		t.Fatal(err)
	}
	voiceHash, err := hash(voice)
	if err != nil {
		t.Fatal(err)
	}
	imageHash, err := hash(image)
	if err != nil {
		t.Fatal(err)
	}
	alignmentPath := filepath.Join(root, ".auto-editor", "alignment.json")
	assetsPath := filepath.Join(root, ".auto-editor", "assets.json")
	if err := artifact.Write(alignmentPath, alignment.Result{Voiceover: alignment.Voiceover{ID: "voice", Path: "voice.wav", SHA256: voiceHash, Audio: audio.Probe{DurationMS: 1000}}}); err != nil {
		t.Fatal(err)
	}
	if err := artifact.Write(assetsPath, analyzeassets.Result{Assets: []analyzeassets.Asset{{AssetID: "image", Path: "image.jpg", Kind: "image", SHA256: imageHash}}}); err != nil {
		t.Fatal(err)
	}
	p := plan.Result{Version: plan.Version, Format: plan.Format{AspectRatio: "16:9", DurationMS: 1000}, Sources: plan.Sources{Alignment: ".auto-editor/alignment.json", Assets: ".auto-editor/assets.json"}, Voiceover: plan.Voiceover{AssetID: "voice", Path: "voice.wav", SHA256: voiceHash}, Visuals: []plan.Visual{{ID: "v1", AssetID: "image", StartMS: 0, EndMS: 1000, SourceEndMS: 1000, SourceSHA256: imageHash, SourceFill: "hold_last_frame", Fit: "cover"}}, Captions: []plan.Caption{{ID: "c1", StartMS: 0, EndMS: 1000, Text: "hello"}}}
	planPath := filepath.Join(root, ".auto-editor", "plan.json")
	if err := artifact.Write(planPath, p); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "output.mp4")
	report := filepath.Join(root, ".auto-editor", "render-report.json")
	settings := config.Default().Render
	result, err := Build(context.Background(), Options{PlanPath: planPath, OutputPath: output, ReportPath: report, WorkDir: filepath.Join(root, "work"), FFprobe: "ffprobe", Settings: settings}, Dependencies{Runner: fakeRunner{}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Output.DurationMS != 1000 || !result.Settings.Captions {
		t.Fatalf("unexpected result: %#v", result)
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("output: %v", err)
	}
	if _, err := os.Stat(report); err != nil {
		t.Fatalf("report: %v", err)
	}
}

func TestValidatePlanAllowsTimelineGaps(t *testing.T) {
	p := plan.Result{Format: plan.Format{AspectRatio: "16:9", DurationMS: 1000}, Visuals: []plan.Visual{{ID: "gap", AssetID: "a", StartMS: 100, EndMS: 1000, SourceEndMS: 900, SourceFill: "hold_last_frame", Fit: "cover"}}}
	if err := validatePlan(p, config.Default().Render); err != nil {
		t.Fatalf("validatePlan() error = %v", err)
	}
}

func TestWriteASSEscapesText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "captions.ass")
	if err := WriteASS(path, []plan.Caption{{StartMS: 0, EndMS: 1000, Text: "{hello}\\nworld"}}, config.Default().Render.Captions, 1920, 1080); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\\{hello\\}\\\\nworld") {
		t.Fatalf("unescaped captions: %s", data)
	}
}

func TestBuildWithFFmpeg(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not installed")
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".auto-editor"), 0o755); err != nil {
		t.Fatal(err)
	}
	image := filepath.Join(root, "image.jpg")
	video := filepath.Join(root, "video.mp4")
	voice := filepath.Join(root, "voice.wav")
	music := filepath.Join(root, "music.wav")
	runFFmpeg(t, ffmpeg, "-f", "lavfi", "-i", "color=c=navy:s=320x180:d=1", "-frames:v", "1", image)
	runFFmpeg(t, ffmpeg, "-f", "lavfi", "-i", "color=c=green:s=320x180:d=0.25", "-c:v", "libx264", "-pix_fmt", "yuv420p", video)
	runFFmpeg(t, ffmpeg, "-f", "lavfi", "-i", "sine=frequency=440:duration=1", voice)
	runFFmpeg(t, ffmpeg, "-f", "lavfi", "-i", "sine=frequency=220:duration=1", music)
	voiceHash, err := hash(voice)
	if err != nil {
		t.Fatal(err)
	}
	imageHash, err := hash(image)
	if err != nil {
		t.Fatal(err)
	}
	videoHash, err := hash(video)
	if err != nil {
		t.Fatal(err)
	}
	if err := artifact.Write(filepath.Join(root, ".auto-editor", "alignment.json"), alignment.Result{Voiceover: alignment.Voiceover{ID: "voice", Path: "voice.wav", SHA256: voiceHash, Audio: audio.Probe{DurationMS: 1000}}}); err != nil {
		t.Fatal(err)
	}
	if err := artifact.Write(filepath.Join(root, ".auto-editor", "assets.json"), analyzeassets.Result{Assets: []analyzeassets.Asset{{AssetID: "image", Path: "image.jpg", Kind: "image", SHA256: imageHash}, {AssetID: "video", Path: "video.mp4", Kind: "video", SHA256: videoHash}}}); err != nil {
		t.Fatal(err)
	}
	musicHash, err := hash(music)
	if err != nil {
		t.Fatal(err)
	}
	p := plan.Result{Version: plan.Version, Format: plan.Format{AspectRatio: "16:9", DurationMS: 1000}, Sources: plan.Sources{Alignment: ".auto-editor/alignment.json", Assets: ".auto-editor/assets.json"}, Voiceover: plan.Voiceover{AssetID: "voice", Path: "voice.wav", SHA256: voiceHash}, Visuals: []plan.Visual{{ID: "v1", AssetID: "image", StartMS: 100, EndMS: 500, SourceEndMS: 400, SourceSHA256: imageHash, SourceFill: "hold_last_frame", Fit: "cover"}, {ID: "v2", AssetID: "video", StartMS: 500, EndMS: 900, SourceEndMS: 250, SourceSHA256: videoHash, SourceFill: "hold_last_frame", Fit: "cover"}}, AudioTracks: []plan.AudioTrack{{AssetID: "music", Path: "music.wav", SHA256: musicHash, StartMS: 0, EndMS: 1000, DuckUnderVoiceover: true}}}
	planPath := filepath.Join(root, ".auto-editor", "plan.json")
	if err := artifact.Write(planPath, p); err != nil {
		t.Fatal(err)
	}
	settings := config.Default().Render
	settings.FFmpeg, settings.FFprobe = ffmpeg, "ffprobe"
	settings.Width, settings.Height, settings.Captions.Enabled = 320, 180, false
	result, err := Build(context.Background(), Options{PlanPath: planPath, OutputPath: filepath.Join(root, "output.mp4"), ReportPath: filepath.Join(root, ".auto-editor", "render-report.json"), FFprobe: "ffprobe", Settings: settings}, Dependencies{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Output.Width != 320 || result.Output.Height != 180 {
		t.Fatalf("unexpected output: %#v", result.Output)
	}
	if len(result.Warnings) != 3 || !strings.Contains(strings.Join(result.Warnings, "\n"), "held its final video frame") || !strings.Contains(strings.Join(result.Warnings, "\n"), "black filler") {
		t.Fatalf("unexpected warnings: %#v", result.Warnings)
	}
}

func runFFmpeg(t *testing.T, executable string, args ...string) {
	t.Helper()
	output, err := exec.Command(executable, append([]string{"-hide_banner", "-nostdin", "-y"}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("ffmpeg %v: %v\n%s", args, err, output)
	}
}

type fakeRunner struct{}

func (fakeRunner) Run(_ context.Context, _, executable string, args ...string) ([]byte, error) {
	if executable == "ffprobe" {
		return []byte(`{"format":{"duration":"1.000"},"streams":[{"codec_type":"video","width":1920,"height":1080,"r_frame_rate":"30/1"}]}`), nil
	}
	if len(args) > 0 {
		if err := os.WriteFile(args[len(args)-1], []byte("rendered"), 0o644); err != nil {
			return nil, err
		}
	}
	return nil, nil
}
func hash(path string) (string, error) { return ingest.FileSHA256(path) }
