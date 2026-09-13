// Package render turns a validated edit plan into a deterministic MP4.
package render

import (
	"auto-editor/internal/alignment"
	"auto-editor/internal/analyzeassets"
	"auto-editor/internal/artifact"
	"auto-editor/internal/config"
	"auto-editor/internal/ingest"
	"auto-editor/internal/plan"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const DefaultReportPath = ".auto-editor/render-report.json"
const Version = 1

type Options struct {
	PlanPath, OutputPath, ReportPath, WorkDir string
	Force                                     bool
	FFprobe                                   string
	Settings                                  config.RenderConfig
}

type StageReporter func(current, total int, message string)

type Dependencies struct{ Runner Runner }

type Result struct {
	Version     int       `json:"version"`
	GeneratedAt time.Time `json:"generated_at"`
	PlanPath    string    `json:"plan_path"`
	PlanSHA256  string    `json:"plan_sha256"`
	OutputPath  string    `json:"output_path"`
	Output      Output    `json:"output"`
	Sources     []Source  `json:"sources"`
	Settings    Settings  `json:"settings"`
	Warnings    []string  `json:"warnings,omitempty"`
}

type Output struct {
	DurationMS int64  `json:"duration_ms"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	FPS        string `json:"fps"`
}

type Source struct {
	AssetID string `json:"asset_id"`
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Kind    string `json:"kind"`
}

type Settings struct {
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	FPS        int    `json:"fps"`
	VideoCodec string `json:"video_codec"`
	AudioCodec string `json:"audio_codec"`
	Captions   bool   `json:"captions"`
}

type RenderError struct {
	Stage, WorkDir string
	Err            error
}

func (e RenderError) Error() string {
	if e.WorkDir != "" {
		return fmt.Sprintf("render %s: %v (temporary files retained at %s)", e.Stage, e.Err, e.WorkDir)
	}
	return fmt.Sprintf("render %s: %v", e.Stage, e.Err)
}
func (e RenderError) Unwrap() error { return e.Err }

func Build(ctx context.Context, options Options, deps Dependencies, report StageReporter) (result Result, err error) {
	if deps.Runner == nil {
		deps.Runner = SystemRunner{}
	}
	if options.Settings.FFmpeg == "" {
		return Result{}, fmt.Errorf("render ffmpeg executable is required")
	}
	if err := validateSettings(options.Settings); err != nil {
		return Result{}, err
	}
	if options.PlanPath == "" || options.OutputPath == "" {
		return Result{}, fmt.Errorf("render plan and output paths are required")
	}

	planPath, err := filepath.Abs(options.PlanPath)
	if err != nil {
		return Result{}, err
	}
	projectRoot := projectRootFor(planPath)
	planValue, planHash, err := readPlan(planPath)
	if err != nil {
		return Result{}, err
	}
	if err := validatePlan(planValue, options.Settings); err != nil {
		return Result{}, err
	}

	stage := func(current int, message string) {
		if report != nil {
			report(current, 6, message)
		}
	}
	stage(1, "Validating render inputs")
	assetsPath := artifact.ResolvePath(projectRoot, planValue.Sources.Assets)
	assets, err := artifact.Read[analyzeassets.Result](assetsPath)
	if err != nil {
		return Result{}, fmt.Errorf("read plan asset artifact: %w", err)
	}
	alignmentPath := artifact.ResolvePath(projectRoot, planValue.Sources.Alignment)
	aligned, err := artifact.Read[alignment.Result](alignmentPath)
	if err != nil {
		return Result{}, fmt.Errorf("read plan alignment artifact: %w", err)
	}
	voiceover, sources, musicTracks, err := verifySources(projectRoot, planValue, assets, aligned)
	if err != nil {
		return Result{}, err
	}

	outputPath, err := filepath.Abs(options.OutputPath)
	if err != nil {
		return Result{}, err
	}
	if _, err := os.Stat(outputPath); err == nil && !options.Force {
		return Result{}, fmt.Errorf("output already exists at %s; pass --force to replace it", outputPath)
	} else if err != nil && !os.IsNotExist(err) {
		return Result{}, err
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return Result{}, err
	}
	workDir := options.WorkDir
	if workDir == "" {
		workDir, err = os.MkdirTemp("", "auto-editor-render-")
	} else {
		err = os.MkdirAll(workDir, 0o755)
	}
	if err != nil {
		return Result{}, fmt.Errorf("create render workspace: %w", err)
	}
	completed := false
	defer func() {
		if completed {
			_ = os.RemoveAll(workDir)
		}
	}()
	fail := func(stage string, cause error) (Result, error) {
		return Result{}, RenderError{Stage: stage, WorkDir: workDir, Err: cause}
	}

	stage(2, "Creating normalized visual clips")
	clips, warnings, err := buildClips(ctx, deps.Runner, workDir, projectRoot, planValue, sources, options.Settings, report)
	if err != nil {
		return fail("visual clips", err)
	}
	stage(3, "Joining visual clips")
	videoPath := filepath.Join(workDir, "video.mp4")
	if err := concatClips(ctx, deps.Runner, workDir, clips, videoPath, options.Settings); err != nil {
		return fail("join clips", err)
	}

	captionPath := ""
	if options.Settings.Captions.Enabled {
		stage(4, "Generating captions")
		captionPath = filepath.Join(workDir, "captions.ass")
		if err := WriteASS(captionPath, planValue.Captions, options.Settings.Captions, options.Settings.Width, options.Settings.Height); err != nil {
			return fail("captions", err)
		}
	}
	stage(5, "Muxing voiceover and finalizing video")
	temp, err := os.CreateTemp(filepath.Dir(outputPath), ".auto-editor-render-*.mp4")
	if err != nil {
		return fail("create output", err)
	}
	tempPath := temp.Name()
	_ = temp.Close()
	_ = os.Remove(tempPath)
	defer func() { _ = os.Remove(tempPath) }()
	if err := muxFinal(ctx, deps.Runner, workDir, videoPath, voiceover, captionPath, musicTracks, tempPath, options.Settings); err != nil {
		return fail("mux output", err)
	}
	stage(6, "Verifying rendered output")
	probe, err := probeOutput(ctx, deps.Runner, options.FFprobe, tempPath)
	if err != nil {
		return fail("verify output", err)
	}
	if err := validateOutput(probe, planValue, options.Settings); err != nil {
		return fail("verify output", err)
	}
	if options.Force {
		_ = os.Remove(outputPath)
	}
	if err := os.Rename(tempPath, outputPath); err != nil {
		return fail("publish output", err)
	}

	reportPath := options.ReportPath
	if reportPath == "" {
		reportPath = filepath.Join(projectRoot, DefaultReportPath)
	}
	if !filepath.IsAbs(reportPath) {
		reportPath = filepath.Join(projectRoot, reportPath)
	}
	result = Result{Version: Version, GeneratedAt: time.Now().UTC(), PlanPath: artifact.DisplayPath(projectRoot, planPath), PlanSHA256: planHash, OutputPath: artifact.DisplayPath(projectRoot, outputPath), Output: probe, Sources: sources, Settings: Settings{Width: options.Settings.Width, Height: options.Settings.Height, FPS: options.Settings.FPS, VideoCodec: options.Settings.VideoCodec, AudioCodec: options.Settings.AudioCodec, Captions: options.Settings.Captions.Enabled}, Warnings: warnings}
	if err := artifact.Write(reportPath, result); err != nil {
		return fail("write report", err)
	}
	completed = true
	return result, nil
}

func projectRootFor(planPath string) string {
	dir := filepath.Dir(planPath)
	if filepath.Base(dir) == ".auto-editor" {
		return filepath.Dir(dir)
	}
	return dir
}

func readPlan(path string) (plan.Result, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return plan.Result{}, "", err
	}
	value, err := artifact.Read[plan.Result](path)
	if err != nil {
		return plan.Result{}, "", err
	}
	if value.Version != plan.Version {
		return plan.Result{}, "", fmt.Errorf("unsupported plan version %d; run plan again", value.Version)
	}
	sum := sha256.Sum256(data)
	return value, hex.EncodeToString(sum[:]), nil
}

func verifySources(root string, p plan.Result, assets analyzeassets.Result, aligned alignment.Result) (string, []Source, []plan.AudioTrack, error) {
	if p.Voiceover.AssetID == "" || p.Voiceover.Path == "" || p.Voiceover.SHA256 == "" {
		return "", nil, nil, fmt.Errorf("plan voiceover fingerprint is missing; run plan again")
	}
	if p.Voiceover.AssetID != aligned.Voiceover.ID || p.Voiceover.SHA256 != aligned.Voiceover.SHA256 {
		return "", nil, nil, fmt.Errorf("plan voiceover does not match its alignment artifact; run plan again")
	}
	voicePath := artifact.ResolvePath(root, p.Voiceover.Path)
	if err := verifyHash(voicePath, p.Voiceover.SHA256); err != nil {
		return "", nil, nil, fmt.Errorf("voiceover changed since planning: %w; run ingest, align, and plan again", err)
	}
	byID := make(map[string]analyzeassets.Asset, len(assets.Assets))
	for _, a := range assets.Assets {
		byID[a.AssetID] = a
	}
	sources := []Source{{AssetID: p.Voiceover.AssetID, Path: p.Voiceover.Path, SHA256: p.Voiceover.SHA256, Kind: "audio"}}
	for _, visual := range p.Visuals {
		asset, ok := byID[visual.AssetID]
		if !ok {
			return "", nil, nil, fmt.Errorf("plan visual %s references unknown asset %s", visual.ID, visual.AssetID)
		}
		if visual.SourceSHA256 == "" || visual.SourceSHA256 != asset.SHA256 {
			return "", nil, nil, fmt.Errorf("plan visual %s fingerprint does not match asset analysis; run plan again", visual.ID)
		}
		if err := verifyHash(artifact.ResolvePath(root, asset.Path), asset.SHA256); err != nil {
			return "", nil, nil, fmt.Errorf("asset %s changed since planning: %w; run ingest, analyze-assets, and plan again", asset.Path, err)
		}
		sources = append(sources, Source{AssetID: asset.AssetID, Path: asset.Path, SHA256: asset.SHA256, Kind: asset.Kind})
	}
	musicTracks := make([]plan.AudioTrack, 0, len(p.AudioTracks))
	for _, track := range p.AudioTracks {
		if track.AssetID == "" || track.Path == "" || track.SHA256 == "" || track.EndMS <= track.StartMS || track.VolumeDB > 12 || track.VolumeDB < -60 {
			return "", nil, nil, fmt.Errorf("music track %q has invalid metadata", track.AssetID)
		}
		path := artifact.ResolvePath(root, track.Path)
		if err := verifyHash(path, track.SHA256); err != nil {
			return "", nil, nil, fmt.Errorf("music asset %s changed since planning: %w; run ingest and plan again", track.Path, err)
		}
		track.Path = path
		musicTracks = append(musicTracks, track)
		relPath, _ := filepath.Rel(root, path)
		sources = append(sources, Source{AssetID: track.AssetID, Path: filepath.ToSlash(relPath), SHA256: track.SHA256, Kind: "music"})
	}
	return voicePath, sources, musicTracks, nil
}

func verifyHash(path, expected string) error {
	actual, err := ingest.FileSHA256(path)
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("file content hash no longer matches the plan")
	}
	return nil
}

func validateSettings(s config.RenderConfig) error {
	if s.Width <= 0 || s.Height <= 0 || s.FPS <= 0 {
		return fmt.Errorf("render width, height, and fps must be positive")
	}
	if s.VideoCodec == "" || s.AudioCodec == "" || s.AudioBitrate == "" {
		return fmt.Errorf("render codec settings cannot be empty")
	}
	return nil
}

func validatePlan(p plan.Result, s config.RenderConfig) error {
	parts := strings.Split(p.Format.AspectRatio, ":")
	if len(parts) != 2 {
		return fmt.Errorf("plan has invalid aspect ratio %q", p.Format.AspectRatio)
	}
	var x, y int
	if _, err := fmt.Sscanf(p.Format.AspectRatio, "%d:%d", &x, &y); err != nil || x <= 0 || y <= 0 || s.Width*y != s.Height*x {
		return fmt.Errorf("render canvas %dx%d does not match plan aspect ratio %q", s.Width, s.Height, p.Format.AspectRatio)
	}
	if p.Format.DurationMS <= 0 || len(p.Visuals) == 0 {
		return fmt.Errorf("plan has no renderable timeline")
	}
	previousEnd := int64(0)
	for _, v := range p.Visuals {
		if v.ID == "" || v.AssetID == "" || v.Fit != "cover" || v.SourceFill != "hold_last_frame" || v.StartMS < previousEnd || v.EndMS <= v.StartMS || v.SourceStartMS < 0 || v.SourceEndMS <= v.SourceStartMS {
			return fmt.Errorf("plan visual %q has unsupported or invalid timing", v.ID)
		}
		previousEnd = v.EndMS
	}
	if previousEnd > p.Format.DurationMS {
		return fmt.Errorf("plan visuals end at %dms, after plan duration %dms", previousEnd, p.Format.DurationMS)
	}
	for _, c := range p.Captions {
		if c.EndMS <= c.StartMS || c.StartMS < 0 || c.EndMS > p.Format.DurationMS || strings.TrimSpace(c.Text) == "" {
			return fmt.Errorf("plan caption %q has invalid timing or text", c.ID)
		}
	}
	for _, track := range p.AudioTracks {
		if track.AssetID == "" || track.Path == "" || track.SHA256 == "" || track.StartMS < 0 || track.EndMS <= track.StartMS || track.EndMS > p.Format.DurationMS {
			return fmt.Errorf("music track %q has unsupported or invalid timing", track.AssetID)
		}
	}
	return nil
}

func validateOutput(out Output, p plan.Result, s config.RenderConfig) error {
	if out.Width != s.Width || out.Height != s.Height {
		return fmt.Errorf("output is %dx%d; expected %dx%d", out.Width, out.Height, s.Width, s.Height)
	}
	if out.DurationMS < p.Format.DurationMS-250 || out.DurationMS > p.Format.DurationMS+250 {
		return fmt.Errorf("output duration %dms differs from plan duration %dms", out.DurationMS, p.Format.DurationMS)
	}
	return nil
}
