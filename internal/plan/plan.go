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
const Version = 5

type Result struct {
	Version     int                 `json:"version"`
	ProjectName string              `json:"project_name"`
	GeneratedAt time.Time           `json:"generated_at"`
	Format      Format              `json:"format"`
	Sources     Sources             `json:"sources"`
	Voiceover   Voiceover           `json:"voiceover"`
	Selection   AssetSelection      `json:"asset_selection"`
	Semantic    SemanticPlan        `json:"semantic_plan"`
	Visuals     []Visual            `json:"visuals"`
	Captions    []Caption           `json:"captions"`
	AudioTracks []AudioTrack        `json:"audio_tracks,omitempty"`
	AudioCues   []AudioCue          `json:"audio_cues,omitempty"`
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
	SHA256  string `json:"sha256"`
}
type Visual struct {
	ID                string `json:"id"`
	AssetID           string `json:"asset_id"`
	StartMS           int64  `json:"start_ms"`
	EndMS             int64  `json:"end_ms"`
	SourceStartMS     int64  `json:"source_start_ms"`
	SourceEndMS       int64  `json:"source_end_ms"`
	SourceSHA256      string `json:"source_sha256"`
	SourceFill        string `json:"source_fill"`
	Fit               string `json:"fit"`
	Role              string `json:"role"`
	SemanticRationale string `json:"semantic_rationale,omitempty"`
}
type Caption struct {
	ID             string `json:"id"`
	StartMS        int64  `json:"start_ms"`
	EndMS          int64  `json:"end_ms"`
	Text           string `json:"text"`
	ScriptWordFrom int    `json:"script_word_from"`
	ScriptWordTo   int    `json:"script_word_to"`
}
type AudioTrack struct {
	AssetID            string  `json:"asset_id"`
	Path               string  `json:"path"`
	SHA256             string  `json:"sha256"`
	StartMS            int64   `json:"start_ms"`
	EndMS              int64   `json:"end_ms"`
	SourceStartMS      int64   `json:"source_start_ms"`
	VolumeDB           float64 `json:"volume_db"`
	DuckUnderVoiceover bool    `json:"duck_under_voiceover"`
	FadeInMS           int64   `json:"fade_in_ms"`
	FadeOutMS          int64   `json:"fade_out_ms"`
	Loop               bool    `json:"loop,omitempty"`
}
type AudioCue struct {
	AssetID       string  `json:"asset_id"`
	Path          string  `json:"path"`
	SHA256        string  `json:"sha256"`
	StartMS       int64   `json:"start_ms"`
	SourceStartMS int64   `json:"source_start_ms"`
	DurationMS    int64   `json:"duration_ms"`
	VolumeDB      float64 `json:"volume_db,omitempty"`
}
type Options struct {
	AlignmentPath, AssetsPath, StylePath, OutputPath string
	AspectRatio                                      string
	SemanticEnabled                                  bool
	AllAssets                                        bool
	AllViewableAssets                                bool
	AllAudioAssets                                   bool
	IndexPath                                        string
}
type StageReporter func(current, total int, message string)
type Dependencies struct{ SemanticPlanner SemanticPlanner }

func Build(ctx context.Context, options Options, report StageReporter) (Result, error) {
	return BuildWithDependencies(ctx, options, Dependencies{}, report)
}

