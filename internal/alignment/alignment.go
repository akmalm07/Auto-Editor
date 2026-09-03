package alignment

import (
	"auto-editor/internal/asr"
	"auto-editor/internal/audio"
	"auto-editor/internal/ingest"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

const (
	DefaultAlignmentPath = ".auto-editor/alignment.json"
	Version              = 1
	maxAlignmentCells    = 4_000_000
)

type Result struct {
	Version     int                 `json:"version"`
	ProjectName string              `json:"project_name"`
	IndexPath   string              `json:"index_path"`
	GeneratedAt time.Time           `json:"generated_at"`
	Script      Script              `json:"script"`
	Voiceover   Voiceover           `json:"voiceover"`
	Transcript  asr.Transcript      `json:"transcript"`
	Metrics     Metrics             `json:"metrics"`
	Segments    []Segment           `json:"segments"`
	Warnings    []ingest.Diagnostic `json:"warnings,omitempty"`
}

type Script struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	Text      string `json:"text"`
	WordCount int    `json:"word_count"`
}

type Voiceover struct {
	ID     string      `json:"id"`
	Path   string      `json:"path"`
	SHA256 string      `json:"sha256"`
	Audio  audio.Probe `json:"audio"`
}

type Metrics struct {
	ScriptWords     int     `json:"script_words"`
	TranscriptWords int     `json:"transcript_words"`
	MatchedWords    int     `json:"matched_words"`
	Substitutions   int     `json:"substitutions"`
	MissingWords    int     `json:"missing_words"`
	ExtraWords      int     `json:"extra_words"`
	Similarity      float64 `json:"similarity"`
	WordsPerMinute  float64 `json:"words_per_minute"`
}

type Segment struct {
	Index          int     `json:"index"`
	Text           string  `json:"text"`
	ScriptWordFrom int     `json:"script_word_from"`
	ScriptWordTo   int     `json:"script_word_to"`
	StartMS        int64   `json:"start_ms"`
	EndMS          int64   `json:"end_ms"`
	Confidence     float64 `json:"confidence"`
}

type Dependencies struct {
	Transcriber asr.Transcriber
	Probe       func(path, executable string) (audio.Probe, error)
}

type Options struct {
	IndexPath     string
	OutputPath    string
	FFprobe       string
	MinSimilarity float64
	Language      string
}

type StageReporter func(current, total int, message string)

