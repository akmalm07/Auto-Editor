// Package projectexport writes portable editor-interchange projects.
package projectexport

import (
	"auto-editor/internal/analyzeassets"
	"auto-editor/internal/artifact"
	"auto-editor/internal/ingest"
	"auto-editor/internal/plan"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Options struct {
	PlanPath   string
	OutputDir  string
	CopyAssets bool
	Format     string
	Width      int
	Height     int
	FPS        int
}

type Result struct {
	OutputDir string
	XMLPath   string
	Copied    int
}

type manifest struct {
	Version   int       `json:"version"`
	Generated time.Time `json:"generated_at"`
	Plan      string    `json:"plan"`
	Format    string    `json:"format"`
	Sources   []source  `json:"sources"`
}

type source struct {
	AssetID string `json:"asset_id"`
	Path    string `json:"path"`
	Kind    string `json:"kind"`
	SHA256  string `json:"sha256"`
}

type assetRef struct {
	ID, Path, Kind, Hash string
}

func Export(options Options) (Result, error) {
	if options.PlanPath == "" || options.OutputDir == "" {
		return Result{}, fmt.Errorf("plan and output directory are required")
	}
	if options.FPS <= 0 {
		options.FPS = 30
	}
	format := options.Format
	if format == "" || format == "xml" {
		format = "fcpxml"
	}
	if format != "fcpxml" && format != "kdenlive" {
		return Result{}, fmt.Errorf("unsupported export format %q", format)
	}
	if options.Width <= 0 {
		options.Width = 1920
	}
	if options.Height <= 0 {
		options.Height = 1080
	}
	planPath, err := filepath.Abs(options.PlanPath)
	if err != nil {
		return Result{}, err
	}
	p, err := artifact.Read[plan.Result](planPath)
	if err != nil {
		return Result{}, fmt.Errorf("read plan: %w", err)
	}
	if p.Version != plan.Version {
		return Result{}, fmt.Errorf("unsupported plan version %d; run plan again", p.Version)
	}
	root := filepath.Dir(planPath)
	if filepath.Base(root) == ".auto-editor" {
		root = filepath.Dir(root)
	}
	assets, err := artifact.Read[analyzeassets.Result](artifact.ResolvePath(root, p.Sources.Assets))
	if err != nil {
		return Result{}, fmt.Errorf("read asset artifact: %w", err)
	}
	byID := make(map[string]analyzeassets.Asset, len(assets.Assets))
	for _, a := range assets.Assets {
		byID[a.AssetID] = a
	}
	refs := make([]assetRef, 0)
	seen := make(map[string]bool)
	add := func(id, path, kind, hash string) error {
		if id == "" || path == "" || hash == "" || seen[id] {
			return nil
		}
		absolute := artifact.ResolvePath(root, path)
		if err := verifyHash(absolute, hash); err != nil {
			return fmt.Errorf("source %s: %w; run ingest, analyze-assets, and plan again", path, err)
		}
		seen[id] = true
		refs = append(refs, assetRef{ID: id, Path: absolute, Kind: kind, Hash: hash})
		return nil
	}
	if err := add(p.Voiceover.AssetID, p.Voiceover.Path, "audio", p.Voiceover.SHA256); err != nil {
		return Result{}, err
	}
	for _, v := range p.Visuals {
		a, ok := byID[v.AssetID]
		if !ok {
			return Result{}, fmt.Errorf("visual %s references unknown asset %s", v.ID, v.AssetID)
		}
		if err := add(a.AssetID, a.Path, a.Kind, a.SHA256); err != nil {
			return Result{}, err
		}
	}
	for _, t := range p.AudioTracks {
		if err := add(t.AssetID, t.Path, "audio", t.SHA256); err != nil {
			return Result{}, err
		}
	}
	for _, c := range p.AudioCues {
		if err := add(c.AssetID, c.Path, "audio", c.SHA256); err != nil {
			return Result{}, err
		}
	}
	if len(refs) == 0 {
		return Result{}, fmt.Errorf("plan contains no exportable sources")
	}
	out, err := filepath.Abs(options.OutputDir)
	if err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(filepath.Join(out, "assets"), 0o755); err != nil {
		return Result{}, err
	}
	paths := make(map[string]string, len(refs))
	manifestSources := make([]source, 0, len(refs))
	copied := 0
	for _, ref := range refs {
		name := safeName(ref.ID) + filepath.Ext(ref.Path)
		if options.CopyAssets {
			target := filepath.Join(out, "assets", name)
			if err := copyFile(ref.Path, target); err != nil {
				return Result{}, err
			}
			paths[ref.ID] = target
			copied++
		} else {
			paths[ref.ID] = ref.Path
		}
		manifestPath := paths[ref.ID]
		if options.CopyAssets {
			manifestPath, _ = filepath.Rel(out, manifestPath)
			manifestPath = filepath.ToSlash(manifestPath)
		}
		manifestSources = append(manifestSources, source{AssetID: ref.ID, Path: manifestPath, Kind: ref.Kind, SHA256: ref.Hash})
	}
	filename, content := "auto-editor.xml", fcpxml(p, refs, paths, options)
	if format == "kdenlive" {
		filename, content = "auto-editor.kdenlive", kdenlive(p, refs, paths, options)
	}
	xmlPath := filepath.Join(out, filename)
	if err := os.WriteFile(xmlPath, []byte(content), 0o644); err != nil {
		return Result{}, err
	}
	manifestFormat := "fcpxml-1.9 (xml)"
	if format == "kdenlive" {
		manifestFormat = "kdenlive-mlt-xml"
	}
	m := manifest{Version: 1, Generated: time.Now().UTC(), Plan: artifact.DisplayPath(root, planPath), Format: manifestFormat, Sources: manifestSources}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(filepath.Join(out, "manifest.json"), append(data, '\n'), 0o644); err != nil {
		return Result{}, err
	}
	readme := "Import auto-editor.xml into your editor. If assets are offline, relink them to the assets directory.\n"
	if format == "kdenlive" {
		readme = "Open auto-editor.kdenlive in Kdenlive. If assets are offline, relink them to the assets directory.\n"
	}
	if !options.CopyAssets {
		if format == "kdenlive" {
			readme = "Open auto-editor.kdenlive in Kdenlive. This project links to the original source files.\n"
		} else {
			readme = "Import auto-editor.xml into your editor. This project links to the original source files.\n"
		}
	}
	if err := os.WriteFile(filepath.Join(out, "IMPORT.md"), []byte(readme), 0o644); err != nil {
		return Result{}, err
	}
	return Result{OutputDir: out, XMLPath: xmlPath, Copied: copied}, nil
}

func fcpxml(p plan.Result, refs []assetRef, paths map[string]string, options Options) string {
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<fcpxml version=\"1.9\">\n<resources>\n")
	fmt.Fprintf(&b, "<format id=\"r-format\" name=\"Auto-Editor %dx%d\" frameDuration=\"1/%ds\" width=\"%d\" height=\"%d\"/>\n", options.Width, options.Height, options.FPS, options.Width, options.Height)
	for i, ref := range refs {
		video, audio := 0, 1
		if ref.Kind == "image" || ref.Kind == "video" {
			video = 1
		}
		if ref.Kind == "image" {
			audio = 0
		}
		fmt.Fprintf(&b, "<asset id=\"r%d\" name=\"%s\" src=\"%s\" start=\"0s\" duration=\"%s\" hasVideo=\"%d\" hasAudio=\"%d\" format=\"r-format\"/>\n", i+1, esc(ref.ID), fileURI(paths[ref.ID]), mediaDuration(p, ref), video, audio)
	}
	b.WriteString("</resources>\n<library><event name=\"Auto-Editor\"><project name=\"" + esc(p.ProjectName) + "\"><sequence format=\"r-format\" duration=\"" + sec(p.Format.DurationMS) + "\" tcStart=\"0s\" tcFormat=\"NDF\"><spine>\n")
	resource := func(id string) string {
		for i, ref := range refs {
			if ref.ID == id {
				return fmt.Sprintf("r%d", i+1)
			}
		}
		return ""
	}
	for _, v := range p.Visuals {
		fmt.Fprintf(&b, "<asset-clip name=\"%s\" ref=\"%s\" offset=\"%s\" start=\"%s\" duration=\"%s\"/>\n", esc(v.ID), resource(v.AssetID), sec(v.StartMS), sec(v.SourceStartMS), sec(v.EndMS-v.StartMS))
	}
	fmt.Fprintf(&b, "<asset-clip name=\"voiceover\" ref=\"%s\" lane=\"1\" offset=\"0s\" start=\"0s\" duration=\"%s\"/>\n", resource(p.Voiceover.AssetID), sec(p.Format.DurationMS))
	for _, t := range p.AudioTracks {
		fmt.Fprintf(&b, "<asset-clip name=\"%s\" ref=\"%s\" lane=\"2\" offset=\"%s\" start=\"%s\" duration=\"%s\"/>\n", esc(t.AssetID), resource(t.AssetID), sec(t.StartMS), sec(t.SourceStartMS), sec(t.EndMS-t.StartMS))
	}
	for _, c := range p.AudioCues {
		fmt.Fprintf(&b, "<asset-clip name=\"%s\" ref=\"%s\" lane=\"3\" offset=\"%s\" start=\"%s\" duration=\"%s\"/>\n", esc(c.AssetID), resource(c.AssetID), sec(c.StartMS), sec(c.SourceStartMS), sec(c.DurationMS))
	}
	b.WriteString("</spine></sequence></project></event></library></fcpxml>\n")
	return b.String()
}

