package analyzestyle

import (
	"auto-editor/internal/analyzeassets"
	"auto-editor/internal/artifact"
	"auto-editor/internal/ingest"
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	DefaultStylePath = ".auto-editor/style.json"
	Version          = 2
)

type Result struct {
	Version       int                 `json:"version"`
	ProjectName   string              `json:"project_name"`
	IndexPath     string              `json:"index_path"`
	GeneratedAt   time.Time           `json:"generated_at"`
	ReferenceSets []ReferenceSet      `json:"reference_sets"`
	Warnings      []ingest.Diagnostic `json:"warnings,omitempty"`
}

type ReferenceSet struct {
	Name     string              `json:"name"`
	Files    []FileEvidence      `json:"files"`
	Profile  TechnicalStyle      `json:"technical_style"`
	Semantic SemanticStyle       `json:"semantic_style"`
	Warnings []ingest.Diagnostic `json:"warnings,omitempty"`
}

type FileEvidence struct {
	FileID          string                  `json:"file_id"`
	Path            string                  `json:"path"`
	SHA256          string                  `json:"sha256"`
	Technical       analyzeassets.Technical `json:"technical"`
	CutTimestampsMS []int64                 `json:"cut_timestamps_ms,omitempty"`
}

type TechnicalStyle struct {
	VideosAnalyzed       int               `json:"videos_analyzed"`
	TotalDurationMS      int64             `json:"total_duration_ms"`
	MedianDurationMS     int64             `json:"median_duration_ms"`
	AspectRatios         []AspectRatioStat `json:"aspect_ratios,omitempty"`
	AudioVideoRatio      float64           `json:"audio_video_ratio"`
	CutCount             int               `json:"cut_count"`
	CutsPerMinute        float64           `json:"cuts_per_minute"`
	MedianShotDurationMS int64             `json:"median_shot_duration_ms"`
}

type AspectRatioStat struct {
	Ratio string `json:"ratio"`
	Count int    `json:"count"`
}

type SemanticStyle struct {
	Status              string          `json:"status"`
	Summary             string          `json:"summary,omitempty"`
	CaptionConventions  []string        `json:"caption_conventions,omitempty"`
	HumorPatterns       []string        `json:"humor_patterns,omitempty"`
	EmotionalPacing     []string        `json:"emotional_pacing,omitempty"`
	VisualRoles         []string        `json:"visual_roles,omitempty"`
	EditingTechniques   []string        `json:"editing_techniques,omitempty"`
	PlanningPreferences []string        `json:"planning_preferences,omitempty"`
	SupportingFrameIDs  []string        `json:"supporting_frame_ids,omitempty"`
	Confidence          string          `json:"confidence,omitempty"`
	SampledFrames       []SemanticFrame `json:"sampled_frames,omitempty"`
}

type SemanticFrame struct {
	ID          string `json:"id"`
	Path        string `json:"path"`
	TimestampMS int64  `json:"timestamp_ms"`
}

type SemanticRequest struct {
	ReferenceSet string
	Technical    TechnicalStyle
	Frames       []SemanticFrame
	FramePaths   []string
}

type SceneDetector interface {
	Detect(path, executable string) ([]int64, error)
}
type SemanticAnalyzer interface {
	AnalyzeStyle(context.Context, SemanticRequest) (SemanticStyle, error)
}
type Dependencies struct {
	Inspector        analyzeassets.Inspector
	SceneDetector    SceneDetector
	FrameExtractor   analyzeassets.FrameExtractor
	SemanticAnalyzer SemanticAnalyzer
}
type Options struct {
	IndexPath, FFprobe, FFmpeg, ThumbnailDir string
	SemanticEnabled                          bool
	SemanticMaxFrames                        int
}
type StageReporter func(current, total int, message string)

