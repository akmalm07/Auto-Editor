package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"auto-editor/internal/project"
)

const (
	DefaultIndexPath = ".auto-editor/index.json"
	IndexVersion     = 2
)

type Index struct {
	Version       int                 `json:"version"`
	ProjectName   string              `json:"project_name"`
	Language      string              `json:"language,omitempty"`
	ManifestPath  string              `json:"manifest_path"`
	ProjectRoot   string              `json:"project_root"`
	GeneratedAt   time.Time           `json:"generated_at"`
	Script        *FileRecord         `json:"script,omitempty"`
	Voiceover     *FileRecord         `json:"voiceover,omitempty"`
	Assets        []FileRecord        `json:"assets,omitempty"`
	ReferenceSets []ReferenceSetIndex `json:"reference_sets,omitempty"`
	Warnings      []Diagnostic        `json:"warnings,omitempty"`
}

type ReferenceSetIndex struct {
	Name  string       `json:"name"`
	Files []FileRecord `json:"files"`
}

type FileRecord struct {
	ID        string `json:"id"`
	Source    string `json:"source"`
	Path      string `json:"path"`
	Kind      string `json:"kind"`
	Extension string `json:"extension"`
	SizeBytes int64  `json:"size_bytes"`
	SHA256    string `json:"sha256"`
}

