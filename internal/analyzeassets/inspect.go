package analyzeassets

import (
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"os/exec"
	"strings"
)

type SystemInspector struct{}

func (SystemInspector) Inspect(path, kind, executable string) (Technical, error) {
	if kind == "image" {
		file, err := os.Open(path)
		if err != nil {
			return Technical{}, err
		}
		defer file.Close()
		cfg, _, err := image.DecodeConfig(file)
		if err == nil {
			return Technical{Width: cfg.Width, Height: cfg.Height}, nil
		}
		return inspectImageWithFFprobe(path, executable)
	}
	if executable == "" {
		executable = "ffprobe"
	}
	data, err := exec.Command(executable, "-v", "error", "-show_entries", "format=duration:stream=codec_type,codec_name,width,height,avg_frame_rate,sample_rate,channels", "-of", "json", path).Output()
	if err != nil {
		return Technical{}, fmt.Errorf("inspect media with %s: %w", executable, err)
	}
	var raw struct {
		Streams []struct {
			Type, Codec, FrameRate, SampleRate string `json:"-"`
			CodecType                          string `json:"codec_type"`
			CodecName                          string `json:"codec_name"`
			Width, Height, Channels            int
			AvgFrameRate                       string `json:"avg_frame_rate"`
			SampleRateJSON                     string `json:"sample_rate"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Technical{}, fmt.Errorf("decode %s output: %w", executable, err)
	}
	technical := Technical{}
	for _, stream := range raw.Streams {
		switch stream.CodecType {
		case "video":
			technical.Width, technical.Height, technical.FrameRate, technical.Codec = stream.Width, stream.Height, stream.AvgFrameRate, stream.CodecName
		case "audio":
			technical.HasAudio, technical.SampleRate, technical.Channels = true, parseInt(stream.SampleRateJSON), stream.Channels
			if technical.Codec == "" {
				technical.Codec = stream.CodecName
			}
		}
	}
	if raw.Format.Duration == "" {
		return Technical{}, fmt.Errorf("media has no valid duration")
	}
	var seconds float64
	if _, err := fmt.Sscanf(raw.Format.Duration, "%f", &seconds); err != nil || seconds <= 0 {
		return Technical{}, fmt.Errorf("media has invalid duration %q", raw.Format.Duration)
	}
	technical.DurationMS = int64(seconds * 1000)
	return technical, nil
}

func inspectImageWithFFprobe(path, executable string) (Technical, error) {
	if executable == "" {
		executable = "ffprobe"
	}
	data, err := exec.Command(executable, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height", "-of", "json", path).Output()
	if err != nil {
		return Technical{}, fmt.Errorf("inspect image with %s: %w", executable, err)
	}
	var raw struct {
		Streams []struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Technical{}, fmt.Errorf("decode %s output: %w", executable, err)
	}
	if len(raw.Streams) == 0 || raw.Streams[0].Width <= 0 || raw.Streams[0].Height <= 0 {
		return Technical{}, fmt.Errorf("image has no valid dimensions")
	}
	return Technical{Width: raw.Streams[0].Width, Height: raw.Streams[0].Height}, nil
}

func parseInt(value string) int {
	var result int
	_, _ = fmt.Sscanf(strings.TrimSpace(value), "%d", &result)
	return result
}