func Build(ctx context.Context, options Options, deps Dependencies, report StageReporter) (Result, error) {
	if deps.Inspector == nil {
		deps.Inspector = analyzeassets.SystemInspector{}
	}
	if deps.SceneDetector == nil {
		deps.SceneDetector = SystemSceneDetector{}
	}
	if deps.FrameExtractor == nil {
		deps.FrameExtractor = analyzeassets.SystemFrameExtractor{}
	}
	index, err := ingest.ReadIndex(options.IndexPath)
	if err != nil {
		return Result{}, err
	}
	result := Result{Version: Version, ProjectName: index.ProjectName, IndexPath: artifact.DisplayPath(index.ProjectRoot, options.IndexPath), GeneratedAt: time.Now().UTC()}
	total := 0
	for _, set := range index.ReferenceSets {
		total += len(set.Files)
	}
	if total == 0 {
		return Result{}, fmt.Errorf("ingest index contains no reference videos")
	}
	current := 0
	for _, indexedSet := range index.ReferenceSets {
		set := ReferenceSet{Name: indexedSet.Name, Semantic: SemanticStyle{Status: "skipped"}}
		for _, record := range indexedSet.Files {
			current++
			if report != nil {
				report(current, total, fmt.Sprintf("Analyzing reference %d/%d: %s", current, total, record.Path))
			}
			path := artifact.ResolvePath(index.ProjectRoot, record.Path)
			if err := ingest.VerifyRecord(path, record); err != nil {
				return Result{}, fmt.Errorf("reference video %s changed since ingest: %w; run ingest again", record.Path, err)
			}
			technical, err := deps.Inspector.Inspect(path, record.Kind, options.FFprobe)
			if err != nil {
				diagnostic := warning("reference_sets."+indexedSet.Name, err.Error(), record.Path)
				set.Warnings = append(set.Warnings, diagnostic)
				result.Warnings = append(result.Warnings, diagnostic)
				continue
			}
			cuts, err := deps.SceneDetector.Detect(path, options.FFmpeg)
			if err != nil {
				diagnostic := warning("reference_sets."+indexedSet.Name+".cuts", err.Error(), record.Path)
				set.Warnings = append(set.Warnings, diagnostic)
				result.Warnings = append(result.Warnings, diagnostic)
			}
			set.Files = append(set.Files, FileEvidence{FileID: record.ID, Path: record.Path, SHA256: record.SHA256, Technical: technical, CutTimestampsMS: cuts})
		}
		set.Profile = profile(set.Files)
		if options.SemanticEnabled {
			if report != nil {
				report(total, total, "Analyzing semantic style for reference set "+set.Name)
			}
			semantic, diagnostic := semanticStyle(ctx, index.ProjectRoot, set, options, deps)
			set.Semantic = semantic
			if diagnostic != nil {
				set.Warnings = append(set.Warnings, *diagnostic)
				result.Warnings = append(result.Warnings, *diagnostic)
			}
		}
		result.ReferenceSets = append(result.ReferenceSets, set)
	}
	return result, nil
}

func semanticStyle(ctx context.Context, root string, set ReferenceSet, options Options, deps Dependencies) (SemanticStyle, *ingest.Diagnostic) {
	if deps.SemanticAnalyzer == nil {
		diagnostic := warning("reference_sets."+set.Name+".semantic", "semantic style analysis was not configured", "")
		return SemanticStyle{Status: "skipped"}, &diagnostic
	}
	frames, err := sampleSemanticFrames(root, set, options, deps.FrameExtractor)
	if err != nil {
		diagnostic := warning("reference_sets."+set.Name+".semantic", "could not extract style frames: "+err.Error(), "")
		return SemanticStyle{Status: "failed"}, &diagnostic
	}
	framePaths := make([]string, 0, len(frames))
	for _, frame := range frames {
		framePaths = append(framePaths, artifact.ResolvePath(root, frame.Path))
	}
	semantic, err := deps.SemanticAnalyzer.AnalyzeStyle(ctx, SemanticRequest{ReferenceSet: set.Name, Technical: set.Profile, Frames: frames, FramePaths: framePaths})
	if err != nil {
		diagnostic := warning("reference_sets."+set.Name+".semantic", "semantic style analysis failed: "+err.Error(), "")
		return SemanticStyle{Status: "failed", SampledFrames: frames}, &diagnostic
	}
	semantic.Status = "complete"
	semantic.SampledFrames = frames
	semantic.SupportingFrameIDs = validFrameIDs(semantic.SupportingFrameIDs, frames)
	return semantic, nil
}