// kdenlive writes a Generation 5 (document version 1.1) Kdenlive project.
// Kdenlive requires more than a renderable MLT tractor: the project bin must
// be named main_bin, timeline tracks must be wrapped in tractors, and the last
// tractor must point to the active sequence.
func kdenlive(p plan.Result, refs []assetRef, paths map[string]string, options Options) string {
	var b strings.Builder
	fps := options.FPS
	frames := func(ms int64) int64 { return (ms*int64(fps) + 500) / 1000 }
	producer := make(map[string]string, len(refs))
	clipID := make(map[string]int, len(refs))
	projectDir, _ := filepath.Abs(options.OutputDir)
	sequenceUUID := kdenliveUUID(p.ProjectName + strconv.FormatInt(p.Format.DurationMS, 10))
	sequenceID := "{" + sequenceUUID + "}"
	sequenceHash := kdenliveHash(sequenceID)
	projectID := "tractor-project"
	durationFrames := maxInt64(frames(p.Format.DurationMS), 1)
	lastFrame := durationFrames - 1
	b.WriteString("<?xml version=\"1.0\" encoding=\"utf-8\"?>\n")
	fmt.Fprintf(&b, "<mlt LC_NUMERIC=\"C\" producer=\"main_bin\" version=\"7.37.0\" root=\"%s\">\n", esc(projectDir))
	fmt.Fprintf(&b, "<profile description=\"auto-editor %dx%d %dfps\" frame_rate_num=\"%d\" frame_rate_den=\"1\" width=\"%d\" height=\"%d\" progressive=\"1\" sample_aspect_num=\"1\" sample_aspect_den=\"1\" display_aspect_num=\"%d\" display_aspect_den=\"%d\" colorspace=\"709\"/>\n", options.Width, options.Height, fps, fps, options.Width, options.Height, options.Width, options.Height)
	for i, ref := range refs {
		id := fmt.Sprintf("producer%d", i)
		producer[ref.ID] = id
		clipID[ref.ID] = i + 1
		service := "avformat"
		clipType := 3 // AV clips keep both streams available in the bin.
		if ref.Kind == "image" {
			service = "qimage"
			clipType = 5
		} else if ref.Kind == "audio" {
			clipType = 1
		}
		fmt.Fprintf(&b, "<producer id=\"%s\" in=\"0\" out=\"%d\"><property name=\"length\">%d</property><property name=\"eof\">pause</property><property name=\"mlt_service\">%s</property><property name=\"resource\">%s</property><property name=\"kdenlive:clipname\">%s</property><property name=\"kdenlive:id\">%d</property><property name=\"kdenlive:clip_type\">%d</property><property name=\"kdenlive:folderid\">-1</property><property name=\"kdenlive:originalurl\">%s</property><property name=\"kdenlive:proxy\">-</property></producer>\n", id, lastFrame, durationFrames, service, esc(paths[ref.ID]), esc(ref.ID), i+1, clipType, esc(paths[ref.ID]))
	}
	fmt.Fprintf(&b, "<producer id=\"black_track\" in=\"0\" out=\"%d\"><property name=\"length\">2147483647</property><property name=\"eof\">continue</property><property name=\"resource\">black</property><property name=\"mlt_service\">color</property><property name=\"mlt_image_format\">rgba</property></producer>\n", lastFrame)
	// A playlist is one Kdenlive timeline track. Entries use source in/out;
	// blank elements position them on the timeline.
	playlist := func(id string, entries []clipEntry) {
		fmt.Fprintf(&b, "<playlist id=\"%s\">\n", id)
		cursor := int64(0)
		for _, entry := range entries {
			if entry.Start > cursor {
				fmt.Fprintf(&b, "<blank length=\"%d\"/>\n", maxInt64(frames(entry.Start-cursor), 1))
			}
			in, out := frames(entry.SourceStart), frames(entry.SourceStart+entry.Duration)-1
			fmt.Fprintf(&b, "<entry producer=\"%s\" in=\"%d\" out=\"%d\"><property name=\"kdenlive:id\">%d</property></entry>\n", producer[entry.AssetID], in, maxInt64(out, in), clipID[entry.AssetID])
			cursor = entry.Start + entry.Duration
		}
		b.WriteString("</playlist>\n")
	}
	visualEntries := make([]clipEntry, 0, len(p.Visuals))
	for _, v := range p.Visuals {
		visualEntries = append(visualEntries, clipEntry{AssetID: v.AssetID, Name: v.ID, Start: v.StartMS, SourceStart: v.SourceStartMS, Duration: v.EndMS - v.StartMS})
	}
	voiceEntries := []clipEntry{{AssetID: p.Voiceover.AssetID, Name: "voiceover", Duration: p.Format.DurationMS}}
	musicEntries := make([]clipEntry, 0, len(p.AudioTracks))
	for _, t := range p.AudioTracks {
		musicEntries = append(musicEntries, clipEntry{AssetID: t.AssetID, Name: t.AssetID, Start: t.StartMS, SourceStart: t.SourceStartMS, Duration: t.EndMS - t.StartMS})
	}
	cueEntries := make([]clipEntry, 0, len(p.AudioCues))
	for _, c := range p.AudioCues {
		cueEntries = append(cueEntries, clipEntry{AssetID: c.AssetID, Name: c.AssetID, Start: c.StartMS, SourceStart: c.SourceStartMS, Duration: c.DurationMS})
	}
	type timelineTrack struct {
		playlist, empty, name, hide string
	}
	playlist("playlist0", visualEntries)
	playlist("playlist1", nil)
	playlist("playlist2", voiceEntries)
	playlist("playlist3", nil)
	tracks := []timelineTrack{{playlist: "playlist0", empty: "playlist1", name: "Video", hide: "audio"}, {playlist: "playlist2", empty: "playlist3", name: "Voiceover", hide: "video"}}
	nextPlaylist := 4
	for _, entry := range musicEntries {
		id := fmt.Sprintf("playlist%d", nextPlaylist)
		empty := fmt.Sprintf("playlist%d", nextPlaylist+1)
		nextPlaylist += 2
		playlist(id, []clipEntry{entry})
		playlist(empty, nil)
		tracks = append(tracks, timelineTrack{playlist: id, empty: empty, name: "Music", hide: "video"})
	}
	for _, entry := range cueEntries {
		id := fmt.Sprintf("playlist%d", nextPlaylist)
		empty := fmt.Sprintf("playlist%d", nextPlaylist+1)
		nextPlaylist += 2
		playlist(id, []clipEntry{entry})
		playlist(empty, nil)
		tracks = append(tracks, timelineTrack{playlist: id, empty: empty, name: "Sound Effects", hide: "video"})
	}
	trackTractors := make([]string, 0, len(tracks))
	for index, track := range tracks {
		id := fmt.Sprintf("tractor%d", index)
		trackTractors = append(trackTractors, id)
		audioTrack := ""
		if track.hide == "video" {
			// Kdenlive's Gen-5 importer uses this tractor property (not MLT's
			// hide attribute) to decide whether a timeline track is audio.
			audioTrack = "<property name=\"kdenlive:audio_track\">1</property>"
		}
		fmt.Fprintf(&b, "<tractor id=\"%s\" in=\"0\" out=\"%d\"><property name=\"kdenlive:track_name\">%s</property>%s<property name=\"kdenlive:trackheight\">67</property><property name=\"kdenlive:locked_track\">0</property><property name=\"kdenlive:timeline_active\">1</property><property name=\"kdenlive:collapsed\">0</property><track producer=\"%s\" hide=\"%s\"/><track producer=\"%s\" hide=\"%s\"/></tractor>\n", id, lastFrame, esc(track.name), audioTrack, track.playlist, track.hide, track.empty, track.hide)
	}
	fmt.Fprintf(&b, "<tractor id=\"%s\" in=\"0\" out=\"%d\"><property name=\"kdenlive:uuid\">%s</property><property name=\"kdenlive:clipname\">Auto-Editor Timeline</property><property name=\"kdenlive:sequenceproperties.hasAudio\">1</property><property name=\"kdenlive:sequenceproperties.hasVideo\">1</property><property name=\"kdenlive:sequenceproperties.activeTrack\">0</property><property name=\"kdenlive:sequenceproperties.tracksCount\">%d</property><property name=\"kdenlive:sequenceproperties.documentuuid\">%s</property><property name=\"kdenlive:duration\">%s</property><property name=\"kdenlive:maxduration\">%d</property><property name=\"kdenlive:producer_type\">17</property><property name=\"kdenlive:id\">%d</property><property name=\"kdenlive:clip_type\">0</property><property name=\"kdenlive:folderid\">1</property><property name=\"kdenlive:file_hash\">%s</property><property name=\"kdenlive:sequenceproperties.groups\">[]</property><track producer=\"black_track\"/>", sequenceID, lastFrame, sequenceID, len(trackTractors), sequenceID, kdenliveTimecode(durationFrames, fps), durationFrames, len(refs)+1, sequenceHash)
	for index, id := range trackTractors {
		fmt.Fprintf(&b, "<track producer=\"%s\" hide=\"%s\"/>", id, tracks[index].hide)
	}
	b.WriteString("<transition id=\"transition0\"><property name=\"a_track\">0</property><property name=\"b_track\">1</property><property name=\"mlt_service\">qtblend</property><property name=\"internal_added\">237</property><property name=\"always_active\">1</property></transition></tractor>\n")
	b.WriteString("<playlist id=\"main_bin\">")
	fmt.Fprintf(&b, "<property name=\"kdenlive:folder.-1.1\">Sequences</property><property name=\"kdenlive:sequenceFolder\">1</property><property name=\"kdenlive:docproperties.audioChannels\">2</property><property name=\"kdenlive:docproperties.compositing\">1</property><property name=\"kdenlive:docproperties.documentid\">1</property><property name=\"kdenlive:docproperties.enableTimelineZone\">0</property><property name=\"kdenlive:docproperties.enableproxy\">0</property><property name=\"kdenlive:docproperties.generateproxy\">0</property><property name=\"kdenlive:docproperties.kdenliveversion\">26.08.0</property><property name=\"kdenlive:docproperties.profile\">%s</property><property name=\"kdenlive:docproperties.projectfolder\">%s</property><property name=\"kdenlive:docproperties.uuid\">%s</property><property name=\"kdenlive:docproperties.version\">1.1</property><property name=\"kdenlive:docproperties.opensequences\">%s</property><property name=\"kdenlive:docproperties.activetimeline\">%s</property><property name=\"kdenlive:docproperties.groups\">[]</property><property name=\"xml_retain\">1</property>", kdenliveProfileID(options.Width, options.Height, fps), esc(projectDir), sequenceID, sequenceID, sequenceID)
	for _, ref := range refs {
		fmt.Fprintf(&b, "<entry producer=\"%s\" in=\"0\" out=\"%d\"/>", producer[ref.ID], lastFrame)
	}
	fmt.Fprintf(&b, "<entry producer=\"%s\" in=\"0\" out=\"0\"/></playlist>\n", sequenceID)
	fmt.Fprintf(&b, "<tractor id=\"%s\" in=\"0\" out=\"%d\"><property name=\"kdenlive:projectTractor\">1</property><track producer=\"%s\" in=\"0\" out=\"%d\"/></tractor>\n", projectID, lastFrame, sequenceID, lastFrame)
	b.WriteString("</mlt>\n")
	return b.String()
}

