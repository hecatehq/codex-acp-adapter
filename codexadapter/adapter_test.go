package codexadapter_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/hecatehq/acp-adapter-kit/commandbridge"
	"github.com/hecatehq/acp-adapter-kit/runtimeacp"
	"github.com/hecatehq/codex-acp-adapter/codexadapter"
)

func TestInfoPinsCodexCapabilities(t *testing.T) {
	info := codexadapter.Info("1.2.3")

	if info.Name != codexadapter.Name || info.Title != codexadapter.Title || info.Version != "1.2.3" {
		t.Fatalf("info = %#v, want Codex adapter metadata", info)
	}
	if !info.Capabilities.Images || !info.Capabilities.EmbeddedContext || !info.Capabilities.MCPHTTP || info.Capabilities.MCPSSE {
		t.Fatalf("capabilities = %#v, want image + embedded context + MCP HTTP only", info.Capabilities)
	}
	if codexadapter.NewServer("1.2.3") == nil {
		t.Fatal("NewServer returned nil")
	}
	if len(codexadapter.Options()) == 0 {
		t.Fatal("Options returned no ACP handlers")
	}
}

func TestNewCLISpecExposesLibraryContract(t *testing.T) {
	spec := codexadapter.NewCLISpec("2.0.0", nil, nil, nil)

	if spec.Info.Name != codexadapter.Name || spec.Info.Version != "2.0.0" {
		t.Fatalf("spec.Info = %#v", spec.Info)
	}
	if spec.Command == nil || spec.Command.BuildPrompt == nil || len(spec.Command.Options) != 2 {
		t.Fatalf("command spec = %#v, want command-backed bridge with config options", spec.Command)
	}
	if spec.Doctor == nil || spec.Doctor.Binary != "codex" {
		t.Fatalf("doctor spec = %#v, want codex doctor", spec.Doctor)
	}
	wantEnv := []string{"PATH", "HOME", "XDG_CONFIG_HOME", "TMPDIR", "CODEX_HOME", "OPENAI_API_KEY", "OPENAI_BASE_URL"}
	if !reflect.DeepEqual(spec.Runtime.InheritEnv, wantEnv) || !reflect.DeepEqual(codexadapter.RuntimeEnv(), wantEnv) {
		t.Fatalf("runtime env = %#v / %#v, want %#v", spec.Runtime.InheritEnv, codexadapter.RuntimeEnv(), wantEnv)
	}
}

func TestPromptCommandBuildsCodexExec(t *testing.T) {
	got, err := codexadapter.PromptCommand(commandbridge.Session{
		CWD:                   "/work",
		AdditionalDirectories: []string{"/extra", ""},
		Config: map[string]string{
			"model":            "gpt-5-codex",
			"reasoning_effort": "high",
		},
	}, runtimeacp.PromptParams{
		Prompt: []runtimeacp.ContentBlock{{Type: "text", Text: "hello codex"}},
	})
	if err != nil {
		t.Fatalf("PromptCommand: %v", err)
	}
	wantArgs := []string{
		"exec",
		"--cd", "/work",
		"--sandbox", "workspace-write",
		"--ask-for-approval", "never",
		"--skip-git-repo-check",
		"--add-dir", "/extra",
		"--model", "gpt-5-codex",
		"--config", `model_reasoning_effort="high"`,
		"hello codex",
	}
	if got.Command != "codex" || got.Dir != "/work" || !reflect.DeepEqual(got.Args, wantArgs) {
		t.Fatalf("process spec = %#v, want codex exec args %#v", got, wantArgs)
	}
}

func TestPromptCommandRequiresWorkspace(t *testing.T) {
	_, err := codexadapter.PromptCommand(commandbridge.Session{}, runtimeacp.PromptParams{
		Prompt: []runtimeacp.ContentBlock{{Type: "text", Text: "hello"}},
	})
	if err == nil || !strings.Contains(err.Error(), "session cwd is required") {
		t.Fatalf("PromptCommand error = %v, want cwd required", err)
	}
}
