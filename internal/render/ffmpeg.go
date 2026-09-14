package render

import (
	"auto-editor/internal/config"
	"auto-editor/internal/plan"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type Runner interface {
	Run(context.Context, string, string, ...string) ([]byte, error)
}
type SystemRunner struct{}

func (SystemRunner) Run(ctx context.Context, dir, executable string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

func buildClips(ctx context.Context, runner Runner, workDir, projectRoot string, p plan.Result, sources []Source, settings config.RenderConfig, report StageReporter) ([]string, []string, error) {
	byID := make(map[string]Source, len(sources))
	for _, source := range sources {
		byID[source.AssetID] = source
	}
	clips, warnings := make([]string, 0, len(p.Visuals)+2), []string{}
	clipIndex := 0
	appendBlack := func(durationMS int64, position string) error {
		if durationMS <= 0 {
			return nil
		}
		clip := filepath.Join(workDir, fmt.Sprintf("clip-%03d.mp4", clipIndex))
		clipIndex++
		args := []string{"-hide_banner", "-nostdin", "-y", "-f", "lavfi", "-i", fmt.Sprintf("color=c=black:s=%dx%d:r=%d", settings.Width, settings.Height, settings.FPS), "-t", seconds(durationMS), "-an", "-c:v", settings.VideoCodec, "-crf", strconv.Itoa(settings.CRF), "-preset", settings.Preset, "-pix_fmt", "yuv420p", "-r", strconv.Itoa(settings.FPS), clip}
		if output, err := runner.Run(ctx, workDir, settings.FFmpeg, args...); err != nil {
			return commandError("create black filler", output, err)
		}
		clips = append(clips, clip)
		warnings = append(warnings, fmt.Sprintf("inserted a black filler clip for %dms %s", durationMS, position))
		return nil
	}
	previousEnd := int64(0)
	for index, visual := range p.Visuals {
		if report != nil {
			report(index+1, len(p.Visuals), fmt.Sprintf("Rendering visual %d/%d", index+1, len(p.Visuals)))
		}
		if err := appendBlack(visual.StartMS-previousEnd, "before visual "+visual.ID); err != nil {
			return nil, nil, err
		}
		source := byID[visual.AssetID]
		path := source.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(projectRoot, filepath.FromSlash(path))
		}
		clip := filepath.Join(workDir, fmt.Sprintf("clip-%03d.mp4", clipIndex))
		clipIndex++
		planned := visual.EndMS - visual.StartMS
		sourceDuration := visual.SourceEndMS - visual.SourceStartMS
		filter := coverFilter(settings)
		args := []string{"-hide_banner", "-nostdin", "-y"}
		if source.Kind == "image" {
			args = append(args, "-loop", "1", "-framerate", strconv.Itoa(settings.FPS), "-i", path)
		} else if source.Kind == "video" {
			args = append(args, "-ss", seconds(visual.SourceStartMS), "-t", seconds(sourceDuration), "-i", path)
			if sourceDuration < planned {
				filter += fmt.Sprintf(",tpad=stop_mode=clone:stop_duration=%s", seconds(planned-sourceDuration))
				warnings = append(warnings, fmt.Sprintf("visual %s held its final video frame for %dms", visual.ID, planned-sourceDuration))
			}
		} else {
			return nil, nil, fmt.Errorf("visual %s has unsupported asset kind %q", visual.ID, source.Kind)
		}
		args = append(args, "-vf", filter, "-t", seconds(planned), "-an", "-c:v", settings.VideoCodec, "-crf", strconv.Itoa(settings.CRF), "-preset", settings.Preset, "-pix_fmt", "yuv420p", "-r", strconv.Itoa(settings.FPS), clip)
		if output, err := runner.Run(ctx, workDir, settings.FFmpeg, args...); err != nil {
			return nil, nil, commandError("create clip", output, err)
		}
		clips = append(clips, clip)
		previousEnd = visual.EndMS
	}
	if err := appendBlack(p.Format.DurationMS-previousEnd, "after the final visual"); err != nil {
		return nil, nil, err
	}
	return clips, warnings, nil
}

func concatClips(ctx context.Context, runner Runner, workDir string, clips []string, outputPath string, settings config.RenderConfig) error {
	listPath := filepath.Join(workDir, "clips.ffconcat")
	var list strings.Builder
	list.WriteString("ffconcat version 1.0\n")
	for _, clip := range clips {
		list.WriteString("file '")
		list.WriteString(ffconcatPath(clip))
		list.WriteString("'\n")
	}
	if err := os.WriteFile(listPath, []byte(list.String()), 0o644); err != nil {
		return err
	}
	args := []string{"-hide_banner", "-nostdin", "-y", "-f", "concat", "-safe", "0", "-i", listPath, "-c", "copy", outputPath}
	output, err := runner.Run(ctx, workDir, settings.FFmpeg, args...)
	if err != nil {
		return commandError("concatenate clips", output, err)
	}
	return nil
}

func muxFinal(ctx context.Context, runner Runner, workDir, videoPath, voiceoverPath, captionPath string, musicTracks []plan.AudioTrack, outputPath string, settings config.RenderConfig) error {
	args := []string{"-hide_banner", "-nostdin", "-y", "-i", videoPath, "-i", voiceoverPath}
	for _, track := range musicTracks {
		if track.Loop {
			args = append(args, "-stream_loop", "-1")
		}
		args = append(args, "-i", track.Path)
	}
	if captionPath != "" {
		args = append(args, "-vf", "ass="+filepath.Base(captionPath))
	}
	if len(musicTracks) == 0 {
		args = append(args, "-map", "0:v:0", "-map", "1:a:0")
	} else {
		filters := make([]string, 0, len(musicTracks)+4)
		musicLabels := make([]string, 0, len(musicTracks))
		sfxLabels := make([]string, 0, len(musicTracks))
		hasLoopingMusic := false
		for index, track := range musicTracks {
			if track.Loop {
				hasLoopingMusic = true
			}
			input := index + 2
			label := fmt.Sprintf("m%d", index)
			volumeDB, fadeInMS, fadeOutMS := settings.MusicVolumeDB, settings.MusicFadeInMS, settings.MusicFadeOutMS
			if !track.Loop {
				// Sound-effect cues must not inherit the background music fades;
				// short cues can otherwise be faded out before becoming audible.
				volumeDB, fadeInMS, fadeOutMS = 0, 0, 0
			}
			if track.VolumeDB != 0 {
				volumeDB = track.VolumeDB
			}
			if track.FadeInMS != 0 {
				fadeInMS = track.FadeInMS
			}
			if track.FadeOutMS != 0 {
				fadeOutMS = track.FadeOutMS
			}
			filter := fmt.Sprintf("[%d:a]atrim=start=%s:duration=%s,asetpts=PTS-STARTPTS,volume=%sdB", input, seconds(track.SourceStartMS), seconds(track.EndMS-track.StartMS), formatDB(volumeDB))
			if track.StartMS > 0 {
				filter += fmt.Sprintf(",adelay=%d|%d", track.StartMS, track.StartMS)
			}
			if fadeInMS > 0 {
				filter += fmt.Sprintf(",afade=t=in:st=0:d=%s", seconds(fadeInMS))
			}
			if fadeOutMS > 0 {
				filter += fmt.Sprintf(",afade=t=out:st=%s:d=%s", seconds(max64(track.EndMS-track.StartMS-fadeOutMS, 0)), seconds(fadeOutMS))
			}
			filter += "[" + label + "]"
			filters = append(filters, filter)
			if track.Loop {
				musicLabels = append(musicLabels, "["+label+"]")
			} else {
				sfxLabels = append(sfxLabels, "["+label+"]")
			}
		}
		mix := func(labels []string, name string) string {
			if len(labels) == 1 {
				return labels[0]
			}
			filters = append(filters, strings.Join(labels, "")+fmt.Sprintf("amix=inputs=%d:duration=longest:dropout_transition=0[%s]", len(labels), name))
			return "[" + name + "]"
		}
		finalLabels := []string{"[1:a]"}
		if len(musicLabels) > 0 {
			music := mix(musicLabels, "music")
			if settings.MusicDucking && hasLoopingMusic {
				filters = append(filters, music+"[1:a]sidechaincompress=threshold=0.02:ratio="+formatFloat(settings.MusicDuckRatio)+":attack=20:release=400[ducked]")
				music = "[ducked]"
			}
			finalLabels = append(finalLabels, music)
		}
		if len(sfxLabels) > 0 {
			finalLabels = append(finalLabels, mix(sfxLabels, "sfx"))
		}
		filters = append(filters, strings.Join(finalLabels, "")+fmt.Sprintf("amix=inputs=%d:duration=first:dropout_transition=0[aout]", len(finalLabels)))
		args = append(args, "-filter_complex", strings.Join(filters, ";"), "-map", "0:v:0", "-map", "[aout]")
	}
	args = append(args, "-c:v", settings.VideoCodec, "-crf", strconv.Itoa(settings.CRF), "-preset", settings.Preset, "-c:a", settings.AudioCodec, "-b:a", settings.AudioBitrate, "-shortest", "-movflags", "+faststart", outputPath)
	output, err := runner.Run(ctx, workDir, settings.FFmpeg, args...)
	if err != nil {
		return commandError("mux final output", output, err)
	}
	return nil
}

func probeOutput(ctx context.Context, runner Runner, executable, path string) (Output, error) {
	if executable == "" {
		executable = "ffprobe"
	}
	args := []string{"-v", "error", "-show_entries", "format=duration:stream=codec_type,width,height,r_frame_rate", "-of", "json", path}
	data, err := runner.Run(ctx, "", executable, args...)
	if err != nil {
		return Output{}, commandError("probe output", data, err)
	}
	var raw struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			CodecType string `json:"codec_type"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
			FrameRate string `json:"r_frame_rate"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Output{}, fmt.Errorf("decode ffprobe output: %w", err)
	}
	for _, stream := range raw.Streams {
		if stream.CodecType == "video" {
			duration, err := strconv.ParseFloat(raw.Format.Duration, 64)
			if err != nil || duration <= 0 {
				return Output{}, fmt.Errorf("ffprobe returned invalid output duration %q", raw.Format.Duration)
			}
			return Output{DurationMS: int64(duration * 1000), Width: stream.Width, Height: stream.Height, FPS: stream.FrameRate}, nil
		}
	}
	return Output{}, fmt.Errorf("rendered output has no video stream")
}

func coverFilter(settings config.RenderConfig) string {
	return fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d,setsar=1,fps=%d", settings.Width, settings.Height, settings.Width, settings.Height, settings.FPS)
}
func seconds(ms int64) string         { return strconv.FormatFloat(float64(ms)/1000, 'f', 3, 64) }
func ffconcatPath(path string) string { return strings.ReplaceAll(filepath.ToSlash(path), "'", "\\'") }
func commandError(stage string, output []byte, err error) error {
	message := strings.TrimSpace(string(output))
	if len(message) > 2000 {
		message = message[len(message)-2000:]
	}
	if message == "" {
		return fmt.Errorf("%s: %w", stage, err)
	}
	var execErr *exec.Error
	if errors.As(err, &execErr) && errors.Is(execErr.Err, exec.ErrNotFound) {
		return fmt.Errorf("%s is not installed or not on PATH", execErr.Name)
	}
	return fmt.Errorf("%s: %w: %s", stage, err, message)
}

func formatDB(value float64) string    { return strconv.FormatFloat(value, 'f', 2, 64) }
func formatFloat(value float64) string { return strconv.FormatFloat(value, 'f', 2, 64) }
func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