type clipEntry struct {
	AssetID, Name                string
	Start, SourceStart, Duration int64
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func kdenliveUUID(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	bytes := sum[:16]
	bytes[6] = (bytes[6] & 0x0f) | 0x50 // RFC 4122 version 5.
	bytes[8] = (bytes[8] & 0x3f) | 0x80 // RFC 4122 variant.
	hex := fmt.Sprintf("%x", bytes)
	return hex[0:8] + "-" + hex[8:12] + "-" + hex[12:16] + "-" + hex[16:20] + "-" + hex[20:32]
}

func kdenliveHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", sum[:16])
}

func kdenliveTimecode(frames int64, fps int) string {
	if fps <= 0 {
		fps = 30
	}
	hours := frames / int64(fps*60*60)
	minutes := (frames / int64(fps*60)) % 60
	seconds := (frames / int64(fps)) % 60
	return fmt.Sprintf("%02d:%02d:%02d:%02d", hours, minutes, seconds, frames%int64(fps))
}

func kdenliveProfileID(width, height, fps int) string {
	if width == 1920 && height == 1080 && fps == 30 {
		return "atsc_1080p_30"
	}
	return fmt.Sprintf("auto-editor_%dx%dp%d", width, height, fps)
}

func mediaDuration(p plan.Result, ref assetRef) string {
	if ref.Kind == "image" {
		return sec(p.Format.DurationMS)
	}
	for _, v := range p.Visuals {
		if v.AssetID == ref.ID {
			return sec(v.EndMS - v.StartMS)
		}
	}
	return sec(p.Format.DurationMS)
}

func sec(ms int64) string     { return strconv.FormatFloat(float64(ms)/1000, 'f', 3, 64) + "s" }
func esc(value string) string { return html.EscapeString(value) }
func safeName(value string) string {
	value = filepath.Base(value)
	value = strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(value)
	return value
}
func fileURI(path string) string {
	abs, _ := filepath.Abs(path)
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	return u.String()
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
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := out.ReadFrom(in); err != nil {
		return err
	}
	return out.Close()
}
