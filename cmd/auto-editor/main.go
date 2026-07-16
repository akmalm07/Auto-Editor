package main

import (
	"fmt"
	"os"

	"auto-editor/internal/app"
	"auto-editor/internal/cli"
)

const version = "dev"

func main() {
	ctx := app.New("auto-editor", version, os.Stdout, os.Stderr)
	if err := cli.Run(os.Args[1:], ctx); err != nil {
		if err.Error() != "" {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
}
