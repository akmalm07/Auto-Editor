package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"auto-editor/internal/alignment"
	"auto-editor/internal/analyzeassets"
	"auto-editor/internal/analyzestyle"
	"auto-editor/internal/app"
	"auto-editor/internal/ingest"
	"auto-editor/internal/plan"
	"auto-editor/internal/project"
	"auto-editor/internal/render"
)

type workflowContext struct {
	HasReferences bool `json:"has_references"`
}

// runWorkflowContext exposes deterministic project facts for the TypeScript
// LangGraph orchestrator. It deliberately performs no routing or stage work.
func runWorkflowContext(args []string, ctx app.App) error {
	fs := flag.NewFlagSet("workflow-context", flag.ContinueOnError)
	fs.SetOutput(ctx.Err)
	jsonOutput := fs.Bool("json", false, "write machine-readable workflow context")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || !*jsonOutput {
		return fmt.Errorf("workflow-context requires --json and accepts no positional arguments")
	}
	manifest, err := project.ReadManifest(project.DefaultManifestName)
	if err != nil {
		return fmt.Errorf("read project manifest: %w", err)
	}
	return json.NewEncoder(ctx.Out).Encode(workflowContext{HasReferences: len(manifest.Inputs.ReferenceSets) > 0})
}

func runStatus(args []string, ctx app.App) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(ctx.Err)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("status does not accept positional arguments")
	}

	manifestPath := project.DefaultManifestName
	configPath := "auto-editor.config.json"
	indexPath := ingest.DefaultIndexPath
	alignmentPath := alignment.DefaultAlignmentPath
	assetsPath := analyzeassets.DefaultAssetsPath
	stylePath := analyzestyle.DefaultStylePath
	planPath := plan.DefaultPlanPath
	reportPath := render.DefaultReportPath

	manifestExists := printArtifactStatus(ctx, "manifest", manifestPath)
	printArtifactStatus(ctx, "config", configPath)
	indexExists := printArtifactStatus(ctx, "ingest index", indexPath)
	alignmentExists := printArtifactStatus(ctx, "alignment", alignmentPath)
	assetsExists := printArtifactStatus(ctx, "asset analysis", assetsPath)

	hasReferences := false
	if manifestExists {
		manifest, err := project.ReadManifest(manifestPath)
		if err != nil {
			fmt.Fprintf(ctx.Out, "! manifest could not be read: %v\n", err)
		} else {
			hasReferences = len(manifest.Inputs.ReferenceSets) > 0
		}
	}
	styleExists := false
	if hasReferences {
		styleExists = printArtifactStatus(ctx, "style analysis", stylePath)
	} else {
		fmt.Fprintln(ctx.Out, "- style analysis (not needed: no reference sets configured)")
	}
	planExists := printArtifactStatus(ctx, "plan", planPath)
	renderExists := printArtifactStatus(ctx, "render report", reportPath)

	switch {
	case !manifestExists:
		fmt.Fprintln(ctx.Out, "next: auto-editor init ...")
	case !indexExists:
		fmt.Fprintln(ctx.Out, "next: auto-editor ingest")
	case !alignmentExists:
		fmt.Fprintln(ctx.Out, "next: auto-editor align")
	case !assetsExists:
		fmt.Fprintln(ctx.Out, "next: auto-editor analyze-assets")
	case hasReferences && !styleExists:
		fmt.Fprintln(ctx.Out, "next: auto-editor analyze-style")
	case !planExists:
		fmt.Fprintln(ctx.Out, "next: auto-editor plan")
	case !renderExists:
		fmt.Fprintln(ctx.Out, "next: auto-editor render")
	default:
		fmt.Fprintln(ctx.Out, "next: render is ready for review")
	}
	return nil
}

func printArtifactStatus(ctx app.App, label, path string) bool {
	if _, err := os.Stat(path); err == nil {
		fmt.Fprintf(ctx.Out, "✓ %s: %s\n", label, filepathDisplay(path))
		return true
	} else if !os.IsNotExist(err) {
		fmt.Fprintf(ctx.Out, "! %s: %v\n", label, err)
		return false
	}
	fmt.Fprintf(ctx.Out, "- %s: %s\n", label, filepathDisplay(path))
	return false
}

func filepathDisplay(path string) string {
	return strings.ReplaceAll(path, "\\", "/")
}
