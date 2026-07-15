package app

import "io"

type App struct {
	Name    string
	Version string
	Out     io.Writer
	Err     io.Writer
}

func New(name, version string, out, err io.Writer) App {
	return App{
		Name:    name,
		Version: version,
		Out:     out,
		Err:     err,
	}
}
