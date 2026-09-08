package cli

import (
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

type pipelineStage int

const (
	stageIngest pipelineStage = iota + 1
	stageAlign
	stageAnalyzeAssets
	stageAnalyzeStyle
	stagePlan
	stageRender
)

func parsePipelineStage(value string) (pipelineStage, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "ingest":
		return stageIngest, nil
	case "align":
		return stageAlign, nil
	case "analyze-assets":
		return stageAnalyzeAssets, nil
	case "analyze-style":
		return stageAnalyzeStyle, nil
	case "plan":
		return stagePlan, nil
	case "render":
		return stageRender, nil
	default:
		return 0, fmt.Errorf("invalid --through value %q: expected ingest, align, analyze-assets, analyze-style, plan, or render", value)
	}
}

func runPipeline(args []string, ctx app.App) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(ctx.Err)
	through := fs.String("through", "plan", "last stage to run: ingest, align, analyze-assets, analyze-style, plan, or render")
	noSemantic := fs.Bool("no-semantic", false, "disable semantic analysis for this run")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("run does not accept positional arguments")
	}
	lastStage, err := parsePipelineStage(*through)
	if err != nil {
		return err
	}

	runStage := func(name string, action func() error) error {
		fmt.Fprintf(ctx.Out, "==> %s\n", name)
		return action()
	}
	if err := runStage("ingest", func() error { return runIngest(nil, ctx) }); err != nil {
		return err
	}
	if lastStage == stageIngest {
		return pipelineComplete(ctx, *through)
	}
	if err := runStage("align", func() error { return runAlign(nil, ctx) }); err != nil {
		return err
	}
	if lastStage == stageAlign {
		return pipelineComplete(ctx, *through)
	}
	semanticArgs := []string(nil)
	if *noSemantic {
		semanticArgs = []string{"--no-semantic"}
	}
	if err := runStage("analyze-assets", func() error { return runAnalyzeAssets(semanticArgs, ctx) }); err != nil {
		return err
	}
	if lastStage == stageAnalyzeAssets {
		return pipelineComplete(ctx, *through)
	}
	references, err := projectHasReferences()
	if err != nil {
		return err
	}
	if references {
		if err := runStage("analyze-style", func() error { return runAnalyzeStyle(semanticArgs, ctx) }); err != nil {
			return err
		}
	} else {
		fmt.Fprintln(ctx.Out, "==> analyze-style (skipped: no reference sets configured)")
	}
	if lastStage == stageAnalyzeStyle {
		return pipelineComplete(ctx, *through)
	}
	if err := runStage("plan", func() error { return runPlan(semanticArgs, ctx) }); err != nil {
		return err
	}
	if lastStage == stagePlan {
		return pipelineComplete(ctx, *through)
	}
	if err := runStage("render", func() error { return runRender([]string{"--force"}, ctx) }); err != nil {
		return err
	}
	return pipelineComplete(ctx, *through)
}

func pipelineComplete(ctx app.App, through string) error {
	fmt.Fprintf(ctx.Out, "ok: pipeline completed through %s\n", through)
	return nil
}

func projectHasReferences() (bool, error) {
	manifest, err := project.ReadManifest(project.DefaultManifestName)
	if err != nil {
		return false, fmt.Errorf("read project manifest: %w", err)
	}
	return len(manifest.Inputs.ReferenceSets) > 0, nil
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
