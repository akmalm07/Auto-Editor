package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"auto-editor/internal/alignment"
	"auto-editor/internal/analyzeassets"
	"auto-editor/internal/analyzestyle"
	"auto-editor/internal/app"
	"auto-editor/internal/asr/openai"
	assetopenai "auto-editor/internal/assetvision/openai"
	"auto-editor/internal/config"
	"auto-editor/internal/ingest"
	"auto-editor/internal/plan"
	planopenai "auto-editor/internal/planopenai"
	"auto-editor/internal/project"
	"auto-editor/internal/projectexport"
	"auto-editor/internal/render"
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
	projectRoot, args, err := splitProjectFlag(args)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		printRootUsage(ctx)
		return nil
	}
	if projectRoot == "" && commandUsesProject(args[0]) {
		root, found, err := findProjectRoot(".")
		if err != nil {
			return err
		}
		if found {
			projectRoot = root
		}
	}
	if projectRoot != "" {
		return inProjectDirectory(projectRoot, func() error { return runCommand(args, ctx) })
	}
	return runCommand(args, ctx)
}

func runCommand(args []string, ctx app.App) error {
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
	case "status":
		return runStatus(args[1:], ctx)
	case "workflow-context":
		return runWorkflowContext(args[1:], ctx)
	case "render":
		return runRender(args[1:], ctx)
	case "export-project":
		return runExportProject(args[1:], ctx)
	case "review":
		return runPlaceholder("review", ctx)
	default:
		printRootUsage(ctx)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func newLoadingSession(ctx app.App, cfg config.Config) *loading.Session {
	return loading.New(ctx.Out, loading.Settings{
		Enabled:    cfg.UI.Loading.Enabled,
		Style:      cfg.UI.Loading.Style,
		IntervalMS: cfg.UI.Loading.IntervalMS,
		TTY:        os.Stdout,
	})
}

func runAnalyzeStyle(args []string, ctx app.App) error {
	fs := flag.NewFlagSet("analyze-style", flag.ContinueOnError)
	fs.SetOutput(ctx.Err)
	indexPath := fs.String("index", ingest.DefaultIndexPath, "ingest index path")
	configPath := fs.String("config", config.DefaultConfigName, "config file path")
	outputPath := fs.String("out", analyzestyle.DefaultStylePath, "style analysis output path")
	noSemantic := fs.Bool("no-semantic", false, "disable semantic style analysis for this run")
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
	deps := analyzestyle.Dependencies{}
	semanticEnabled := !*noSemantic && cfg.Style.SemanticProvider == "openai"
	if semanticEnabled {
		apiKey, ok := os.LookupEnv(cfg.Style.APIKeyEnvironment)
		if !ok || strings.TrimSpace(apiKey) == "" {
			semanticEnabled = false
			fmt.Fprintf(ctx.Err, "warning: style semantic analysis skipped because environment variable %s is not set\n", cfg.Style.APIKeyEnvironment)
		} else {
			provider := assetopenai.Client{APIKey: apiKey, Model: cfg.Style.SemanticModel, ImageDetail: cfg.Style.SemanticImageDetail, MaxAttempts: cfg.Style.SemanticMaxAttempts, Timeout: time.Duration(cfg.Style.SemanticTimeoutSeconds) * time.Second}
			deps.SemanticAnalyzer = assetopenai.StyleClient{Client: provider}
			if cfg.Style.SemanticCache {
				cache, err := analyzestyle.LoadSemanticCache(analyzestyle.DefaultSemanticCachePath(*indexPath))
				if err != nil {
					fmt.Fprintf(ctx.Err, "warning: style semantic cache unavailable: %v\n", err)
				} else {
					deps.SemanticAnalyzer = analyzestyle.CachedSemanticAnalyzer{Analyzer: deps.SemanticAnalyzer, Cache: cache, Scope: (assetopenai.StyleClient{Client: provider}).CacheScope()}
				}
			}
		}
	}
	session := newLoadingSession(ctx, cfg)
	session.Start("Preparing style analysis")
	result, err := analyzestyle.Build(context.Background(), analyzestyle.Options{IndexPath: *indexPath, FFprobe: cfg.Assets.FFprobe, FFmpeg: cfg.Assets.FFmpeg, SemanticEnabled: semanticEnabled, SemanticMaxFrames: cfg.Style.SemanticMaxFrames}, deps, func(current, total int, message string) {
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
	semanticComplete := 0
	for _, set := range result.ReferenceSets {
		if set.Semantic.Status == "complete" {
			semanticComplete++
		}
	}
	if semanticComplete > 0 {
		ui.Success(fmt.Sprintf("completed semantic style analysis for %d reference set(s)", semanticComplete))
	} else if !*noSemantic && cfg.Style.SemanticProvider == "openai" {
		fmt.Fprintln(ctx.Err, "warning: semantic style analysis did not complete; see diagnostics")
	}
	ui.Success(fmt.Sprintf("created %s", filepath.Clean(resolvedOutputPath)))
	return nil
}

func runAnalyzeAssets(args []string, ctx app.App) error {
	fs := flag.NewFlagSet("analyze-assets", flag.ContinueOnError)
	fs.SetOutput(ctx.Err)
	indexPath := fs.String("index", ingest.DefaultIndexPath, "ingest index path")
	configPath := fs.String("config", config.DefaultConfigName, "config file path")
	outputPath := fs.String("out", analyzeassets.DefaultAssetsPath, "asset analysis output path")
	noSemantic := fs.Bool("no-semantic", false, "disable semantic asset analysis for this run")
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
	session := newLoadingSession(ctx, cfg)
	deps := analyzeassets.Dependencies{}
	semanticEnabled := !*noSemantic && cfg.Assets.SemanticProvider == "openai"
	if semanticEnabled {
		apiKey, ok := os.LookupEnv(cfg.Assets.APIKeyEnvironment)
		if !ok || strings.TrimSpace(apiKey) == "" {
			semanticEnabled = false
			fmt.Fprintf(ctx.Err, "warning: semantic analysis skipped because environment variable %s is not set\n", cfg.Assets.APIKeyEnvironment)
		} else {
			provider := assetopenai.Client{
				APIKey:      apiKey,
				Model:       cfg.Assets.SemanticModel,
				ImageDetail: cfg.Assets.SemanticImageDetail,
				MaxAttempts: cfg.Assets.SemanticMaxAttempts,
				Timeout:     time.Duration(cfg.Assets.SemanticTimeoutSeconds) * time.Second,
			}
			deps.SemanticAnalyzer = provider
			if cfg.Assets.SemanticCache {
				cache, err := analyzeassets.LoadSemanticCache(analyzeassets.DefaultSemanticCachePath(*indexPath))
				if err != nil {
					fmt.Fprintf(ctx.Err, "warning: semantic cache unavailable: %v\n", err)
				} else {
					deps.SemanticAnalyzer = analyzeassets.CachedSemanticAnalyzer{Analyzer: provider, Cache: cache, Scope: provider.CacheScope()}
				}
			}
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
	} else if *noSemantic {
		ui.Success("skipped semantic asset analysis (--no-semantic)")
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
	var music stringListFlag
	var references stringListFlag
	fs.Var(&assets, "asset", "asset file or directory path; may be repeated")
	fs.Var(&music, "music", "background music file or directory path; may be repeated")
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
	manifest.Inputs.Music = music

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
	session := newLoadingSession(ctx, cfg)

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
	session := newLoadingSession(ctx, cfg)
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
	configPath := fs.String("config", config.DefaultConfigName, "config file path")
	outputPath := fs.String("out", plan.DefaultPlanPath, "plan output path")
	aspect := fs.String("aspect", "16:9", "output aspect ratio")
	noSemantic := fs.Bool("no-semantic", false, "disable semantic visual beat planning for this run")
	allAssets := fs.Bool("all-assets", false, "require the plan to use every visual and audio cue asset")
	allViewable := fs.Bool("all-viewable-assets", false, "require every image and video asset")
	allAudio := fs.Bool("all-audio-assets", false, "include every audio sound-effect asset")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Read(*configPath)
	if err != nil {
		return err
	}
	session := newLoadingSession(ctx, cfg)
	deps := plan.Dependencies{}
	semanticEnabled := !*noSemantic && cfg.Planning.SemanticProvider == "openai"
	if semanticEnabled {
		apiKey, ok := os.LookupEnv(cfg.Planning.APIKeyEnvironment)
		if !ok || strings.TrimSpace(apiKey) == "" {
			semanticEnabled = false
			fmt.Fprintf(ctx.Err, "warning: semantic planning skipped because environment variable %s is not set\n", cfg.Planning.APIKeyEnvironment)
		} else {
			deps.SemanticPlanner = planopenai.Client{Client: assetopenai.Client{APIKey: apiKey, Model: cfg.Planning.SemanticModel, MaxAttempts: cfg.Planning.SemanticMaxAttempts, Timeout: time.Duration(cfg.Planning.SemanticTimeoutSeconds) * time.Second}}
		}
	}
	session.Start("Preparing edit plan")
	result, err := plan.BuildWithDependencies(context.Background(), plan.Options{IndexPath: *indexPath, AlignmentPath: filepath.Join(filepath.Dir(*indexPath), "alignment.json"), AssetsPath: *assetsPath, StylePath: *stylePath, AspectRatio: *aspect, SemanticEnabled: semanticEnabled, AllAssets: *allAssets, AllViewableAssets: *allViewable, AllAudioAssets: *allAudio}, deps, func(current, total int, message string) {
		session.Update(fmt.Sprintf("[%d/%d] %s", current, total, message))
	})
	if err != nil {
		session.Stop("")
		return err
	}
	out, err := resolveAlignmentOutputPath(*indexPath, *outputPath)
	if err != nil {
		session.Stop("")
		return err
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

func runRender(args []string, ctx app.App) error {
	fs := flag.NewFlagSet("render", flag.ContinueOnError)
	fs.SetOutput(ctx.Err)
	planPath := fs.String("plan", plan.DefaultPlanPath, "edit plan path")
	configPath := fs.String("config", config.DefaultConfigName, "config file path")
	outputPath := fs.String("out", "output/render.mp4", "final MP4 output path")
	reportPath := fs.String("report", render.DefaultReportPath, "render report output path")
	noCaptions := fs.Bool("no-captions", false, "do not burn captions into this render")
	force := fs.Bool("force", false, "replace an existing output file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("render does not accept positional arguments")
	}
	if *planPath == "" || *configPath == "" || *outputPath == "" || *reportPath == "" {
		return fmt.Errorf("plan, config, output, and report paths cannot be empty")
	}
	cfg, err := config.Read(*configPath)
	if err != nil {
		return err
	}
	settings := cfg.Render
	if *noCaptions {
		settings.Captions.Enabled = false
	}
	planAbsolute, err := filepath.Abs(*planPath)
	if err != nil {
		return err
	}
	root := filepath.Dir(planAbsolute)
	if filepath.Base(root) == ".auto-editor" {
		root = filepath.Dir(root)
	}
	resolve := func(path string) string {
		if filepath.IsAbs(path) {
			return filepath.Clean(path)
		}
		return filepath.Join(root, path)
	}
	ui := newPresenter(ctx.Out, ctx.Err, cfg)
	session := newLoadingSession(ctx, cfg)
	session.Start("Preparing render")
	result, err := render.Build(context.Background(), render.Options{PlanPath: planAbsolute, OutputPath: resolve(*outputPath), ReportPath: resolve(*reportPath), Force: *force, FFprobe: settings.FFprobe, Settings: settings}, render.Dependencies{}, func(current, total int, message string) {
		session.Update(fmt.Sprintf("[%d/%d] %s", current, total, message))
	})
	if err != nil {
		session.Stop("")
		return err
	}
	session.Stop("")
	ui.Success(fmt.Sprintf("rendered %s", filepath.Clean(result.OutputPath)))
	ui.Success(fmt.Sprintf("created %s", filepath.Clean(resolve(*reportPath))))
	return nil
}

func runPlaceholder(name string, ctx app.App) error {
	fmt.Fprintf(ctx.Out, "%s is scaffolded but not implemented yet\n", strings.ToLower(name))
	return nil
}

func runExportProject(args []string, ctx app.App) error {
	fs := flag.NewFlagSet("export-project", flag.ContinueOnError)
	fs.SetOutput(ctx.Err)
	planPath := fs.String("plan", plan.DefaultPlanPath, "edit plan path")
	configPath := fs.String("config", config.DefaultConfigName, "config file path")
	outputDir := fs.String("out", "output/project", "exported editor project directory")
	format := fs.String("format", "xml", "export format (currently FCPXML XML)")
	kdenlive := fs.Bool("kdenlive", false, "generate a native-style Kdenlive MLT project")
	copyAssets := fs.Bool("copy-assets", true, "copy source media into the exported project")
	linkAssets := fs.Bool("link-assets", false, "link original source media instead of copying")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *kdenlive {
		*format = "kdenlive"
	}
	if *format != "xml" && *format != "fcpxml" && *format != "kdenlive" {
		return fmt.Errorf("unsupported export format %q; currently supported: xml", *format)
	}
	cfg, err := config.Read(*configPath)
	if err != nil {
		return err
	}
	copy := *copyAssets
	if *linkAssets {
		copy = false
	}
	result, err := projectexport.Export(projectexport.Options{PlanPath: *planPath, OutputDir: *outputDir, CopyAssets: copy, Format: *format, Width: cfg.Render.Width, Height: cfg.Render.Height, FPS: cfg.Render.FPS})
	if err != nil {
		return err
	}
	fmt.Fprintf(ctx.Out, "ok: exported %s project to %s (%d source asset(s))\n", *format, filepath.Clean(result.OutputDir), result.Copied)
	return nil
}

func printRootUsage(ctx app.App) {
	fmt.Fprintf(ctx.Out, `%s

Usage:
  auto-editor [--project <directory>] <command> [flags]

Commands:
  init     create a project manifest
  ingest   validate project inputs and write an index
  align    transcribe voiceover and align it to the script
  analyze-assets  inspect assets and optionally analyze their visual content
  analyze-style   measure reference-video style facts
  plan     draft an edit timeline
  status   show project artifacts and the next command
  render   render the current timeline
  export-project  export an editable XML or Kdenlive project package
  review   score and revise a render
  version  print the CLI version

Flags:
  -h, --help  show help

Init example:
  auto-editor init --name launch-video --script script.txt --voiceover voiceover.wav --asset assets --reference shorts=refs/shorts --reference documentary=refs/docs

Project commands infer the nearest auto-editor.json. Run them from a project folder,
or pass --project C:\path\to\project before the command.

Ingest example:
  auto-editor ingest

Align example:
  auto-editor align

Analyze-assets example:
  auto-editor analyze-assets

Add --no-semantic to skip semantic asset analysis for one run.

Analyze-style example (requires reference videos in the manifest):
  auto-editor analyze-style

Add --no-semantic to skip semantic style analysis for one run.

Plan example:
  auto-editor plan

Render example:
  auto-editor render --out output/render.mp4

Use --no-semantic with analyze-assets or analyze-style to skip AI analysis.
`, ctx.Name)
}
