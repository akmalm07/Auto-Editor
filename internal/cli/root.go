package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"auto-editor/internal/app"
	"auto-editor/internal/project"
)

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
	case "plan":
		return runPlaceholder("plan", ctx)
	case "render":
		return runPlaceholder("render", ctx)
	case "review":
		return runPlaceholder("review", ctx)
	default:
		printRootUsage(ctx)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runInit(args []string, ctx app.App) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(ctx.Err)

	name := fs.String("name", "new-project", "project name")
	manifestPath := fs.String("manifest", project.DefaultManifestName, "manifest file path")
	aspectRatio := fs.String("aspect", "16:9", "default aspect ratio")
	language := fs.String("language", "en", "default language")
	runtime := fs.String("runtime", "60s", "target runtime")
	force := fs.Bool("force", false, "overwrite an existing manifest")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *manifestPath == "" {
		return fmt.Errorf("manifest path cannot be empty")
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

	if err := project.WriteManifest(*manifestPath, manifest); err != nil {
		return err
	}

	fmt.Fprintf(ctx.Out, "created %s\n", filepath.Clean(*manifestPath))
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
  plan     draft an edit timeline
  render   render the current timeline
  review   score and revise a render
  version  print the CLI version

Flags:
  -h, --help  show help
`, ctx.Name)
}