func Build(ctx context.Context, options Options, dependencies Dependencies, report StageReporter) (Result, error) {
	if dependencies.Transcriber == nil {
		return Result{}, fmt.Errorf("alignment transcriber is required")
	}
	if dependencies.Probe == nil {
		dependencies.Probe = audio.ProbeFile
	}
	stage := func(current int, message string) {
		if report != nil {
			report(current, 6, message)
		}
	}

	stage(1, "Loading ingest index")
	index, err := ingest.ReadIndex(options.IndexPath)
	if err != nil {
		return Result{}, err
	}
	if index.Script == nil || index.Voiceover == nil {
		return Result{}, fmt.Errorf("ingest index must contain a script and voiceover")
	}

	scriptPath := resolvePath(index.ProjectRoot, index.Script.Path)
	voiceoverPath := resolvePath(index.ProjectRoot, index.Voiceover.Path)
	stage(2, "Checking indexed inputs")
	if err := verifyRecord(scriptPath, *index.Script); err != nil {
		return Result{}, fmt.Errorf("script changed since ingest: %w; run ingest again", err)
	}
	if err := verifyRecord(voiceoverPath, *index.Voiceover); err != nil {
		return Result{}, fmt.Errorf("voiceover changed since ingest: %w; run ingest again", err)
	}
	scriptBytes, err := os.ReadFile(scriptPath)
	if err != nil {
		return Result{}, err
	}
	scriptTokens := tokenize(string(scriptBytes))
	if len(scriptTokens) == 0 {
		return Result{}, fmt.Errorf("script contains no alignable words")
	}

	stage(3, "Probing voiceover audio")
	probe, err := dependencies.Probe(voiceoverPath, options.FFprobe)
	if err != nil {
		return Result{}, err
	}

	stage(4, "Transcribing voiceover")
	language := options.Language
	if language == "" {
		language = index.Language
	}
	transcript, err := dependencies.Transcriber.Transcribe(ctx, voiceoverPath, language)
	if err != nil {
		return Result{}, err
	}
	transcriptTokens, err := transcriptTokens(transcript)
	if err != nil {
		return Result{}, err
	}

	stage(5, "Comparing script to transcript")
	operations, metrics, err := compare(scriptTokens, transcriptTokens, probe.DurationMS)
	if err != nil {
		return Result{}, err
	}
	segments := buildSegments(scriptTokens, transcriptTokens, operations)

	stage(6, "Building alignment artifact")
	result := Result{
		Version:     Version,
		ProjectName: index.ProjectName,
		IndexPath:   displayPath(index.ProjectRoot, options.IndexPath),
		GeneratedAt: time.Now().UTC(),
		Script:      Script{ID: index.Script.ID, Path: index.Script.Path, SHA256: index.Script.SHA256, Text: string(scriptBytes), WordCount: len(scriptTokens)},
		Voiceover:   Voiceover{ID: index.Voiceover.ID, Path: index.Voiceover.Path, SHA256: index.Voiceover.SHA256, Audio: probe},
		Transcript:  transcript,
		Metrics:     metrics,
		Segments:    segments,
	}
	if metrics.Similarity < options.MinSimilarity {
		result.Warnings = append(result.Warnings, warning("alignment", fmt.Sprintf("script/transcript similarity %.2f is below configured threshold %.2f", metrics.Similarity, options.MinSimilarity)))
	}
	if metrics.MissingWords > 0 || metrics.ExtraWords > 0 || metrics.Substitutions > 0 {
		result.Warnings = append(result.Warnings, warning("alignment", fmt.Sprintf("comparison found %d missing, %d extra, and %d substituted word(s)", metrics.MissingWords, metrics.ExtraWords, metrics.Substitutions)))
	}
	if transcriptTokens[len(transcriptTokens)-1].EndMS > probe.DurationMS+1000 {
		result.Warnings = append(result.Warnings, warning("transcript.words", "final word timestamp exceeds the probed voiceover duration by more than one second"))
	}
	return result, nil
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

type token struct {
	Raw     string
	Normal  string
	StartMS int64
	EndMS   int64
}

func tokenize(text string) []token {
	var tokens []token
	var word []rune
	flush := func() {
		if len(word) == 0 {
			return
		}
		raw := string(word)
		tokens = append(tokens, token{Raw: raw, Normal: strings.ToLower(raw)})
		word = nil
	}
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || (r == '\'' && len(word) > 0) {
			word = append(word, r)
			continue
		}
		flush()
	}
	flush()
	return tokens
}

func transcriptTokens(transcript asr.Transcript) ([]token, error) {
	if strings.TrimSpace(transcript.Text) == "" {
		return nil, fmt.Errorf("transcription is empty")
	}
	if len(transcript.Words) == 0 {
		return nil, fmt.Errorf("transcription has no word timestamps")
	}
	var tokens []token
	var previousEnd int64
	for _, word := range transcript.Words {
		words := tokenize(word.Text)
		if len(words) == 0 {
			continue
		}
		if word.StartMS < previousEnd || word.EndMS <= word.StartMS {
			return nil, fmt.Errorf("transcription has invalid or non-monotonic word timestamps")
		}
		span := word.EndMS - word.StartMS
		for index := range words {
			words[index].StartMS = word.StartMS + span*int64(index)/int64(len(words))
			words[index].EndMS = word.StartMS + span*int64(index+1)/int64(len(words))
			tokens = append(tokens, words[index])
		}
		previousEnd = word.EndMS
	}
	if len(tokens) == 0 {
		return nil, fmt.Errorf("transcription contains no alignable words")
	}
	return tokens, nil
}

