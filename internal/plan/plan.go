package plan

import (
	"auto-editor/internal/alignment"
	"auto-editor/internal/analyzeassets"
	"auto-editor/internal/analyzestyle"
	"auto-editor/internal/artifact"
	"auto-editor/internal/ingest"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const DefaultPlanPath = ".auto-editor/plan.json"
const Version = 2

type Result struct {
	Version     int                 `json:"version"`
	ProjectName string              `json:"project_name"`
	GeneratedAt time.Time           `json:"generated_at"`
	Format      Format              `json:"format"`
	Sources     Sources             `json:"sources"`
	Voiceover   Voiceover           `json:"voiceover"`
	Selection   AssetSelection      `json:"asset_selection"`
	Visuals     []Visual            `json:"visuals"`
	Captions    []Caption           `json:"captions"`
	Warnings    []ingest.Diagnostic `json:"warnings,omitempty"`
}
type Format struct {
	AspectRatio string `json:"aspect_ratio"`
	DurationMS  int64  `json:"duration_ms"`
}
type Sources struct {
	Alignment string `json:"alignment"`
	Assets    string `json:"assets"`
	Style     string `json:"style,omitempty"`
}
type Voiceover struct {
	AssetID string `json:"asset_id"`
	Path    string `json:"path"`
}
type Visual struct {
	ID            string `json:"id"`
	AssetID       string `json:"asset_id"`
	StartMS       int64  `json:"start_ms"`
	EndMS         int64  `json:"end_ms"`
	SourceStartMS int64  `json:"source_start_ms"`
	SourceEndMS   int64  `json:"source_end_ms"`
	Fit           string `json:"fit"`
	Role          string `json:"role"`
}
type Caption struct {
	ID             string `json:"id"`
	StartMS        int64  `json:"start_ms"`
	EndMS          int64  `json:"end_ms"`
	Text           string `json:"text"`
	ScriptWordFrom int    `json:"script_word_from"`
	ScriptWordTo   int    `json:"script_word_to"`
}
type Options struct {
	AlignmentPath, AssetsPath, StylePath, OutputPath string
	AspectRatio                                      string
}
type StageReporter func(current, total int, message string)

func Build(ctx context.Context, options Options, report StageReporter) (Result, error) {
	_ = ctx
	projectRoot := filepath.Dir(options.AlignmentPath)
	if filepath.Base(projectRoot) == ".auto-editor" {
		projectRoot = filepath.Dir(projectRoot)
	}
	resolve := func(path string) string {
		if filepath.IsAbs(path) {
			return path
		}
		return filepath.Join(projectRoot, path)
	}
	alignmentResult, err := artifact.Read[alignment.Result](resolve(options.AlignmentPath))
	if err != nil {
		return Result{}, fmt.Errorf("read alignment artifact: %w", err)
	}
	assetsResult, err := artifact.Read[analyzeassets.Result](resolve(options.AssetsPath))
	if err != nil {
		return Result{}, fmt.Errorf("read asset artifact: %w", err)
	}
	style := analyzestyle.Result{}
	styleData, styleErr := os.ReadFile(resolve(options.StylePath))
	if styleErr == nil {
		if err := json.Unmarshal(styleData, &style); err != nil {
			return Result{}, fmt.Errorf("decode style artifact: %w", err)
		}
	}
	if len(assetsResult.Assets) == 0 {
		return Result{}, fmt.Errorf("asset artifact contains no assets")
	}
	visuals := make([]analyzeassets.Asset, 0)
	for _, asset := range assetsResult.Assets {
		if asset.Kind == ingest.KindImage || asset.Kind == ingest.KindVideo {
			visuals = append(visuals, asset)
		}
	}
	if len(visuals) == 0 {
		return Result{}, fmt.Errorf("asset artifact contains no visual assets")
	}
	duration := alignmentResult.Voiceover.Audio.DurationMS
	if duration <= 0 && len(alignmentResult.Segments) > 0 {
		duration = alignmentResult.Segments[len(alignmentResult.Segments)-1].EndMS
	}
	result := Result{Version: Version, ProjectName: alignmentResult.ProjectName, GeneratedAt: time.Now().UTC(), Format: Format{AspectRatio: options.AspectRatio, DurationMS: duration}, Sources: Sources{Alignment: options.AlignmentPath, Assets: options.AssetsPath}, Voiceover: Voiceover{AssetID: alignmentResult.Voiceover.ID, Path: alignmentResult.Voiceover.Path}}
	if styleErr == nil {
		result.Sources.Style = options.StylePath
	} else {
		result.Warnings = append(result.Warnings, ingest.Diagnostic{Level: "warning", Field: "style", Message: "style artifact not found; using deterministic asset selection"})
	}
	visuals, result.Selection = rankVisualAssets(visuals, style, styleErr == nil)
	for index, segment := range alignmentResult.Segments {
		if report != nil {
			report(index+1, len(alignmentResult.Segments), fmt.Sprintf("Planning segment %d/%d: %s", index+1, len(alignmentResult.Segments), segment.Text))
		}
		asset := visuals[index%len(visuals)]
		end := segment.EndMS
		if end <= segment.StartMS {
			continue
		}
		sourceEnd := end - segment.StartMS
		if asset.Kind == ingest.KindVideo && asset.Technical.DurationMS > 0 && sourceEnd > asset.Technical.DurationMS {
			sourceEnd = asset.Technical.DurationMS
		}
		role := "b_roll"
		if asset.Editorial.Intent != "" {
			role = asset.Editorial.Intent
		}
		result.Visuals = append(result.Visuals, Visual{ID: fmt.Sprintf("visual_%03d", index), AssetID: asset.AssetID, StartMS: segment.StartMS, EndMS: end, SourceEndMS: sourceEnd, Fit: "cover", Role: role})
		result.Captions = append(result.Captions, Caption{ID: fmt.Sprintf("caption_%03d", index), StartMS: segment.StartMS, EndMS: end, Text: segment.Text, ScriptWordFrom: segment.ScriptWordFrom, ScriptWordTo: segment.ScriptWordTo})
	}
	return result, nil
}

func Write(path string, result Result) error {
	return artifact.Write(path, result)
}