func BuildWithDependencies(ctx context.Context, options Options, deps Dependencies, report StageReporter) (Result, error) {
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
	audioCues := make([]analyzeassets.Asset, 0)
	for _, asset := range assetsResult.Assets {
		if asset.Kind == ingest.KindAudio && asset.AssetID != alignmentResult.Voiceover.ID {
			audioCues = append(audioCues, asset)
		}
	}
	duration := alignmentResult.Voiceover.Audio.DurationMS
	if duration <= 0 && len(alignmentResult.Segments) > 0 {
		duration = alignmentResult.Segments[len(alignmentResult.Segments)-1].EndMS
	}
	result := Result{Version: Version, ProjectName: alignmentResult.ProjectName, GeneratedAt: time.Now().UTC(), Format: Format{AspectRatio: options.AspectRatio, DurationMS: duration}, Sources: Sources{Alignment: options.AlignmentPath, Assets: options.AssetsPath}, Voiceover: Voiceover{AssetID: alignmentResult.Voiceover.ID, Path: alignmentResult.Voiceover.Path, SHA256: alignmentResult.Voiceover.SHA256}, Semantic: SemanticPlan{Status: "skipped"}}
	if options.IndexPath != "" {
		if index, indexErr := ingest.ReadIndex(resolve(options.IndexPath)); indexErr == nil {
			for _, music := range index.Music {
				result.AudioTracks = append(result.AudioTracks, AudioTrack{AssetID: music.ID, Path: music.Path, SHA256: music.SHA256, StartMS: 0, EndMS: duration, DuckUnderVoiceover: true, Loop: true})
			}
		} else {
			result.Warnings = append(result.Warnings, ingest.Diagnostic{Level: "warning", Field: "inputs.music", Message: "could not read music records: " + indexErr.Error()})
		}
	}
	if styleErr == nil {
		result.Sources.Style = options.StylePath
	} else {
		result.Warnings = append(result.Warnings, ingest.Diagnostic{Level: "warning", Field: "style", Message: "style artifact not found; using deterministic asset selection"})
	}
	visuals, result.Selection = rankVisualAssets(visuals, style, styleErr == nil)
	if options.SemanticEnabled && deps.SemanticPlanner != nil && !options.AllViewableAssets {
		if report != nil {
			report(1, 1, "Planning semantic visual beats")
		}
		beats, semanticErr := deps.SemanticPlanner.Plan(ctx, semanticRequest(alignmentResult, visuals, result.Selection.Preferences, options.AllAssets))
		if semanticErr == nil {
			semanticErr = validateSemanticBeats(beats, alignmentResult.Transcript.Words, visuals)
		}
		if semanticErr == nil && options.AllAssets {
			semanticErr = validateAllAssets(beats, visuals)
		}
		if semanticErr == nil {
			byID := make(map[string]analyzeassets.Asset, len(visuals))
			for _, asset := range visuals {
				byID[asset.AssetID] = asset
			}
			result.Semantic = SemanticPlan{Status: "complete", Beats: beats}
			for index, beat := range beats {
				start, end := alignmentResult.Transcript.Words[beat.WordFrom].StartMS, alignmentResult.Transcript.Words[beat.WordTo].EndMS
				asset := byID[beat.AssetID]
				sourceEnd := end - start
				if asset.Kind == ingest.KindVideo && asset.Technical.DurationMS > 0 && sourceEnd > asset.Technical.DurationMS {
					sourceEnd = asset.Technical.DurationMS
				}
				role := beat.Role
				if role == "" {
					role = asset.Editorial.Intent
				}
				if role == "" {
					role = "b_roll"
				}
				result.Visuals = append(result.Visuals, Visual{ID: fmt.Sprintf("visual_%03d", index), AssetID: asset.AssetID, StartMS: start, EndMS: end, SourceEndMS: sourceEnd, SourceSHA256: asset.SHA256, SourceFill: "hold_last_frame", Fit: "cover", Role: role, SemanticRationale: beat.Rationale})
				result.Captions = append(result.Captions, Caption{ID: fmt.Sprintf("caption_%03d", index), StartMS: start, EndMS: end, Text: wordsText(alignmentResult.Transcript.Words[beat.WordFrom : beat.WordTo+1])})
			}
			result.Visuals = fillTimelineGaps(result.Visuals, duration)
			if options.AllAudioAssets || options.AllAssets {
				result.AudioCues = distributeAudioCues(audioCues, duration)
			}
			return result, nil
		}
		result.Semantic = SemanticPlan{Status: "failed"}
		result.Warnings = append(result.Warnings, ingest.Diagnostic{Level: "warning", Field: "planning.semantic", Message: "semantic planning failed; using deterministic segments: " + semanticErr.Error()})
	} else if options.SemanticEnabled {
		result.Warnings = append(result.Warnings, ingest.Diagnostic{Level: "warning", Field: "planning.semantic", Message: "semantic planning was not configured; using deterministic segments"})
	}
	if options.AllAssets || options.AllViewableAssets {
		beats, forcedErr := forcedBeats(alignmentResult.Transcript.Words, visuals)
		if forcedErr != nil {
			return Result{}, forcedErr
		}
		result.Semantic = SemanticPlan{Status: "forced_deterministic", Beats: beats}
		result.Visuals = nil
		result.Captions = nil
		for index, beat := range beats {
			asset := visuals[index]
			start, end := alignmentResult.Transcript.Words[beat.WordFrom].StartMS, alignmentResult.Transcript.Words[beat.WordTo].EndMS
			sourceEnd := end - start
			if asset.Kind == ingest.KindVideo && asset.Technical.DurationMS > 0 && sourceEnd > asset.Technical.DurationMS {
				sourceEnd = asset.Technical.DurationMS
			}
			result.Visuals = append(result.Visuals, Visual{ID: fmt.Sprintf("visual_%03d", index), AssetID: asset.AssetID, StartMS: start, EndMS: end, SourceEndMS: sourceEnd, SourceSHA256: asset.SHA256, SourceFill: "hold_last_frame", Fit: "cover", Role: asset.Editorial.Intent, SemanticRationale: "--all-assets constraint"})
			result.Captions = append(result.Captions, Caption{ID: fmt.Sprintf("caption_%03d", index), StartMS: start, EndMS: end, Text: wordsText(alignmentResult.Transcript.Words[beat.WordFrom : beat.WordTo+1])})
		}
		if options.AllAssets || options.AllAudioAssets {
			result.AudioCues = distributeAudioCues(audioCues, duration)
		}
		result.Visuals = fillTimelineGaps(result.Visuals, duration)
		return result, nil
	}
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
		result.Visuals = append(result.Visuals, Visual{ID: fmt.Sprintf("visual_%03d", index), AssetID: asset.AssetID, StartMS: segment.StartMS, EndMS: end, SourceEndMS: sourceEnd, SourceSHA256: asset.SHA256, SourceFill: "hold_last_frame", Fit: "cover", Role: role})
		result.Captions = append(result.Captions, Caption{ID: fmt.Sprintf("caption_%03d", index), StartMS: segment.StartMS, EndMS: end, Text: segment.Text, ScriptWordFrom: segment.ScriptWordFrom, ScriptWordTo: segment.ScriptWordTo})
	}
	if options.AllAudioAssets || options.AllAssets {
		result.AudioCues = distributeAudioCues(audioCues, duration)
	}
	result.Visuals = fillTimelineGaps(result.Visuals, duration)
	return result, nil
}

