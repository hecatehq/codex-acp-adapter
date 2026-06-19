package app

import (
	"fmt"
	"io"

	"github.com/hecatehq/acp-adapter-kit/acp"
	"github.com/hecatehq/acp-adapter-kit/adaptercli"
	"github.com/hecatehq/acp-adapter-kit/commandbridge"
	"github.com/hecatehq/acp-adapter-kit/doctor"
	adapterprocess "github.com/hecatehq/acp-adapter-kit/process"
	"github.com/hecatehq/acp-adapter-kit/runtimeacp"
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
		Command: &commandbridge.Spec{
			Options:     codexConfigOptions(),
			BuildPrompt: codexPromptCommand,
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

const configDefault = "__default__"

func codexConfigOptions() []commandbridge.SelectConfigOption {
	return []commandbridge.SelectConfigOption{
		{
			ID:           "model",
			Name:         "Model",
			Description:  "Codex CLI model override. Default uses the operator's Codex configuration.",
			Category:     "model",
			DefaultValue: configDefault,
			Options: []commandbridge.SelectValue{
				{Value: configDefault, Name: "Configured default"},
				{Value: "gpt-5", Name: "GPT-5"},
				{Value: "gpt-5-codex", Name: "GPT-5 Codex"},
				{Value: "o4-mini", Name: "o4-mini"},
			},
		},
		{
			ID:           "reasoning_effort",
			Name:         "Reasoning effort",
			Description:  "Codex CLI reasoning-effort override. Default uses the operator's Codex configuration.",
			Category:     "thought_level",
			DefaultValue: configDefault,
			Options: []commandbridge.SelectValue{
				{Value: configDefault, Name: "Configured default"},
				{Value: "minimal", Name: "Minimal"},
				{Value: "low", Name: "Low"},
				{Value: "medium", Name: "Medium"},
				{Value: "high", Name: "High"},
				{Value: "xhigh", Name: "xHigh"},
			},
		},
	}
}

func codexPromptCommand(session commandbridge.Session, params runtimeacp.PromptParams) (adapterprocess.Spec, error) {
	text, err := commandbridge.RequirePromptText(params)
	if err != nil {
		return adapterprocess.Spec{}, err
	}
	if session.CWD == "" {
		return adapterprocess.Spec{}, fmt.Errorf("session cwd is required")
	}
	args := []string{
		"exec",
		"--cd", session.CWD,
		"--sandbox", "workspace-write",
		"--ask-for-approval", "never",
		"--skip-git-repo-check",
	}
	for _, dir := range session.AdditionalDirectories {
		if dir != "" {
			args = append(args, "--add-dir", dir)
		}
	}
	if model := selectedConfig(session, "model"); model != "" {
		args = append(args, "--model", model)
	}
	if effort := selectedConfig(session, "reasoning_effort"); effort != "" {
		args = append(args, "--config", fmt.Sprintf("model_reasoning_effort=%q", effort))
	}
	args = append(args, text)
	return adapterprocess.Spec{
		Command: "codex",
		Args:    args,
		Dir:     session.CWD,
	}, nil
}

func selectedConfig(session commandbridge.Session, id string) string {
	value := session.Config[id]
	if value == "" || value == configDefault {
		return ""
	}
	return value
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
