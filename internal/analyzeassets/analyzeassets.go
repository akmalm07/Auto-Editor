package analyzeassets

import (
	"auto-editor/internal/artifact"
	"auto-editor/internal/ingest"
	"context"
	"fmt"
	"path/filepath"
	"time"
)

const (
	DefaultAssetsPath = ".auto-editor/assets.json"
	Version           = 2
)

type Result struct {
	Version     int                 `json:"version"`
	ProjectName string              `json:"project_name"`
	IndexPath   string              `json:"index_path"`
	GeneratedAt time.Time           `json:"generated_at"`
	Assets      []Asset             `json:"assets"`
	Warnings    []ingest.Diagnostic `json:"warnings,omitempty"`
}

type Asset struct {
	AssetID   string              `json:"asset_id"`
	Path      string              `json:"path"`
	Kind      string              `json:"kind"`
	SHA256    string              `json:"sha256"`
	Technical Technical           `json:"technical"`
	Frames    []Frame             `json:"frames,omitempty"`
	Literal   LiteralContent      `json:"literal"`
	Editorial EditorialMeaning    `json:"editorial_meaning"`
	Warnings  []ingest.Diagnostic `json:"warnings,omitempty"`
}

type Frame struct {
	Position    string `json:"position"`
	Path        string `json:"path"`
	TimestampMS int64  `json:"timestamp_ms"`
}

type Technical struct {
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	FrameRate  string `json:"frame_rate,omitempty"`
	Codec      string `json:"codec,omitempty"`
	SampleRate int    `json:"sample_rate,omitempty"`
	Channels   int    `json:"channels,omitempty"`
	HasAudio   bool   `json:"has_audio"`
}

type LiteralContent struct {
	Status      string   `json:"status"`
	Summary     string   `json:"summary,omitempty"`
	Subjects    []string `json:"subjects,omitempty"`
	Actions     []string `json:"actions,omitempty"`
	Settings    []string `json:"settings,omitempty"`
	Mood        []string `json:"mood,omitempty"`
	VisibleText []string `json:"visible_text,omitempty"`
}

type EditorialMeaning struct {
	Status        string   `json:"status"`
	Summary       string   `json:"summary,omitempty"`
	Intent        string   `json:"intent,omitempty"`
	EmotionalBeat string   `json:"emotional_beat,omitempty"`
	SuggestedUses []string `json:"suggested_uses,omitempty"`
}

type SemanticAnalysis struct {
	Literal   LiteralContent   `json:"literal"`
	Editorial EditorialMeaning `json:"editorial_meaning"`
}

type Inspector interface {
	Inspect(path, kind, executable string) (Technical, error)
}
type SemanticAnalyzer interface {
	Analyze(context.Context, []string) (SemanticAnalysis, error)
}
type FrameExtractor interface {
	Extract(videoPath, outputPath, executable string, timestampMS int64) error
}
type Dependencies struct {
	Inspector        Inspector
	SemanticAnalyzer SemanticAnalyzer
	FrameExtractor   FrameExtractor
}
type Options struct {
	IndexPath, FFprobe, FFmpeg, ThumbnailDir string
	SemanticEnabled                          bool
}
type StageReporter func(current, total int, message string)

