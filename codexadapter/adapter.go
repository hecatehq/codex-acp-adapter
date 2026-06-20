// Package codexadapter exposes the Codex-specific ACP adapter wiring for hosts
// that want to embed the adapter as a library instead of launching the
// codex-acp-adapter binary.
package codexadapter

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"

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
const authMethodAgentLogin = "agent-login"

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
		Commands:          AvailableCommands(),
		AuthMethods:       AuthMethods(),
		IncludeTranscript: true,
		BuildPrompt:       PromptCommand,
		BuildAuthenticate: AuthenticateCommand,
		BuildLogout:       LogoutCommand,
		AuthRequired:      CommandAuthRequired,
		NewStreamParser:   NewStreamParser,
	}
}

func AuthMethods() []acp.AuthMethod {
	return []acp.AuthMethod{{
		ID:          authMethodAgentLogin,
		Name:        "Codex login",
		Description: "Sign in with the local Codex CLI.",
	}}
}

func AvailableCommands() []commandbridge.AvailableCommand {
	return []commandbridge.AvailableCommand{
		{
			Name:        "review",
			Description: "Review uncommitted workspace changes with Codex.",
			InputHint:   "optional review instructions",
		},
		{
			Name:        "init",
			Description: "Ask Codex to inspect the workspace and create or update agent instructions.",
			InputHint:   "optional instruction focus",
		},
	}
}

