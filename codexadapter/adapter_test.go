package codexadapter_test

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/hecatehq/acp-adapter-kit/acptest"
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

func TestPromptCommandUsesNativeCodexCLIOnly(t *testing.T) {
	got, err := codexadapter.PromptCommand(commandbridge.Session{
		CWD: "/work",
	}, runtimeacp.PromptParams{
		Prompt: []runtimeacp.ContentBlock{{Type: "text", Text: "hello codex"}},
	})
	if err != nil {
		t.Fatalf("PromptCommand: %v", err)
	}
	assertNoPackageRunnerCommand(t, got.Command)
	if got.Command != "codex" {
		t.Fatalf("process command = %q, want native codex CLI", got.Command)
	}

	spec := codexadapter.NewCLISpec("2.0.0", nil, nil, nil)
	if spec.Doctor == nil {
		t.Fatal("doctor spec is nil")
	}
	assertNoPackageRunnerCommand(t, spec.Doctor.Binary)
	if spec.Doctor.Binary != "codex" {
		t.Fatalf("doctor binary = %q, want native codex CLI", spec.Doctor.Binary)
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

func TestNewServerStreamsNativeCodexOutput(t *testing.T) {
	installFakeCommand(t, "codex", `
if [ "$1" != "exec" ]; then
  echo "unexpected command: $*" >&2
  exit 64
fi
printf 'chunk one '
sleep 0.05
printf 'chunk two'
`)
	client := acptest.NewClient(t, codexadapter.NewServer("test"))
	client.Request("initialize", map[string]any{})
	created := client.Request("session/new", map[string]any{"cwd": t.TempDir()})
	var session struct {
		SessionID string `json:"sessionId"`
	}
	created.ResultInto(t, &session)
	if session.SessionID == "" {
		t.Fatal("session id is empty")
	}

	responses := client.Send(map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "session/prompt",
		"params": map[string]any{
			"sessionId": session.SessionID,
			"prompt":    []map[string]any{{"type": "text", "text": "hello"}},
		},
	})
	if len(responses) < 2 {
		t.Fatalf("got %d responses, want streamed update(s) + prompt response: %#v", len(responses), responses)
	}
	var streamed strings.Builder
	for i, response := range responses[:len(responses)-1] {
		if response.Method != "session/update" {
			t.Fatalf("response %d method = %q, want session/update", i, response.Method)
		}
		var update struct {
			Update struct {
				SessionUpdate string `json:"sessionUpdate"`
				Content       struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"update"`
		}
		response.ParamsInto(t, &update)
		if update.Update.SessionUpdate != "agent_message_chunk" {
			t.Fatalf("response %d update = %#v, want agent_message_chunk", i, update.Update)
		}
		streamed.WriteString(update.Update.Content.Text)
	}
	if streamed.String() != "chunk one chunk two" {
		t.Fatalf("streamed text = %q, want chunked command stdout", streamed.String())
	}
	var promptResult struct {
		StopReason string `json:"stopReason"`
	}
	responses[len(responses)-1].ResultInto(t, &promptResult)
	if promptResult.StopReason != "end_turn" {
		t.Fatalf("stop reason = %q, want end_turn", promptResult.StopReason)
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

func installFakeCommand(t testing.TB, name string, body string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake command is Unix-only")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+body), 0o755); err != nil {
		t.Fatalf("write fake %s command: %v", name, err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func assertNoPackageRunnerCommand(t testing.TB, command string) {
	t.Helper()
	switch command {
	case "npx", "npm", "node", "bun", "sh", "bash", "zsh", "cmd", "powershell", "pwsh":
		t.Fatalf("command = %q, want fixed native CLI without package runner or shell", command)
	}
}
