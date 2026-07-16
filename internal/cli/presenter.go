package cli

import (
	"fmt"
	"io"
	"strings"

	"auto-editor/internal/config"
	"auto-editor/internal/ingest"

	"github.com/fatih/color"
)

type presenter struct {
	out      io.Writer
	err      io.Writer
	progress bool
	styles   styles
}

type styles struct {
	errorLabel   func(a ...interface{}) string
	warningLabel func(a ...interface{}) string
	stageLabel   func(a ...interface{}) string
	successLabel func(a ...interface{}) string
}

func newPresenter(ctxOut, ctxErr io.Writer, cfg config.Config) presenter {
	enabled := cfg.UI.Color != "never"
	if cfg.UI.Color == "always" {
		color.NoColor = false
		enabled = true
	}

	return presenter{
		out:      ctxOut,
		err:      ctxErr,
		progress: cfg.UI.Progress,
		styles: styles{
			errorLabel:   color.New(color.FgRed, color.Bold).SprintFunc(),
			warningLabel: color.New(color.FgYellow, color.Bold).SprintFunc(),
			stageLabel:   color.New(color.FgCyan, color.Bold).SprintFunc(),
			successLabel: color.New(color.FgGreen, color.Bold).SprintFunc(),
		}.withColor(enabled),
	}
}

func (p presenter) Stage(current, total int, message string) {
	if !p.progress {
		return
	}
	fmt.Fprintf(p.out, "%s %s\n", p.styles.stageLabel(fmt.Sprintf("[%d/%d]", current, total)), message)
}

func (p presenter) Success(message string) {
	fmt.Fprintf(p.out, "%s %s\n", p.styles.successLabel("ok:"), message)
}

func (p presenter) Diagnostics(diagnostics []ingest.Diagnostic) {
	for _, diagnostic := range diagnostics {
		label := diagnostic.Level + ":"
		if diagnostic.Level == "error" {
			label = p.styles.errorLabel(label)
		}
		if diagnostic.Level == "warning" {
			label = p.styles.warningLabel(label)
		}

		location := diagnostic.Field
		if diagnostic.Path != "" {
			location += " " + diagnostic.Path
		}
		fmt.Fprintf(p.err, "%s %s: %s\n", label, location, diagnostic.Message)
	}
}

func (s styles) withColor(enabled bool) styles {
	if enabled {
		return s
	}

	plain := func(a ...interface{}) string {
		return strings.TrimSuffix(fmt.Sprintln(a...), "\n")
	}
	s.errorLabel = plain
	s.warningLabel = plain
	s.stageLabel = plain
	s.successLabel = plain
	return s
}
