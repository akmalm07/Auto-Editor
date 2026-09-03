package audio

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

type fakeRunner struct {
	output []byte
	err    error
}

func (r fakeRunner) Output(string, ...string) ([]byte, error) { return r.output, r.err }

func TestProbeFileWithRunner(t *testing.T) {
	probe, err := ProbeFileWithRunner("voice.wav", "ffprobe", fakeRunner{output: []byte(`{"streams":[{"codec_name":"pcm_s16le","sample_rate":"48000","channels":2}],"format":{"duration":"12.345"}}`)})
	if err != nil {
		t.Fatal(err)
	}
	if probe.DurationMS != 12345 || probe.SampleRate != 48000 || probe.Channels != 2 {
		t.Fatalf("unexpected probe: %#v", probe)
	}
}

func TestProbeFileWithRunnerRejectsMissingStream(t *testing.T) {
	_, err := ProbeFileWithRunner("voice.wav", "ffprobe", fakeRunner{output: []byte(`{"streams":[],"format":{"duration":"12"}}`)})
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestProbeFileWithRunnerIncludesCommandFailure(t *testing.T) {
	_, err := ProbeFileWithRunner("voice.wav", "ffprobe", fakeRunner{err: errors.New("not found")})
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestProbeFileWithRunnerReportsMissingFFprobe(t *testing.T) {
	_, err := ProbeFileWithRunner("voice.wav", "ffprobe", fakeRunner{
		err: &exec.Error{Name: "ffprobe", Err: exec.ErrNotFound},
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "ffprobe is not installed or not on PATH") {
		t.Fatalf("unexpected error: %v", err)
	}
}
