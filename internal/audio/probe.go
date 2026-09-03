package audio

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type Probe struct {
	DurationMS int64  `json:"duration_ms"`
	Codec      string `json:"codec"`
	SampleRate int    `json:"sample_rate"`
	Channels   int    `json:"channels"`
}

type CommandRunner interface {
	Output(name string, args ...string) ([]byte, error)
}

type systemRunner struct{}

func (systemRunner) Output(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

func ProbeFile(path, executable string) (Probe, error) {
	return ProbeFileWithRunner(path, executable, systemRunner{})
}

func ProbeFileWithRunner(path, executable string, runner CommandRunner) (Probe, error) {
	if strings.TrimSpace(executable) == "" {
		executable = "ffprobe"
	}

	output, err := runner.Output(executable,
		"-v", "error",
		"-select_streams", "a:0",
		"-show_entries", "stream=codec_name,sample_rate,channels:format=duration",
		"-of", "json",
		path,
	)
	if err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) && errors.Is(execErr.Err, exec.ErrNotFound) {
			return Probe{}, fmt.Errorf("%s is not installed or not on PATH; install FFmpeg or pass --ffprobe <path>", executable)
		}
		return Probe{}, fmt.Errorf("probe voiceover with %s: %w", executable, err)
	}

	var raw struct {
		Streams []struct {
			CodecName  string `json:"codec_name"`
			SampleRate string `json:"sample_rate"`
			Channels   int    `json:"channels"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(output, &raw); err != nil {
		return Probe{}, fmt.Errorf("decode %s output: %w", executable, err)
	}
	if len(raw.Streams) == 0 {
		return Probe{}, fmt.Errorf("voiceover has no audio stream")
	}

	durationSeconds, err := strconv.ParseFloat(raw.Format.Duration, 64)
	if err != nil || durationSeconds <= 0 {
		return Probe{}, fmt.Errorf("voiceover has an invalid duration %q", raw.Format.Duration)
	}
	sampleRate, err := strconv.Atoi(raw.Streams[0].SampleRate)
	if err != nil || sampleRate <= 0 {
		return Probe{}, fmt.Errorf("voiceover has an invalid sample rate %q", raw.Streams[0].SampleRate)
	}
	if raw.Streams[0].Channels <= 0 {
		return Probe{}, fmt.Errorf("voiceover has an invalid channel count")
	}

	return Probe{
		DurationMS: int64(durationSeconds * 1000),
		Codec:      raw.Streams[0].CodecName,
		SampleRate: sampleRate,
		Channels:   raw.Streams[0].Channels,
	}, nil
}
