// Package codexadapter exposes the Codex-specific ACP adapter wiring for hosts
// that want to embed the adapter as a library instead of launching the
// codex-acp-adapter binary.
package codexadapter

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

const configDefault = "__default__"

func NewCLISpec(version string, stdin io.Reader, stdout io.Writer, stderr io.Writer) adaptercli.Spec {
	return adaptercli.Spec{
		Info:   Info(version),
		Short:  "ACP adapter for Codex-compatible coding agents",
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
		Runtime: adaptercli.RuntimeSpec{
			InheritEnv: RuntimeEnv(),
		},
		Command: CommandSpec(),
		Doctor:  DoctorSpec(),
	}
}

func Info(version string) acp.AdapterInfo {
	return acp.AdapterInfo{
		Name:    Name,
		Title:   Title,
		Version: version,
		Capabilities: acp.Capabilities{
			Images:          true,
			EmbeddedContext: true,
			MCPHTTP:         true,
			LoadSession:     true,
		},
	}
}

func NewServer(version string) *acp.Server {
	return acp.NewServer(Info(version), Options()...)
}

func Options() []acp.Option {
	return commandbridge.New(*CommandSpec()).Options()
}

func CommandSpec() *commandbridge.Spec {
	return &commandbridge.Spec{
		Options:           ConfigOptions(),
		IncludeTranscript: true,
		BuildPrompt:       PromptCommand,
		NewStreamParser:   NewStreamParser,
	}
}

func ConfigOptions() []commandbridge.SelectConfigOption {
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
		{
			ID:           "sandbox",
			Name:         "Sandbox",
			Description:  "Codex CLI sandbox policy. Default matches the adapter's workspace-write boundary.",
			Category:     "permission",
			DefaultValue: "workspace-write",
			Options: []commandbridge.SelectValue{
				{Value: "read-only", Name: "Read only"},
				{Value: "workspace-write", Name: "Workspace write"},
				{Value: "danger-full-access", Name: "Full access"},
			},
		},
	}
}

func PromptCommand(session commandbridge.Session, params runtimeacp.PromptParams) (adapterprocess.Spec, error) {
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
		"--sandbox", selectedSandbox(session),
		"--ask-for-approval", "never",
		"--skip-git-repo-check",
		"--json",
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

func RuntimeEnv() []string {
	return []string{
		"PATH",
		"HOME",
		"XDG_CONFIG_HOME",
		"TMPDIR",
		"CODEX_HOME",
		"OPENAI_API_KEY",
		"OPENAI_BASE_URL",
	}
}

func DoctorSpec() *adaptercli.DoctorSpec {
	return &adaptercli.DoctorSpec{
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
	}
}

func selectedConfig(session commandbridge.Session, id string) string {
	value := session.Config[id]
	if value == "" || value == configDefault {
		return ""
	}
	return value
}

func selectedSandbox(session commandbridge.Session) string {
	if value := selectedConfig(session, "sandbox"); value != "" {
		return value
	}
	return "workspace-write"
}
