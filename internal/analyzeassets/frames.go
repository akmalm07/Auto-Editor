package analyzeassets

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type SystemFrameExtractor struct{}

func (SystemFrameExtractor) Extract(videoPath, outputPath, executable string, timestampMS int64) error {
	if executable == "" {
		executable = "ffmpeg"
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	seconds := fmt.Sprintf("%.3f", float64(timestampMS)/1000)
	if output, err := exec.Command(executable, "-y", "-ss", seconds, "-i", videoPath, "-frames:v", "1", "-q:v", "2", outputPath).CombinedOutput(); err != nil {
		return fmt.Errorf("extract frame with %s: %w: %s", executable, err, string(output))
	}
	return nil
}
