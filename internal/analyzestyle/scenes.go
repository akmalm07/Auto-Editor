package analyzestyle

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
)

var ptsTime = regexp.MustCompile(`pts_time:([0-9.]+)`)

type SystemSceneDetector struct{}

func (SystemSceneDetector) Detect(path, executable string) ([]int64, error) {
	if executable == "" {
		executable = "ffmpeg"
	}
	output, err := exec.Command(executable, "-hide_banner", "-i", path, "-vf", "select='gt(scene,0.35)',showinfo", "-an", "-f", "null", "-").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("detect scene changes with %s: %w: %s", executable, err, string(output))
	}
	matches := ptsTime.FindAllStringSubmatch(string(output), -1)
	result := make([]int64, 0, len(matches))
	for _, match := range matches {
		seconds, err := strconv.ParseFloat(match[1], 64)
		if err != nil {
			continue
		}
		result = append(result, int64(seconds*1000))
	}
	return result, nil
}
