package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"auto-editor/internal/project"
)

func splitProjectFlag(args []string) (string, []string, error) {
	if len(args) == 0 {
		return "", args, nil
	}
	if args[0] == "--project" {
		if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
			return "", nil, fmt.Errorf("--project requires a directory path")
		}
		return args[1], args[2:], nil
	}
	if value, ok := strings.CutPrefix(args[0], "--project="); ok {
		if strings.TrimSpace(value) == "" {
			return "", nil, fmt.Errorf("--project requires a directory path")
		}
		return value, args[1:], nil
	}
	return "", args, nil
}

func findProjectRoot(start string) (string, bool, error) {
	current, err := filepath.Abs(start)
	if err != nil {
		return "", false, err
	}
	for {
		manifest := filepath.Join(current, project.DefaultManifestName)
		if _, err := os.Stat(manifest); err == nil {
			return current, true, nil
		} else if !os.IsNotExist(err) {
			return "", false, err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false, nil
		}
		current = parent
	}
}

func inProjectDirectory(path string, action func() error) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("project path is not a directory: %s", path)
	}
	previous, err := os.Getwd()
	if err != nil {
		return err
	}
	if err := os.Chdir(absolute); err != nil {
		return err
	}
	defer func() { _ = os.Chdir(previous) }()
	return action()
}

func commandUsesProject(command string) bool {
	switch command {
	case "ingest", "align", "analyze-assets", "analyze-style", "plan", "run", "status", "render", "export-project":
		return true
	default:
		return false
	}
}
