package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"auto-editor/internal/alignment"
	"auto-editor/internal/analyzeassets"
	"auto-editor/internal/analyzestyle"
	"auto-editor/internal/app"
	"auto-editor/internal/asr/openai"
	assetopenai "auto-editor/internal/assetvision/openai"
	"auto-editor/internal/config"
	"auto-editor/internal/ingest"
	"auto-editor/internal/plan"
	"auto-editor/internal/project"
	"auto-editor/internal/ui/loading"
)

type stringListFlag []string

func (f *stringListFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *stringListFlag) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("path cannot be empty")
	}

	*f = append(*f, value)
	return nil
}

func Run(args []string, ctx app.App) error {
	if len(args) == 0 {
		printRootUsage(ctx)
		return nil
	}

	switch args[0] {
	case "help", "-h", "--help":
		printRootUsage(ctx)
		return nil
	case "version":
		fmt.Fprintln(ctx.Out, ctx.Version)
		return nil
	case "init":
		return runInit(args[1:], ctx)
	case "ingest":
		return runIngest(args[1:], ctx)
	case "align":
		return runAlign(args[1:], ctx)
	case "analyze-assets":
		return runAnalyzeAssets(args[1:], ctx)
	case "analyze-style":
		return runAnalyzeStyle(args[1:], ctx)
	case "plan":
		return runPlan(args[1:], ctx)
	case "render":
		return runPlaceholder("render", ctx)
	case "review":
		return runPlaceholder("review", ctx)
	default:
		printRootUsage(ctx)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runAnalyzeStyle(args []string, ctx app.App) error {
	fs := flag.NewFlagSet("analyze-style", flag.ContinueOnError)
	fs.SetOutput(ctx.Err)
	indexPath := fs.String("index", ingest.DefaultIndexPath, "ingest index path")
	configPath := fs.String("config", config.DefaultConfigName, "config file path")
	outputPath := fs.String("out", analyzestyle.DefaultStylePath, "style analysis output path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *indexPath == "" || *configPath == "" || *outputPath == "" {
		return fmt.Errorf("index, config, and output paths cannot be empty")
	}
	cfg, err := config.Read(*configPath)
	if err != nil {
		return err
	}
	ui := newPresenter(ctx.Out, ctx.Err, cfg)
	session := loading.New(ctx.Out, loading.Settings{Enabled: cfg.UI.Loading.Enabled, Style: cfg.UI.Loading.Style, IntervalMS: cfg.UI.Loading.IntervalMS, TTY: os.Stdout})
	session.Start("Preparing style analysis")
	result, err := analyzestyle.Build(context.Background(), analyzestyle.Options{IndexPath: *indexPath, FFprobe: cfg.Assets.FFprobe, FFmpeg: cfg.Assets.FFmpeg}, analyzestyle.Dependencies{}, func(current, total int, message string) {
		session.Update(fmt.Sprintf("[%d/%d] %s", current, total, message))
	})
	if err != nil {
		session.Stop("")
		return err
	}
	resolvedOutputPath, err := resolveAlignmentOutputPath(*indexPath, *outputPath)
	if err != nil {
		session.Stop("")
		return err
	}
	session.Update("Writing style analysis")
	if err := analyzestyle.Write(resolvedOutputPath, result); err != nil {
		session.Stop("")
		return err
	}
	session.Stop("")
	ui.Diagnostics(result.Warnings)
	ui.Success(fmt.Sprintf("analyzed %d reference set(s)", len(result.ReferenceSets)))
	ui.Success(fmt.Sprintf("created %s", filepath.Clean(resolvedOutputPath)))
	return nil
}

func runAnalyzeAssets(args []string, ctx app.App) error {
	fs := flag.NewFlagSet("analyze-assets", flag.ContinueOnError)
	fs.SetOutput(ctx.Err)
	indexPath := fs.String("index", ingest.DefaultIndexPath, "ingest index path")
	configPath := fs.String("config", config.DefaultConfigName, "config file path")
	outputPath := fs.String("out", analyzeassets.DefaultAssetsPath, "asset analysis output path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *indexPath == "" || *configPath == "" || *outputPath == "" {
		return fmt.Errorf("index, config, and output paths cannot be empty")
	}
	cfg, err := config.Read(*configPath)
	if err != nil {
		return err
	}
	ui := newPresenter(ctx.Out, ctx.Err, cfg)
	session := loading.New(ctx.Out, loading.Settings{Enabled: cfg.UI.Loading.Enabled, Style: cfg.UI.Loading.Style, IntervalMS: cfg.UI.Loading.IntervalMS, TTY: os.Stdout})
	deps := analyzeassets.Dependencies{}
	semanticEnabled := cfg.Assets.SemanticProvider == "openai"
	if semanticEnabled {
		apiKey, ok := os.LookupEnv(cfg.Assets.APIKeyEnvironment)
		if !ok || strings.TrimSpace(apiKey) == "" {
			semanticEnabled = false
			fmt.Fprintf(ctx.Err, "warning: semantic analysis skipped because environment variable %s is not set\n", cfg.Assets.APIKeyEnvironment)
		} else {
			deps.SemanticAnalyzer = assetopenai.Client{APIKey: apiKey, Model: cfg.Assets.SemanticModel}
		}
	}
	session.Start("Preparing asset analysis")
	result, err := analyzeassets.Build(context.Background(), analyzeassets.Options{IndexPath: *indexPath, FFprobe: cfg.Assets.FFprobe, FFmpeg: cfg.Assets.FFmpeg, SemanticEnabled: semanticEnabled}, deps, func(current, total int, message string) {
		session.Update(fmt.Sprintf("[%d/%d] %s", current, total, message))
	})
	if err != nil {
		session.Stop("")
		return err
	}
	resolvedOutputPath, err := resolveAlignmentOutputPath(*indexPath, *outputPath)
	if err != nil {
		session.Stop("")
		return err
	}
	session.Update("Writing asset analysis")
	if err := analyzeassets.Write(resolvedOutputPath, result); err != nil {
		session.Stop("")
		return err
	}
	session.Stop("")
	ui.Diagnostics(result.Warnings)
	semanticComplete, visualAssets := 0, 0
	for _, asset := range result.Assets {
		if asset.Kind == ingest.KindImage || asset.Kind == ingest.KindVideo {
			visualAssets++
			if asset.Literal.Status == "complete" && asset.Editorial.Status == "complete" {
				semanticComplete++
			}
		}
	}
	ui.Success(fmt.Sprintf("analyzed %d asset(s) deterministically", len(result.Assets)))
	if semanticComplete == visualAssets {
		ui.Success(fmt.Sprintf("completed semantic analysis for %d visual asset(s)", semanticComplete))
	} else {
		fmt.Fprintf(ctx.Err, "warning: semantic analysis completed for %d of %d visual asset(s); see diagnostics\n", semanticComplete, visualAssets)
	}
	ui.Success(fmt.Sprintf("created %s", filepath.Clean(resolvedOutputPath)))
	return nil
}

func runInit(args []string, ctx app.App) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(ctx.Err)

	name := fs.String("name", "new-project", "project name")
	manifestPath := fs.String("manifest", project.DefaultManifestName, "manifest file path")
	configPath := fs.String("config", config.DefaultConfigName, "config file path")
	aspectRatio := fs.String("aspect", "16:9", "default aspect ratio")
	language := fs.String("language", "en", "default language")
	runtime := fs.String("runtime", "60s", "target runtime")
	force := fs.Bool("force", false, "overwrite an existing manifest")
	script := fs.String("script", "", "path to the voiceover script")
	voiceover := fs.String("voiceover", "", "path to the voiceover audio")

	var assets stringListFlag
	var references stringListFlag
	fs.Var(&assets, "asset", "asset file or directory path; may be repeated")
	fs.Var(&references, "reference", "reference set in name=path form; may be repeated")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *manifestPath == "" {
		return fmt.Errorf("manifest path cannot be empty")
	}
	if *configPath == "" {
		return fmt.Errorf("config path cannot be empty")
	}

	if !*force {
		if _, err := os.Stat(*manifestPath); err == nil {
			return fmt.Errorf("manifest already exists at %s", *manifestPath)
		} else if !os.IsNotExist(err) {
			return err
		}
	}

	manifest := project.NewManifest(*name)
	manifest.AspectRatio = *aspectRatio
	manifest.Language = *language
	manifest.Runtime = *runtime
	manifest.Inputs.Script = *script
	manifest.Inputs.Voiceover = *voiceover
	manifest.Inputs.Assets = assets

	referenceSets, err := parseReferenceSets(references)
	if err != nil {
		return err
	}
	manifest.Inputs.ReferenceSets = referenceSets

	if err := project.WriteManifest(*manifestPath, manifest); err != nil {
		return err
	}
	createdConfig := true
	if !*force {
		if _, err := os.Stat(*configPath); err == nil {
			fmt.Fprintf(ctx.Err, "config already exists at %s\n", *configPath)
			createdConfig = false
		} else if !os.IsNotExist(err) {
			return err
		} else if err := config.WriteDefault(*configPath); err != nil {
			return err
		}
	} else if err := config.WriteDefault(*configPath); err != nil {
		return err
	}

	fmt.Fprintf(ctx.Out, "created %s\n", filepath.Clean(*manifestPath))
	if createdConfig {
		fmt.Fprintf(ctx.Out, "created %s\n", filepath.Clean(*configPath))
	}
	return nil
}

func runIngest(args []string, ctx app.App) error {
	fs := flag.NewFlagSet("ingest", flag.ContinueOnError)
	fs.SetOutput(ctx.Err)

	manifestPath := fs.String("manifest", project.DefaultManifestName, "manifest file path")
	configPath := fs.String("config", config.DefaultConfigName, "config file path")
	outputPath := fs.String("out", ingest.DefaultIndexPath, "index output path")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *manifestPath == "" {
		return fmt.Errorf("manifest path cannot be empty")
	}
	if *configPath == "" {
		return fmt.Errorf("config path cannot be empty")
	}
	if *outputPath == "" {
		return fmt.Errorf("output path cannot be empty")
	}

	cfg, err := config.Read(*configPath)
	if err != nil {
		return err
	}
	ui := newPresenter(ctx.Out, ctx.Err, cfg)
	session := loading.New(ctx.Out, loading.Settings{
		Enabled:    cfg.UI.Loading.Enabled,
		Style:      cfg.UI.Loading.Style,
		IntervalMS: cfg.UI.Loading.IntervalMS,
		TTY:        os.Stdout,
	})

	session.Start("Loading config")
	index, err := ingest.BuildIndexWithProgress(*manifestPath, func(current, total int, message string) {
		session.Update(fmt.Sprintf("[%d/%d] %s", current, total, message))
	})
	if err != nil {
		var validationError ingest.ValidationError
		if errors.As(err, &validationError) {
			ui.Diagnostics(validationError.Diagnostics)
			session.Stop("")
			return silentError{}
		}
		session.Stop("")
		return err
	}

	resolvedOutputPath, err := resolveOutputPath(*manifestPath, *outputPath)
	if err != nil {
		session.Stop("")
		return err
	}
	session.Update("Writing index")
	if err := ingest.WriteIndex(resolvedOutputPath, index); err != nil {
		session.Stop("")
		return err
	}
	session.Stop("")

	ui.Diagnostics(index.Warnings)
	ui.Success(fmt.Sprintf("indexed %d asset(s), %d reference set(s)", len(index.Assets), len(index.ReferenceSets)))
	ui.Success(fmt.Sprintf("created %s", filepath.Clean(resolvedOutputPath)))
	return nil
}

func runAlign(args []string, ctx app.App) error {
	fs := flag.NewFlagSet("align", flag.ContinueOnError)
	fs.SetOutput(ctx.Err)
	indexPath := fs.String("index", ingest.DefaultIndexPath, "ingest index path")
	configPath := fs.String("config", config.DefaultConfigName, "config file path")
	outputPath := fs.String("out", alignment.DefaultAlignmentPath, "alignment output path")
	ffprobePath := fs.String("ffprobe", "ffprobe", "ffprobe executable")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *indexPath == "" || *configPath == "" || *outputPath == "" || *ffprobePath == "" {
		return fmt.Errorf("index, config, output, and ffprobe paths cannot be empty")
	}

	cfg, err := config.Read(*configPath)
	if err != nil {
		return err
	}
	apiKey, ok := os.LookupEnv(cfg.Alignment.APIKeyEnvironment)
	if !ok || strings.TrimSpace(apiKey) == "" {
		return fmt.Errorf("alignment requires an API key in environment variable %s", cfg.Alignment.APIKeyEnvironment)
	}
	ui := newPresenter(ctx.Out, ctx.Err, cfg)
	session := loading.New(ctx.Out, loading.Settings{Enabled: cfg.UI.Loading.Enabled, Style: cfg.UI.Loading.Style, IntervalMS: cfg.UI.Loading.IntervalMS, TTY: os.Stdout})
	session.Start("Preparing alignment")
	result, err := alignment.Build(context.Background(), alignment.Options{
		IndexPath: *indexPath, FFprobe: *ffprobePath, MinSimilarity: cfg.Alignment.MinSimilarity,
	}, alignment.Dependencies{
		Transcriber: openai.Client{APIKey: apiKey, Model: cfg.Alignment.Model},
	}, func(current, total int, message string) {
		session.Update(fmt.Sprintf("[%d/%d] %s", current, total, message))
	})
	if err != nil {
		session.Stop("")
		return err
	}
	resolvedOutputPath, err := resolveAlignmentOutputPath(*indexPath, *outputPath)
	if err != nil {
		session.Stop("")
		return err
	}
	session.Update("Writing alignment")
	if err := alignment.Write(resolvedOutputPath, result); err != nil {
		session.Stop("")
		return err
	}
	session.Stop("")
	ui.Diagnostics(result.Warnings)
	ui.Success(fmt.Sprintf("aligned %d/%d script words (similarity %.2f)", result.Metrics.MatchedWords, result.Metrics.ScriptWords, result.Metrics.Similarity))
	ui.Success(fmt.Sprintf("created %s", filepath.Clean(resolvedOutputPath)))
	return nil
}

type silentError struct{}

func (silentError) Error() string {
	return ""
}

func parseReferenceSets(values []string) ([]project.ReferenceSet, error) {
	byName := make(map[string]int)
	sets := make([]project.ReferenceSet, 0)

	for _, value := range values {
		name, path, ok := strings.Cut(value, "=")
		if !ok {
			return nil, fmt.Errorf("reference must use name=path form: %s", value)
		}

		name = strings.TrimSpace(name)
		path = strings.TrimSpace(path)
		if name == "" || path == "" {
			return nil, fmt.Errorf("reference must include both name and path: %s", value)
		}

		if index, exists := byName[name]; exists {
			sets[index].Paths = append(sets[index].Paths, path)
			continue
		}

		byName[name] = len(sets)
		sets = append(sets, project.ReferenceSet{
			Name:  name,
			Paths: []string{path},
		})
	}

	return sets, nil
}

func resolveOutputPath(manifestPath, outputPath string) (string, error) {
	if filepath.IsAbs(outputPath) {
		return filepath.Clean(outputPath), nil
	}

	absoluteManifestPath, err := filepath.Abs(manifestPath)
	if err != nil {
		return "", err
	}

	return filepath.Clean(filepath.Join(filepath.Dir(absoluteManifestPath), outputPath)), nil
}

func resolveAlignmentOutputPath(indexPath, outputPath string) (string, error) {
	if filepath.IsAbs(outputPath) {
		return filepath.Clean(outputPath), nil
	}
	index, err := ingest.ReadIndex(indexPath)
	if err != nil {
		return "", err
	}
	return filepath.Clean(filepath.Join(index.ProjectRoot, outputPath)), nil
}

func runPlan(args []string, ctx app.App) error {
	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	fs.SetOutput(ctx.Err)
	indexPath := fs.String("index", ingest.DefaultIndexPath, "ingest index path")
	assetsPath := fs.String("assets", analyzeassets.DefaultAssetsPath, "asset analysis path")
	stylePath := fs.String("style", analyzestyle.DefaultStylePath, "style analysis path")
	outputPath := fs.String("out", plan.DefaultPlanPath, "plan output path")
	aspect := fs.String("aspect", "16:9", "output aspect ratio")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Read(config.DefaultConfigName)
	if err != nil {
		return err
	}
	session := loading.New(ctx.Out, loading.Settings{Enabled: cfg.UI.Loading.Enabled, Style: cfg.UI.Loading.Style, IntervalMS: cfg.UI.Loading.IntervalMS, TTY: os.Stdout})
	session.Start("Preparing edit plan")
	result, err := plan.Build(context.Background(), plan.Options{AlignmentPath: filepath.Join(filepath.Dir(*indexPath), "alignment.json"), AssetsPath: *assetsPath, StylePath: *stylePath, AspectRatio: *aspect}, func(current, total int, message string) {
		session.Update(fmt.Sprintf("[%d/%d] %s", current, total, message))
	})
	if err != nil {
		session.Stop("")
		return err
	}
	out := filepath.Join(filepath.Dir(*indexPath), *outputPath)
	if filepath.IsAbs(*outputPath) {
		out = *outputPath
	}
	if err := plan.Write(out, result); err != nil {
		session.Stop("")
		return err
	}
	session.Stop("")
	newPresenter(ctx.Out, ctx.Err, cfg).Diagnostics(result.Warnings)
	fmt.Fprintf(ctx.Out, "ok: created %s\n", filepath.Clean(out))
	return nil
}

func runPlaceholder(name string, ctx app.App) error {
	fmt.Fprintf(ctx.Out, "%s is scaffolded but not implemented yet\n", strings.ToLower(name))
	return nil
}

func printRootUsage(ctx app.App) {
	fmt.Fprintf(ctx.Out, `%s

Usage:
  auto-editor <command> [flags]

Commands:
  init     create a project manifest
  ingest   validate project inputs and write an index
  align    transcribe voiceover and align it to the script
  analyze-assets  inspect assets and optionally analyze their visual content
  analyze-style   measure reference-video style facts
  plan     draft an edit timeline
  render   render the current timeline
  review   score and revise a render
  version  print the CLI version

Flags:
  -h, --help  show help

Init example:
  auto-editor init --name launch-video --script script.txt --voiceover voiceover.wav --asset assets --reference shorts=refs/shorts --reference documentary=refs/docs

Ingest example:
  auto-editor ingest --manifest auto-editor.json --config auto-editor.config.json

Align example:
  auto-editor align --index .auto-editor/index.json --config auto-editor.config.json
`, ctx.Name)
}