func Build(ctx context.Context, options Options, deps Dependencies, report StageReporter) (Result, error) {
	if deps.Inspector == nil {
		deps.Inspector = SystemInspector{}
	}
	if deps.FrameExtractor == nil {
		deps.FrameExtractor = SystemFrameExtractor{}
	}
	index, err := ingest.ReadIndex(options.IndexPath)
	if err != nil {
		return Result{}, err
	}
	if len(index.Assets) == 0 {
		return Result{}, fmt.Errorf("ingest index contains no assets")
	}
	result := Result{Version: Version, ProjectName: index.ProjectName, IndexPath: artifact.DisplayPath(index.ProjectRoot, options.IndexPath), GeneratedAt: time.Now().UTC()}
	total := len(index.Assets)
	for i, record := range index.Assets {
		if report != nil {
			report(i+1, total, fmt.Sprintf("Analyzing asset %d/%d: %s", i+1, total, record.Path))
		}
		path := artifact.ResolvePath(index.ProjectRoot, record.Path)
		asset := Asset{AssetID: record.ID, Path: record.Path, Kind: record.Kind, SHA256: record.SHA256, Literal: LiteralContent{Status: "not_applicable"}, Editorial: EditorialMeaning{Status: "not_applicable"}}
		if err := ingest.VerifyRecord(path, record); err != nil {
			return Result{}, fmt.Errorf("asset %s changed since ingest: %w; run ingest again", record.Path, err)
		}
		technical, err := deps.Inspector.Inspect(path, record.Kind, options.FFprobe)
		if err != nil {
			asset.Warnings = append(asset.Warnings, warning("assets."+record.ID, err.Error(), record.Path))
			result.Warnings = append(result.Warnings, asset.Warnings...)
			asset.Literal = LiteralContent{Status: "skipped"}
			asset.Editorial = EditorialMeaning{Status: "skipped"}
			result.Assets = append(result.Assets, asset)
			continue
		}
		asset.Technical = technical
		if !isVisual(record.Kind) {
			result.Assets = append(result.Assets, asset)
			continue
		}
		if !options.SemanticEnabled || deps.SemanticAnalyzer == nil {
			asset.Literal = LiteralContent{Status: "skipped"}
			asset.Editorial = EditorialMeaning{Status: "skipped"}
			asset.Warnings = append(asset.Warnings, warning("assets."+record.ID+".semantic", "semantic analysis was not configured", record.Path))
		} else {
			semanticPaths := []string{path}
			frameExtractionFailed := false
			if record.Kind == ingest.KindVideo {
				semanticPaths = nil
				thumbnailDir := options.ThumbnailDir
				if thumbnailDir == "" {
					thumbnailDir = filepath.Join(index.ProjectRoot, ".auto-editor", "thumbnails")
				}
				for _, sample := range videoSamples(technical.DurationMS) {
					framePath := filepath.Join(thumbnailDir, record.ID+"-"+sample.Position+".jpg")
					if err := deps.FrameExtractor.Extract(path, framePath, options.FFmpeg, sample.TimestampMS); err != nil {
						asset.Literal = LiteralContent{Status: "failed"}
						asset.Editorial = EditorialMeaning{Status: "failed"}
						asset.Warnings = append(asset.Warnings, warning("assets."+record.ID+".semantic", "could not extract video frame: "+err.Error(), record.Path))
						frameExtractionFailed = true
						break
					}
					asset.Frames = append(asset.Frames, Frame{Position: sample.Position, Path: artifact.DisplayPath(index.ProjectRoot, framePath), TimestampMS: sample.TimestampMS})
					semanticPaths = append(semanticPaths, framePath)
				}
			}
			if !frameExtractionFailed {
				semantic, err := deps.SemanticAnalyzer.Analyze(ctx, semanticPaths)
				if err != nil {
					asset.Literal = LiteralContent{Status: "failed"}
					asset.Editorial = EditorialMeaning{Status: "failed"}
					asset.Warnings = append(asset.Warnings, warning("assets."+record.ID+".semantic", "semantic analysis failed: "+err.Error(), record.Path))
				} else {
					semantic.Literal.Status = "complete"
					semantic.Editorial.Status = "complete"
					asset.Literal = semantic.Literal
					asset.Editorial = semantic.Editorial
				}
			}
		}
		result.Warnings = append(result.Warnings, asset.Warnings...)
		result.Assets = append(result.Assets, asset)
	}
	return result, nil
}

type videoSample struct {
	Position    string
	TimestampMS int64
}

func videoSamples(durationMS int64) []videoSample {
	return []videoSample{
		{Position: "start", TimestampMS: durationMS / 10},
		{Position: "middle", TimestampMS: durationMS / 2},
		{Position: "end", TimestampMS: durationMS * 9 / 10},
	}
}

func Write(path string, result Result) error {
	return artifact.Write(path, result)
}
func isVisual(kind string) bool { return kind == ingest.KindImage || kind == ingest.KindVideo }
func warning(field, message, path string) ingest.Diagnostic {
	return ingest.Diagnostic{Level: "warning", Field: field, Message: message, Path: path}
}
