package app

import (
	"io"

	"github.com/hecatehq/acp-adapter-kit/adaptercli"
	"github.com/hecatehq/codex-acp-adapter/codexadapter"
)

const (
	Name  = codexadapter.Name
	Title = codexadapter.Title
)

var Version = "0.0.0-dev"

func Run(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	return adaptercli.Run(args, adapterSpec(stdin, stdout, stderr))
}

func adapterSpec(stdin io.Reader, stdout io.Writer, stderr io.Writer) adaptercli.Spec {
	return codexadapter.NewCLISpec(Version, stdin, stdout, stderr)
}