func sampleSemanticFrames(root string, set ReferenceSet, options Options, extractor analyzeassets.FrameExtractor) ([]SemanticFrame, error) {
	maxFrames := options.SemanticMaxFrames
	if maxFrames <= 0 {
		maxFrames = 12
	}
	thumbnailDir := options.ThumbnailDir
	if thumbnailDir == "" {
		thumbnailDir = filepath.Join(root, ".auto-editor", "style-thumbnails")
	}
	candidates := make([][]semanticSample, len(set.Files))
	for index, file := range set.Files {
		candidates[index] = styleSamples(file)
	}
	frames := make([]SemanticFrame, 0, maxFrames)
	for sampleIndex := 0; len(frames) < maxFrames; sampleIndex++ {
		added := false
		for fileIndex, file := range set.Files {
			if sampleIndex >= len(candidates[fileIndex]) || len(frames) == maxFrames {
				continue
			}
			sample := candidates[fileIndex][sampleIndex]
			outputPath := filepath.Join(thumbnailDir, file.FileID+"-"+sample.Position+".jpg")
			if err := extractor.Extract(artifact.ResolvePath(root, file.Path), outputPath, options.FFmpeg, sample.TimestampMS); err != nil {
				return nil, err
			}
			frames = append(frames, SemanticFrame{ID: file.FileID + "/" + sample.Position, Path: artifact.DisplayPath(root, outputPath), TimestampMS: sample.TimestampMS})
			added = true
		}
		if !added {
			break
		}
	}
	if len(frames) == 0 {
		return nil, fmt.Errorf("reference set has no sampleable videos")
	}
	return frames, nil
}

type semanticSample struct {
	Position    string
	TimestampMS int64
}

func styleSamples(file FileEvidence) []semanticSample {
	duration := file.Technical.DurationMS
	samples := []semanticSample{{"start", duration / 10}, {"middle", duration / 2}, {"end", duration * 9 / 10}}
	if len(file.CutTimestampsMS) > 0 {
		samples = append(samples, semanticSample{Position: "cut", TimestampMS: file.CutTimestampsMS[len(file.CutTimestampsMS)/2]})
	}
	return samples
}

func validFrameIDs(ids []string, frames []SemanticFrame) []string {
	available := make(map[string]bool, len(frames))
	for _, frame := range frames {
		available[frame.ID] = true
	}
	valid := make([]string, 0, len(ids))
	for _, id := range ids {
		if available[id] {
			valid = append(valid, id)
		}
	}
	return valid
}

func profile(files []FileEvidence) TechnicalStyle {
	style := TechnicalStyle{VideosAnalyzed: len(files)}
	ratioCounts := map[string]int{}
	durations := make([]int64, 0, len(files))
	withAudio := 0
	shotDurations := []int64{}
	for _, file := range files {
		technical := file.Technical
		style.TotalDurationMS += technical.DurationMS
		durations = append(durations, technical.DurationMS)
		if technical.HasAudio {
			withAudio++
		}
		if technical.Width > 0 && technical.Height > 0 {
			ratioCounts[ratio(technical.Width, technical.Height)]++
		}
		style.CutCount += len(file.CutTimestampsMS)
		previous := int64(0)
		for _, cut := range file.CutTimestampsMS {
			if cut > previous {
				shotDurations = append(shotDurations, cut-previous)
				previous = cut
			}
		}
		if technical.DurationMS > previous {
			shotDurations = append(shotDurations, technical.DurationMS-previous)
		}
	}
	if len(files) > 0 {
		style.AudioVideoRatio = float64(withAudio) / float64(len(files))
	}
	if len(durations) > 0 {
		slices.Sort(durations)
		style.MedianDurationMS = durations[len(durations)/2]
	}
	if style.TotalDurationMS > 0 {
		style.CutsPerMinute = float64(style.CutCount) * 60000 / float64(style.TotalDurationMS)
	}
	if len(shotDurations) > 0 {
		slices.Sort(shotDurations)
		style.MedianShotDurationMS = shotDurations[len(shotDurations)/2]
	}
	for value, count := range ratioCounts {
		style.AspectRatios = append(style.AspectRatios, AspectRatioStat{Ratio: value, Count: count})
	}
	slices.SortFunc(style.AspectRatios, func(a, b AspectRatioStat) int { return strings.Compare(a.Ratio, b.Ratio) })
	return style
}

func Write(path string, result Result) error {
	return artifact.Write(path, result)
}
func warning(field, message, path string) ingest.Diagnostic {
	return ingest.Diagnostic{Level: "warning", Field: field, Message: message, Path: path}
}
func ratio(width, height int) string {
	divisor := gcd(width, height)
	return fmt.Sprintf("%d:%d", width/divisor, height/divisor)
}
func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}