func distributeAudioCues(assets []analyzeassets.Asset, duration int64) []AudioCue {
	if len(assets) == 0 || duration <= 0 {
		return nil
	}
	step := duration / int64(len(assets))
	if step < 1 {
		step = 1
	}
	cues := make([]AudioCue, 0, len(assets))
	for i, asset := range assets {
		start := int64(i) * step
		if start >= duration {
			start = duration - 1
		}
		length := step
		if i == len(assets)-1 || start+length > duration {
			length = duration - start
		}
		cues = append(cues, AudioCue{AssetID: asset.AssetID, Path: asset.Path, SHA256: asset.SHA256, StartMS: start, DurationMS: length, VolumeDB: -6})
	}
	return cues
}

// fillTimelineGaps keeps the picture continuous during narration pauses by
// holding the nearest planned visual instead of leaving a blank interval.
func fillTimelineGaps(visuals []Visual, duration int64) []Visual {
	if len(visuals) == 0 || duration <= 0 {
		return visuals
	}
	filled := make([]Visual, 0, len(visuals)+2)
	previousEnd := int64(0)
	for _, visual := range visuals {
		if visual.StartMS > previousEnd {
			base := visual
			if len(filled) > 0 {
				base = filled[len(filled)-1]
			}
			base.ID = fmt.Sprintf("visual_fill_%03d", len(filled))
			base.StartMS, base.EndMS = previousEnd, visual.StartMS
			base.SourceStartMS, base.SourceEndMS = 0, base.EndMS-base.StartMS
			filled = append(filled, base)
		}
		filled = append(filled, visual)
		previousEnd = visual.EndMS
	}
	if previousEnd < duration {
		base := filled[len(filled)-1]
		base.ID = fmt.Sprintf("visual_fill_%03d", len(filled))
		base.StartMS, base.EndMS = previousEnd, duration
		base.SourceStartMS, base.SourceEndMS = 0, base.EndMS-base.StartMS
		filled = append(filled, base)
	}
	return filled
}

func Write(path string, result Result) error {
	return artifact.Write(path, result)
}
