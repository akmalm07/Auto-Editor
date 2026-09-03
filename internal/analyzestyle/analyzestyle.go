package analyzestyle

import (
	"auto-editor/internal/analyzeassets"
	"auto-editor/internal/ingest"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	DefaultStylePath = ".auto-editor/style.json"
	Version          = 1
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

type SceneDetector interface {
	Detect(path, executable string) ([]int64, error)
}
type Dependencies struct {
	Inspector     analyzeassets.Inspector
	SceneDetector SceneDetector
}
type Options struct{ IndexPath, FFprobe, FFmpeg string }
type StageReporter func(current, total int, message string)

func Build(ctx context.Context, options Options, deps Dependencies, report StageReporter) (Result, error) {
	_ = ctx
	if deps.Inspector == nil {
		deps.Inspector = analyzeassets.SystemInspector{}
	}
	if deps.SceneDetector == nil {
		deps.SceneDetector = SystemSceneDetector{}
	}
	index, err := ingest.ReadIndex(options.IndexPath)
	if err != nil {
		return Result{}, err
	}
	result := Result{Version: Version, ProjectName: index.ProjectName, IndexPath: displayPath(index.ProjectRoot, options.IndexPath), GeneratedAt: time.Now().UTC()}
	total := 0
	for _, set := range index.ReferenceSets {
		total += len(set.Files)
	}
	if total == 0 {
		return Result{}, fmt.Errorf("ingest index contains no reference videos")
	}
	current := 0
	for _, indexedSet := range index.ReferenceSets {
		set := ReferenceSet{Name: indexedSet.Name}
		for _, record := range indexedSet.Files {
			current++
			if report != nil {
				report(current, total, fmt.Sprintf("Analyzing reference %d/%d: %s", current, total, record.Path))
			}
			path := resolvePath(index.ProjectRoot, record.Path)
			if err := verifyRecord(path, record); err != nil {
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
		result.ReferenceSets = append(result.ReferenceSets, set)
	}
	return result, nil
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
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
func verifyRecord(path string, record ingest.FileRecord) error {
	hash, err := ingest.FileSHA256(path)
	if err != nil {
		return err
	}
	if hash != record.SHA256 {
		return fmt.Errorf("file content hash no longer matches index")
	}
	return nil
}
func resolvePath(root, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(root, filepath.FromSlash(path))
}
func displayPath(root, path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return filepath.ToSlash(filepath.Clean(path))
	}
	relative, err := filepath.Rel(root, absolute)
	if err == nil && !strings.HasPrefix(relative, "..") {
		return filepath.ToSlash(relative)
	}
	return filepath.ToSlash(absolute)
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
