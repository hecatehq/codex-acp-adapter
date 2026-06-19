package app

import (
	"io"

	"github.com/hecatehq/acp-adapter-kit/acp"
	"github.com/hecatehq/acp-adapter-kit/adaptercli"
	"github.com/hecatehq/acp-adapter-kit/doctor"
)

const (
	Name  = "codex-acp-adapter"
	Title = "Codex ACP Adapter"
)

var Version = "0.0.0-dev"

func Run(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	return adaptercli.Run(args, adapterSpec(stdin, stdout, stderr))
}

func adapterSpec(stdin io.Reader, stdout io.Writer, stderr io.Writer) adaptercli.Spec {
	return adaptercli.Spec{
		Info:   adapterInfo(),
		Short:  "ACP adapter for Codex-compatible coding agents",
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
		Runtime: adaptercli.RuntimeSpec{
			InheritEnv: []string{
				"PATH",
				"HOME",
				"XDG_CONFIG_HOME",
				"TMPDIR",
				"CODEX_HOME",
				"OPENAI_API_KEY",
				"OPENAI_BASE_URL",
			},
		},
		Doctor: &adaptercli.DoctorSpec{
			Short:       "Check the local Codex runtime boundary",
			Binary:      "codex",
			VersionArgs: []string{"--version"},
			InheritEnv: []string{
				"PATH",
				"HOME",
				"XDG_CONFIG_HOME",
				"TMPDIR",
			},
			EnvVars: []doctor.EnvVar{
				{Name: "CODEX_HOME"},
				{Name: "OPENAI_API_KEY"},
				{Name: "OPENAI_BASE_URL"},
			},
		},
	}
}

func adapterInfo() acp.AdapterInfo {
	return acp.AdapterInfo{
		Name:    Name,
		Title:   Title,
		Version: Version,
		Capabilities: acp.Capabilities{
			Images:          true,
			EmbeddedContext: true,
			MCPHTTP:         true,
		},
	}
}