type operation struct {
	Kind        byte
	ScriptIndex int
	ASRIndex    int
}

func compare(script, transcript []token, durationMS int64) ([]operation, Metrics, error) {
	if (len(script)+1)*(len(transcript)+1) > maxAlignmentCells {
		return nil, Metrics{}, fmt.Errorf("script and transcript are too large for exact alignment; split the voiceover into smaller sections")
	}
	width := len(transcript) + 1
	dp := make([]int, (len(script)+1)*width)
	cell := func(i, j int) *int { return &dp[i*width+j] }
	for i := 1; i <= len(script); i++ {
		*cell(i, 0) = i
	}
	for j := 1; j <= len(transcript); j++ {
		*cell(0, j) = j
	}
	for i := 1; i <= len(script); i++ {
		for j := 1; j <= len(transcript); j++ {
			cost := 1
			if script[i-1].Normal == transcript[j-1].Normal {
				cost = 0
			}
			diagonal := *cell(i-1, j-1) + cost
			deletion := *cell(i-1, j) + 1
			insertion := *cell(i, j-1) + 1
			*cell(i, j) = min(diagonal, min(deletion, insertion))
		}
	}

	var operations []operation
	metrics := Metrics{ScriptWords: len(script), TranscriptWords: len(transcript)}
	for i, j := len(script), len(transcript); i > 0 || j > 0; {
		if i > 0 && j > 0 {
			cost := 1
			kind := byte('s')
			if script[i-1].Normal == transcript[j-1].Normal {
				cost, kind = 0, 'm'
			}
			if *cell(i, j) == *cell(i-1, j-1)+cost {
				operations = append(operations, operation{Kind: kind, ScriptIndex: i - 1, ASRIndex: j - 1})
				if kind == 'm' {
					metrics.MatchedWords++
				} else {
					metrics.Substitutions++
				}
				i--
				j--
				continue
			}
		}
		if i > 0 && *cell(i, j) == *cell(i-1, j)+1 {
			operations = append(operations, operation{Kind: 'd', ScriptIndex: i - 1, ASRIndex: -1})
			metrics.MissingWords++
			i--
			continue
		}
		operations = append(operations, operation{Kind: 'i', ScriptIndex: -1, ASRIndex: j - 1})
		metrics.ExtraWords++
		j--
	}
	for left, right := 0, len(operations)-1; left < right; left, right = left+1, right-1 {
		operations[left], operations[right] = operations[right], operations[left]
	}
	denominator := max(len(script), len(transcript))
	if denominator > 0 {
		metrics.Similarity = float64(metrics.MatchedWords) / float64(denominator)
	}
	if durationMS > 0 {
		metrics.WordsPerMinute = float64(len(transcript)) * 60000 / float64(durationMS)
	}
	return operations, metrics, nil
}

func buildSegments(script, transcript []token, operations []operation) []Segment {
	var segments []Segment
	for _, op := range operations {
		if op.Kind != 'm' {
			continue
		}
		word := script[op.ScriptIndex]
		spoken := transcript[op.ASRIndex]
		if len(segments) == 0 || op.ScriptIndex != segments[len(segments)-1].ScriptWordTo+1 || spoken.StartMS-segments[len(segments)-1].EndMS > 2000 {
			segments = append(segments, Segment{Index: len(segments), Text: word.Raw, ScriptWordFrom: op.ScriptIndex, ScriptWordTo: op.ScriptIndex, StartMS: spoken.StartMS, EndMS: spoken.EndMS, Confidence: 1})
			continue
		}
		segment := &segments[len(segments)-1]
		segment.Text += " " + word.Raw
		segment.ScriptWordTo = op.ScriptIndex
		segment.EndMS = spoken.EndMS
	}
	return segments
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

func warning(field, message string) ingest.Diagnostic {
	return ingest.Diagnostic{Level: "warning", Field: field, Message: message}
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