type Diagnostic struct {
	Level   string `json:"level"`
	Field   string `json:"field"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
}

type ValidationError struct {
	Diagnostics []Diagnostic
}

type StageReporter func(current, total int, message string)

func (e ValidationError) Error() string {
	return fmt.Sprintf("ingest failed with %d error(s)", len(e.Diagnostics))
}

func BuildIndex(manifestPath string) (Index, error) {
	return BuildIndexWithProgress(manifestPath, nil)
}

func BuildIndexWithProgress(manifestPath string, report StageReporter) (Index, error) {
	stage := func(current int, message string) {
		if report != nil {
			report(current, 6, message)
		}
	}

	stage(1, "Loading manifest")
	absoluteManifestPath, err := filepath.Abs(manifestPath)
	if err != nil {
		return Index{}, err
	}

	manifest, err := project.ReadManifest(absoluteManifestPath)
	if err != nil {
		return Index{}, err
	}

	root := filepath.Dir(absoluteManifestPath)
	builder := indexBuilder{
		root:     root,
		manifest: manifest,
		index: Index{
			Version:      IndexVersion,
			ProjectName:  manifest.Name,
			Language:     manifest.Language,
			ManifestPath: displayPath(root, absoluteManifestPath),
			ProjectRoot:  cleanPath(root),
			GeneratedAt:  time.Now().UTC(),
		},
	}

	stage(2, "Validating required inputs")
	builder.validateManifest()
	stage(3, "Indexing script and voiceover")
	builder.indexScript()
	builder.indexVoiceover()
	stage(4, "Expanding asset paths")
	builder.indexAssets()
	stage(5, "Expanding reference paths")
	builder.indexReferences()
	stage(6, "Checking duplicate content")
	builder.addDuplicateWarnings()

	if len(builder.errors) > 0 {
		return Index{}, ValidationError{Diagnostics: builder.errors}
	}

	builder.index.Warnings = builder.warnings
	return builder.index, nil
}

func WriteIndex(path string, index Index) error {
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func ReadIndex(path string) (Index, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Index{}, err
	}

	var index Index
	if err := json.Unmarshal(data, &index); err != nil {
		return Index{}, err
	}
	if index.Version != IndexVersion {
		return Index{}, fmt.Errorf("unsupported index version %d", index.Version)
	}
	return index, nil
}

type indexBuilder struct {
	root     string
	manifest project.Manifest
	index    Index
	errors   []Diagnostic
	warnings []Diagnostic
	records  []FileRecord
}

func (b *indexBuilder) validateManifest() {
	if strings.TrimSpace(b.manifest.Name) == "" {
		b.addError("name", "project name is required", "")
	}
	if strings.TrimSpace(b.manifest.Inputs.Script) == "" {
		b.addError("inputs.script", "script path is required", "")
	}
	if strings.TrimSpace(b.manifest.Inputs.Voiceover) == "" {
		b.addError("inputs.voiceover", "voiceover path is required", "")
	}
	if len(b.manifest.Inputs.Assets) == 0 {
		b.addError("inputs.assets", "at least one asset path is required", "")
	}
}

func (b *indexBuilder) indexScript() {
	path := strings.TrimSpace(b.manifest.Inputs.Script)
	if path == "" {
		return
	}

	record, err := b.indexSingle("inputs.script", "script", path, []string{KindText, KindSubtitle})
	if err != nil {
		b.addError("inputs.script", err.Error(), path)
		return
	}

	if err := validateUTF8(resolvePath(b.root, path)); err != nil {
		b.addError("inputs.script", err.Error(), path)
		return
	}

	b.index.Script = &record
	b.records = append(b.records, record)
}

func (b *indexBuilder) indexVoiceover() {
	path := strings.TrimSpace(b.manifest.Inputs.Voiceover)
	if path == "" {
		return
	}

	record, err := b.indexSingle("inputs.voiceover", "voiceover", path, []string{KindAudio})
	if err != nil {
		b.addError("inputs.voiceover", err.Error(), path)
		return
	}

	b.index.Voiceover = &record
	b.records = append(b.records, record)
}

func (b *indexBuilder) indexAssets() {
	records := b.indexMany("inputs.assets", "asset", b.manifest.Inputs.Assets, []string{KindAudio, KindImage, KindVideo, KindText, KindSubtitle})
	b.index.Assets = records
	b.records = append(b.records, records...)
}

func (b *indexBuilder) indexReferences() {
	for setIndex, set := range b.manifest.Inputs.ReferenceSets {
		field := fmt.Sprintf("inputs.reference_sets[%d]", setIndex)
		name := strings.TrimSpace(set.Name)
		if name == "" {
			b.addError(field+".name", "reference set name is required", "")
			continue
		}
		if len(set.Paths) == 0 {
			b.addError(field+".paths", "reference set must include at least one path", "")
			continue
		}

		records := b.indexMany(field+".paths", "reference", set.Paths, []string{KindVideo})
		b.index.ReferenceSets = append(b.index.ReferenceSets, ReferenceSetIndex{
			Name:  name,
			Files: records,
		})
		b.records = append(b.records, records...)
	}
}

func (b *indexBuilder) indexSingle(field, source, inputPath string, allowedKinds []string) (FileRecord, error) {
	absolutePath := resolvePath(b.root, inputPath)
	info, err := os.Stat(absolutePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return FileRecord{}, fmt.Errorf("path does not exist")
		}
		return FileRecord{}, err
	}
	if info.IsDir() {
		return FileRecord{}, fmt.Errorf("expected a file, got a directory")
	}

	record, err := buildRecord(b.root, source, absolutePath, info)
	if err != nil {
		return FileRecord{}, err
	}
	if !slices.Contains(allowedKinds, record.Kind) {
		return FileRecord{}, fmt.Errorf("unsupported %s file type %q", source, record.Extension)
	}

	return record, nil
}

func (b *indexBuilder) indexMany(field, source string, paths []string, allowedKinds []string) []FileRecord {
	var records []FileRecord

	for pathIndex, inputPath := range paths {
		inputPath = strings.TrimSpace(inputPath)
		itemField := fmt.Sprintf("%s[%d]", field, pathIndex)
		if inputPath == "" {
			b.addError(itemField, "path cannot be empty", "")
			continue
		}

		absolutePath := resolvePath(b.root, inputPath)
		info, err := os.Stat(absolutePath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				b.addError(itemField, "path does not exist", inputPath)
			} else {
				b.addError(itemField, err.Error(), inputPath)
			}
			continue
		}

		if !info.IsDir() {
			record, err := buildRecord(b.root, source, absolutePath, info)
			if err != nil {
				b.addError(itemField, err.Error(), inputPath)
				continue
			}
			if !slices.Contains(allowedKinds, record.Kind) {
				b.addError(itemField, fmt.Sprintf("unsupported %s file type %q", source, record.Extension), inputPath)
				continue
			}

			records = append(records, record)
			continue
		}

		before := len(records)
		if err := b.indexDirectory(itemField, source, absolutePath, allowedKinds, &records); err != nil {
			b.addError(itemField, err.Error(), inputPath)
			continue
		}
		if len(records) == before {
			b.addError(itemField, "directory contains no supported files", inputPath)
		}
	}

	return records
}

func (b *indexBuilder) indexDirectory(field, source, dir string, allowedKinds []string, records *[]FileRecord) error {
	return filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			b.addWarning(field, err.Error(), displayPath(b.root, path))
			return nil
		}

		record, err := buildRecord(b.root, source, path, info)
		if err != nil {
			b.addWarning(field, err.Error(), displayPath(b.root, path))
			return nil
		}
		if !slices.Contains(allowedKinds, record.Kind) {
			b.addWarning(field, fmt.Sprintf("skipping unsupported file type %q", record.Extension), record.Path)
			return nil
		}

		*records = append(*records, record)
		return nil
	})
}

func (b *indexBuilder) addDuplicateWarnings() {
	byHash := make(map[string][]string)
	for _, record := range b.records {
		byHash[record.SHA256] = append(byHash[record.SHA256], record.Path)
	}

	for _, paths := range byHash {
		if len(paths) < 2 {
			continue
		}
		b.addWarning("files", "duplicate file content detected: "+strings.Join(paths, ", "), "")
	}
}

func (b *indexBuilder) addError(field, message, path string) {
	b.errors = append(b.errors, Diagnostic{
		Level:   "error",
		Field:   field,
		Message: message,
		Path:    cleanPath(path),
	})
}

func (b *indexBuilder) addWarning(field, message, path string) {
	b.warnings = append(b.warnings, Diagnostic{
		Level:   "warning",
		Field:   field,
		Message: message,
		Path:    cleanPath(path),
	})
}

const (
	KindAudio    = "audio"
	KindImage    = "image"
	KindSubtitle = "subtitle"
	KindText     = "text"
	KindVideo    = "video"
	KindUnknown  = "unknown"
)

var extensionKinds = map[string]string{
	".aac":      KindAudio,
	".aiff":     KindAudio,
	".flac":     KindAudio,
	".m4a":      KindAudio,
	".mp3":      KindAudio,
	".ogg":      KindAudio,
	".opus":     KindAudio,
	".wav":      KindAudio,
	".webm":     KindVideo,
	".avi":      KindVideo,
	".m4v":      KindVideo,
	".mkv":      KindVideo,
	".mov":      KindVideo,
	".mp4":      KindVideo,
	".bmp":      KindImage,
	".gif":      KindImage,
	".jpeg":     KindImage,
	".jpg":      KindImage,
	".png":      KindImage,
	".tif":      KindImage,
	".tiff":     KindImage,
	".webp":     KindImage,
	".md":       KindText,
	".markdown": KindText,
	".txt":      KindText,
	".ass":      KindSubtitle,
	".srt":      KindSubtitle,
	".vtt":      KindSubtitle,
}

func buildRecord(root, source, absolutePath string, info os.FileInfo) (FileRecord, error) {
	if info.Size() == 0 {
		return FileRecord{}, fmt.Errorf("file is empty")
	}

	extension := strings.ToLower(filepath.Ext(absolutePath))
	kind := extensionKinds[extension]
	if kind == "" {
		kind = KindUnknown
	}

	hash, err := FileSHA256(absolutePath)
	if err != nil {
		return FileRecord{}, err
	}

	path := displayPath(root, absolutePath)
	return FileRecord{
		ID:        makeID(source, path, hash),
		Source:    source,
		Path:      path,
		Kind:      kind,
		Extension: extension,
		SizeBytes: info.Size(),
		SHA256:    hash,
	}, nil
}

func FileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func validateUTF8(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !utf8.Valid(data) {
		return fmt.Errorf("script is not valid UTF-8 text")
	}
	return nil
}

func makeID(source, path, hash string) string {
	sum := sha256.Sum256([]byte(path + "\x00" + hash))
	return source + "_" + hex.EncodeToString(sum[:])[:16]
}

func resolvePath(root, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(root, path))
}

func displayPath(root, path string) string {
	relative, err := filepath.Rel(root, path)
	if err == nil && !strings.HasPrefix(relative, "..") && !filepath.IsAbs(relative) {
		return cleanPath(relative)
	}
	return cleanPath(path)
}

func cleanPath(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(path))
}