func ConfigOptions() []commandbridge.SelectConfigOption {
	return []commandbridge.SelectConfigOption{
		{
			ID:           "model",
			Name:         "Model",
			Description:  "Codex CLI model override. Default uses the Codex CLI default.",
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
			Description:  "Codex CLI reasoning-effort override. Default uses the Codex CLI default.",
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
		{
			ID:           "web_search",
			Name:         "Web search",
			Description:  "Enable Codex CLI live web search for normal exec turns.",
			Category:     "tool",
			DefaultValue: "disabled",
			Options: []commandbridge.SelectValue{
				{Value: "disabled", Name: "Disabled"},
				{Value: "enabled", Name: "Enabled"},
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
	if instructions, ok := reviewCommandInstructions(text); ok {
		return codexReviewCommand(session, instructions)
	}
	args := []string{
		"--ask-for-approval", "never",
	}
	if selectedConfig(session, "web_search") == "enabled" {
		args = append(args, "--search")
	}
	args = append(args,
		"exec",
		"--cd", session.CWD,
		"--sandbox", selectedSandbox(session),
		"--ignore-user-config",
		"--skip-git-repo-check",
		"--json",
	)
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
	mcpArgs, err := codexMCPConfigArgs(session.MCPServers)
	if err != nil {
		return adapterprocess.Spec{}, err
	}
	args = append(args, mcpArgs...)
	args = append(args, text)
	return adapterprocess.Spec{
		Command: "codex",
		Args:    args,
		Dir:     session.CWD,
		Env:     codexProcessEnv(),
	}, nil
}

func LogoutCommand() (adapterprocess.Spec, error) {
	dir, err := os.Getwd()
	if err != nil {
		return adapterprocess.Spec{}, err
	}
	return adapterprocess.Spec{
		Command: "codex",
		Args:    []string{"logout"},
		Dir:     dir,
		Env:     codexProcessEnv(),
	}, nil
}

func AuthenticateCommand(methodID string) (adapterprocess.Spec, error) {
	if strings.TrimSpace(methodID) != authMethodAgentLogin {
		return adapterprocess.Spec{}, fmt.Errorf("unsupported auth method %q", methodID)
	}
	dir, err := os.Getwd()
	if err != nil {
		return adapterprocess.Spec{}, err
	}
	return adapterprocess.Spec{
		Command: "codex",
		Args:    []string{"login"},
		Dir:     dir,
		Env:     codexProcessEnv(),
	}, nil
}

func CommandAuthRequired(result adapterprocess.Result, err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(strings.Join([]string{
		err.Error(),
		string(result.Stderr),
		string(result.Stdout),
	}, "\n"))
	for _, marker := range []string{
		"authentication required",
		"auth required",
		"not authenticated",
		"not signed in",
		"not logged in",
		"please log in",
		"please login",
		"run codex login",
		"codex login",
		"openai_api_key",
		"api key",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func codexReviewCommand(session commandbridge.Session, instructions string) (adapterprocess.Spec, error) {
	args := []string{
		"review",
		"--uncommitted",
	}
	if model := selectedConfig(session, "model"); model != "" {
		args = append(args, "--config", fmt.Sprintf("model=%q", model))
	}
	if effort := selectedConfig(session, "reasoning_effort"); effort != "" {
		args = append(args, "--config", fmt.Sprintf("model_reasoning_effort=%q", effort))
	}
	mcpArgs, err := codexMCPConfigArgs(session.MCPServers)
	if err != nil {
		return adapterprocess.Spec{}, err
	}
	args = append(args, mcpArgs...)
	if instructions = strings.TrimSpace(instructions); instructions != "" {
		args = append(args, instructions)
	}
	return adapterprocess.Spec{
		Command: "codex",
		Args:    args,
		Dir:     session.CWD,
		Env:     codexProcessEnv(),
	}, nil
}

func codexMCPConfigArgs(servers []runtimeacp.MCPServer) ([]string, error) {
	var args []string
	for i, server := range servers {
		key := codexMCPServerKey(i, server)
		value, err := codexMCPServerValue(server)
		if err != nil {
			return nil, err
		}
		args = append(args, "--config", "mcp_servers."+key+"="+value)
	}
	return args, nil
}

func codexMCPServerValue(server runtimeacp.MCPServer) (string, error) {
	name := strings.TrimSpace(firstNonEmpty(server.Name, server.ID))
	if name == "" {
		name = "unnamed"
	}
	if strings.TrimSpace(server.URL) != "" {
		if strings.TrimSpace(server.Command) != "" {
			return "", fmt.Errorf("mcp server %q cannot set both url and command", name)
		}
		fields := []string{"url=" + tomlString(strings.TrimSpace(server.URL))}
		if len(server.Headers) != 0 {
			fields = append(fields, "http_headers="+tomlStringMap(httpHeadersMap(server.Headers)))
		}
		return "{" + strings.Join(fields, ",") + "}", nil
	}
	if strings.TrimSpace(server.Command) == "" {
		return "", fmt.Errorf("mcp server %q must set url or command", name)
	}
	fields := []string{"command=" + tomlString(strings.TrimSpace(server.Command))}
	if len(server.Args) != 0 {
		fields = append(fields, "args="+tomlStringArray(server.Args))
	}
	if len(server.Env) != 0 {
		fields = append(fields, "env="+tomlStringMap(envVariableMap(server.Env)))
	}
	return "{" + strings.Join(fields, ",") + "}", nil
}

var codexMCPKeyChars = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

func codexMCPServerKey(index int, server runtimeacp.MCPServer) string {
	base := strings.TrimSpace(firstNonEmpty(server.ID, server.Name))
	base = codexMCPKeyChars.ReplaceAllString(base, "_")
	base = strings.Trim(base, "_-")
	if base == "" {
		base = "server"
	}
	return fmt.Sprintf("hecate_%02d_%s", index+1, base)
}

func httpHeadersMap(headers []runtimeacp.HTTPHeader) map[string]string {
	values := make(map[string]string, len(headers))
	for _, header := range headers {
		name := strings.TrimSpace(header.Name)
		if name != "" {
			values[name] = header.Value
		}
	}
	return values
}

func envVariableMap(env []runtimeacp.EnvVariable) map[string]string {
	values := make(map[string]string, len(env))
	for _, item := range env {
		name := strings.TrimSpace(item.Name)
		if name != "" {
			values[name] = item.Value
		}
	}
	return values
}

func tomlStringArray(values []string) string {
	encoded := make([]string, 0, len(values))
	for _, value := range values {
		encoded = append(encoded, tomlString(value))
	}
	return "[" + strings.Join(encoded, ",") + "]"
}

func tomlStringMap(values map[string]string) string {
	if len(values) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	fields := make([]string, 0, len(keys))
	for _, key := range keys {
		fields = append(fields, tomlString(key)+"="+tomlString(values[key]))
	}
	return "{" + strings.Join(fields, ",") + "}"
}

func tomlString(value string) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

func reviewCommandInstructions(text string) (string, bool) {
	current := strings.TrimSpace(currentPromptText(text))
	if current != "/review" && !strings.HasPrefix(current, "/review ") && !strings.HasPrefix(current, "/review\n") && !strings.HasPrefix(current, "/review\t") {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(current, "/review")), true
}

func currentPromptText(text string) string {
	const marker = "Current user request:\n"
	if idx := strings.LastIndex(text, marker); idx >= 0 {
		return text[idx+len(marker):]
	}
	return text
}

func RuntimeEnv() []string {
	return []string{
		"PATH",
		"HOME",
		"USER",
		"LOGNAME",
		"XDG_CONFIG_HOME",
		"TMPDIR",
		"CODEX_HOME",
		"OPENAI_API_KEY",
		"OPENAI_BASE_URL",
	}
}

func codexProcessEnv() adapterprocess.EnvPolicy {
	return adapterprocess.EnvPolicy{Inherit: RuntimeEnv()}
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
