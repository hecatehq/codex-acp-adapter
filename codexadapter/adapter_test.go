package codexadapter_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/hecatehq/acp-adapter-kit/acptest"
	"github.com/hecatehq/acp-adapter-kit/adaptertest"
	"github.com/hecatehq/acp-adapter-kit/commandbridge"
	adapterprocess "github.com/hecatehq/acp-adapter-kit/process"
	"github.com/hecatehq/acp-adapter-kit/runtimeacp"
	"github.com/hecatehq/codex-acp-adapter/codexadapter"
)

func TestInfoPinsCodexCapabilities(t *testing.T) {
	info := codexadapter.Info("1.2.3")

	if info.Name != codexadapter.Name || info.Title != codexadapter.Title || info.Version != "1.2.3" {
		t.Fatalf("info = %#v, want Codex adapter metadata", info)
	}
	if info.Capabilities.Images ||
		info.Capabilities.EmbeddedContext ||
		!info.Capabilities.MCPHTTP ||
		info.Capabilities.MCPSSE ||
		!info.Capabilities.LoadSession ||
		!info.Capabilities.SessionList ||
		!info.Capabilities.SessionResume ||
		!info.Capabilities.SessionClose ||
		!info.Capabilities.SessionDelete ||
		!info.Capabilities.AdditionalDirectories {
		t.Fatalf("capabilities = %#v, want Codex stable ACP surface", info.Capabilities)
	}
	if codexadapter.NewServer("1.2.3") == nil {
		t.Fatal("NewServer returned nil")
	}
	if len(codexadapter.Options()) == 0 {
		t.Fatal("Options returned no ACP handlers")
	}
}

func TestInitializeAdvertisesLoadSession(t *testing.T) {
	adaptertest.AssertInitializeContract(t, codexadapter.NewServer("test"), adaptertest.InitializeContract{
		Name:                  codexadapter.Name,
		Title:                 codexadapter.Title,
		Version:               "test",
		Images:                false,
		EmbeddedContext:       false,
		MCPHTTP:               true,
		MCPSSE:                false,
		LoadSession:           true,
		SessionList:           true,
		SessionResume:         true,
		SessionClose:          true,
		SessionDelete:         true,
		AdditionalDirectories: true,
		Logout:                true,
		AuthMethodIDs:         []string{"agent-login"},
	})
}

func TestNewServerExposesHecateControls(t *testing.T) {
	adaptertest.AssertSessionBootstrapContract(t, codexadapter.NewServer("test"), adaptertest.SessionBootstrapContract{
		CWD: t.TempDir(),
		ConfigOptions: []adaptertest.ConfigOptionContract{
			{ID: "model", Category: "model", CurrentValue: "__default__"},
			{ID: "reasoning_effort", Category: "thought_level", CurrentValue: "__default__"},
			{ID: "sandbox", Category: "permission", CurrentValue: "workspace-write"},
			{ID: "approval_policy", Category: "permission", CurrentValue: "__default__"},
			{ID: "web_search", Category: "tool", CurrentValue: "disabled"},
		},
		AvailableCommands: []string{"review", "init"},
	})
}

func TestNewServerDefaultSessionIDsRemainDistinctAcrossRestarts(t *testing.T) {
	newSession := func(t *testing.T, client *acptest.Client) string {
		t.Helper()
		responses := client.Send(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "session/new",
			"params":  map[string]any{"cwd": t.TempDir()},
		})
		if len(responses) != 2 {
			t.Fatalf("session/new responses = %#v, want available commands + result", responses)
		}
		var created struct {
			SessionID string `json:"sessionId"`
		}
		responses[1].ResultInto(t, &created)
		assertRestartSafeSessionID(t, created.SessionID)
		return created.SessionID
	}

	firstClient := acptest.NewClient(t, codexadapter.NewServer("test"))
	first := newSession(t, firstClient)
	second := newSession(t, acptest.NewClient(t, codexadapter.NewServer("test")))
	if first == second {
		t.Fatalf("independent adapter processes returned the same session id %q", first)
	}

	forkResponses := firstClient.Send(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "session/fork",
		"params":  map[string]any{"sessionId": first},
	})
	if len(forkResponses) != 2 {
		t.Fatalf("session/fork responses = %#v, want available commands + result", forkResponses)
	}
	var forked struct {
		SessionID string `json:"sessionId"`
	}
	forkResponses[1].ResultInto(t, &forked)
	assertRestartSafeSessionID(t, forked.SessionID)
	if forked.SessionID == first {
		t.Fatalf("fork reused source session id %q", first)
	}
}

func assertRestartSafeSessionID(t testing.TB, sessionID string) {
	t.Helper()
	encoded := strings.TrimPrefix(sessionID, "session-")
	if encoded == sessionID || len(encoded) != 32 {
		t.Fatalf("session id = %q, want session- plus 128 bits", sessionID)
	}
	if _, err := hex.DecodeString(encoded); err != nil {
		t.Fatalf("session id = %q, want hexadecimal entropy: %v", sessionID, err)
	}
}

func TestNewServerCloseSessionFreesCommandBridgeState(t *testing.T) {
	client := acptest.NewClient(t, codexadapter.NewServer("test"))
	responses := client.Send(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "session/new",
		"params":  map[string]any{"cwd": t.TempDir()},
	})
	if len(responses) != 2 {
		t.Fatalf("responses = %#v, want available command update + session response", responses)
	}
	var created struct {
		SessionID string `json:"sessionId"`
	}
	responses[1].ResultInto(t, &created)

	closeResp := client.Request("session/close", map[string]any{"sessionId": created.SessionID})
	var closeResult map[string]any
	closeResp.ResultInto(t, &closeResult)

	listResp := client.Request("session/list", map[string]any{})
	var list struct {
		Sessions []struct {
			SessionID string `json:"sessionId"`
		} `json:"sessions"`
	}
	listResp.ResultInto(t, &list)
	if len(list.Sessions) != 0 {
		t.Fatalf("sessions after close = %#v, want closed session removed", list.Sessions)
	}

	promptResp := client.Request("session/prompt", map[string]any{
		"sessionId": created.SessionID,
		"prompt":    []map[string]any{{"type": "text", "text": "after close"}},
	})
	if promptResp.Error == nil || promptResp.Error.Code != -32001 || promptResp.Error.Message != "session not found" {
		t.Fatalf("prompt after close error = %#v, want session not found", promptResp.Error)
	}
}

func TestNewServerMatchesPortableUpstreamParity(t *testing.T) {
	adaptertest.AssertUpstreamParityContract(t, codexadapter.NewServer("test"), adaptertest.UpstreamParityContract{
		CWD:          t.TempDir(),
		AuthMethodID: "agent-login",
		ConfigChange: adaptertest.ConfigChangeContract{
			ID:    "model",
			Value: "gpt-5-codex",
		},
		LoadUnknownSession: adaptertest.LoadUnknownSessionContract{
			SessionID: "upstream-codex-missing-session",
			CWD:       t.TempDir(),
			Allowed:   false,
		},
	})
}

func TestNewCLISpecExposesLibraryContract(t *testing.T) {
	spec := codexadapter.NewCLISpec("2.0.0", nil, nil, nil)

	if spec.Info.Name != codexadapter.Name || spec.Info.Version != "2.0.0" {
		t.Fatalf("spec.Info = %#v", spec.Info)
	}
	if spec.Command == nil || spec.Command.BuildPrompt == nil || spec.Command.BuildAuthenticate == nil || spec.Command.BuildLogout == nil || spec.Command.NewStreamParser == nil || len(spec.Command.AuthMethods) != 1 || len(spec.Command.Options) != 5 || len(spec.Command.Commands) != 2 || !spec.Command.IncludeTranscript {
		t.Fatalf("command spec = %#v, want command-backed bridge with config options and commands", spec.Command)
	}
	if spec.Command.AuthMethods[0].ID != "agent-login" || spec.Command.AuthMethods[0].Name != "Codex login" {
		t.Fatalf("auth methods = %#v, want Codex login", spec.Command.AuthMethods)
	}
	if spec.Command.Commands[0].Name != "review" || spec.Command.Commands[0].InputHint == "" ||
		spec.Command.Commands[1].Name != "init" || spec.Command.Commands[1].InputHint == "" {
		t.Fatalf("commands = %#v, want review/init commands with input hints", spec.Command.Commands)
	}
	if spec.Doctor == nil || spec.Doctor.Binary != "codex" {
		t.Fatalf("doctor spec = %#v, want codex doctor", spec.Doctor)
	}
	wantEnv := []string{"PATH", "HOME", "USER", "LOGNAME", "XDG_CONFIG_HOME", "TMPDIR", "CODEX_HOME", "OPENAI_API_KEY", "OPENAI_BASE_URL"}
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
	if !reflect.DeepEqual(got.Env.Inherit, codexadapter.RuntimeEnv()) {
		t.Fatalf("process env = %#v, want runtime env allowlist", got.Env)
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

func TestLogoutCommandUsesNativeCodexCLIOnly(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	got, err := codexadapter.LogoutCommand()
	if err != nil {
		t.Fatalf("LogoutCommand: %v", err)
	}
	assertNoPackageRunnerCommand(t, got.Command)
	if got.Command != "codex" || got.Dir != cwd || !reflect.DeepEqual(got.Args, []string{"logout"}) {
		t.Fatalf("process spec = %#v, want codex logout", got)
	}
	if !reflect.DeepEqual(got.Env.Inherit, codexadapter.RuntimeEnv()) {
		t.Fatalf("process env = %#v, want runtime env allowlist", got.Env)
	}
}

func TestAuthenticateCommandUsesNativeCodexCLIOnly(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	got, err := codexadapter.AuthenticateCommand("agent-login")
	if err != nil {
		t.Fatalf("AuthenticateCommand: %v", err)
	}
	assertNoPackageRunnerCommand(t, got.Command)
	if got.Command != "codex" || got.Dir != cwd || !reflect.DeepEqual(got.Args, []string{"login"}) {
		t.Fatalf("process spec = %#v, want codex login", got)
	}
	if !reflect.DeepEqual(got.Env.Inherit, codexadapter.RuntimeEnv()) {
		t.Fatalf("process env = %#v, want runtime env allowlist", got.Env)
	}
	if _, err := codexadapter.AuthenticateCommand("browser-login"); err == nil || !strings.Contains(err.Error(), "unsupported auth method") {
		t.Fatalf("AuthenticateCommand unsupported error = %v, want unsupported auth method", err)
	}
}

func TestPromptCommandBuildsCodexExec(t *testing.T) {
	got, err := codexadapter.PromptCommand(commandbridge.Session{
		CWD:                   "/work",
		AdditionalDirectories: []string{"/extra", ""},
		MCPServers: []runtimeacp.MCPServer{
			{
				ID:   "weather-http",
				Name: "Weather HTTP",
				URL:  "https://mcp.example.com/mcp",
				Headers: []runtimeacp.HTTPHeader{
					{Name: "X-Test", Value: "yes"},
					{Name: "Authorization", Value: "Bearer token"},
				},
			},
			{
				Name:    "Local FS",
				Command: "/bin/mcp",
				Args:    []string{"--root", "/work"},
				Env:     []runtimeacp.EnvVariable{{Name: "TOKEN", Value: "secret"}},
			},
		},
		Config: map[string]string{
			"model":            "gpt-5-codex",
			"reasoning_effort": "high",
			"sandbox":          "read-only",
			"approval_policy":  "never",
			"web_search":       "enabled",
		},
	}, runtimeacp.PromptParams{
		Prompt: []runtimeacp.ContentBlock{{Type: "text", Text: "hello codex"}},
	})
	if err != nil {
		t.Fatalf("PromptCommand: %v", err)
	}
	wantArgs := []string{
		"--search",
		"--ask-for-approval", "never",
		"exec",
		"--cd", "/work",
		"--sandbox", "read-only",
		"--ignore-user-config",
		"--skip-git-repo-check",
		"--json",
		"--add-dir", "/extra",
		"--model", "gpt-5-codex",
		"--config", `model_reasoning_effort="high"`,
		"--config", `mcp_servers.hecate_01_weather-http={url="https://mcp.example.com/mcp",http_headers={"Authorization"="Bearer token","X-Test"="yes"}}`,
		"--config", `mcp_servers.hecate_02_Local_FS={command="/bin/mcp",args=["--root","/work"],env={"TOKEN"="secret"}}`,
		"hello codex",
	}
	if got.Command != "codex" || got.Dir != "/work" || !reflect.DeepEqual(got.Args, wantArgs) {
		t.Fatalf("process spec = %#v, want codex exec args %#v", got, wantArgs)
	}
}

func TestPromptCommandBuildsCodexExecBypassApprovalsAndSandbox(t *testing.T) {
	got, err := codexadapter.PromptCommand(commandbridge.Session{
		CWD: "/work",
		Config: map[string]string{
			"approval_policy": "bypass",
			"sandbox":         "read-only",
		},
	}, runtimeacp.PromptParams{
		Prompt: []runtimeacp.ContentBlock{{Type: "text", Text: "hello codex"}},
	})
	if err != nil {
		t.Fatalf("PromptCommand: %v", err)
	}
	wantArgs := []string{
		"--dangerously-bypass-approvals-and-sandbox",
		"exec",
		"--cd", "/work",
		"--ignore-user-config",
		"--skip-git-repo-check",
		"--json",
		"hello codex",
	}
	if got.Command != "codex" || got.Dir != "/work" || !reflect.DeepEqual(got.Args, wantArgs) {
		t.Fatalf("process spec = %#v, want codex exec args %#v", got, wantArgs)
	}
}

func TestPromptCommandBuildsCodexReview(t *testing.T) {
	got, err := codexadapter.PromptCommand(commandbridge.Session{
		CWD:                   "/work",
		AdditionalDirectories: []string{"/extra"},
		MCPServers: []runtimeacp.MCPServer{{
			Name: "Docs",
			URL:  "https://docs.example.com/mcp",
		}},
		Config: map[string]string{
			"model":            "gpt-5-codex",
			"reasoning_effort": "high",
			"sandbox":          "read-only",
			"web_search":       "enabled",
		},
	}, runtimeacp.PromptParams{
		Prompt: []runtimeacp.ContentBlock{{Type: "text", Text: "Previous conversation:\n\nUser:\nhello\n\nCurrent user request:\n/review focus on tests"}},
	})
	if err != nil {
		t.Fatalf("PromptCommand: %v", err)
	}
	wantArgs := []string{
		"review",
		"--uncommitted",
		"--config", `model="gpt-5-codex"`,
		"--config", `model_reasoning_effort="high"`,
		"--config", `mcp_servers.hecate_01_Docs={url="https://docs.example.com/mcp"}`,
		"focus on tests",
	}
	if got.Command != "codex" || got.Dir != "/work" || !reflect.DeepEqual(got.Args, wantArgs) {
		t.Fatalf("process spec = %#v, want codex review args %#v", got, wantArgs)
	}
}

func TestPromptCommandBuildsCodexInitAsExec(t *testing.T) {
	got, err := codexadapter.PromptCommand(commandbridge.Session{
		CWD: "/work",
		Config: map[string]string{
			"model":            "gpt-5-codex",
			"reasoning_effort": "medium",
			"sandbox":          "workspace-write",
		},
	}, runtimeacp.PromptParams{
		Prompt: []runtimeacp.ContentBlock{{Type: "text", Text: "/init focus on repo guidance"}},
	})
	if err != nil {
		t.Fatalf("PromptCommand: %v", err)
	}
	wantArgs := []string{
		"exec",
		"--cd", "/work",
		"--sandbox", "workspace-write",
		"--ignore-user-config",
		"--skip-git-repo-check",
		"--json",
		"--model", "gpt-5-codex",
		"--config", `model_reasoning_effort="medium"`,
		"/init focus on repo guidance",
	}
	if got.Command != "codex" || got.Dir != "/work" || !reflect.DeepEqual(got.Args, wantArgs) {
		t.Fatalf("process spec = %#v, want codex exec args %#v", got, wantArgs)
	}
}

func TestPromptCommandRejectsUnsupportedMCPServer(t *testing.T) {
	_, err := codexadapter.PromptCommand(commandbridge.Session{
		CWD:        "/work",
		MCPServers: []runtimeacp.MCPServer{{Name: "broken"}},
	}, runtimeacp.PromptParams{
		Prompt: []runtimeacp.ContentBlock{{Type: "text", Text: "hello codex"}},
	})
	if err == nil || !strings.Contains(err.Error(), `mcp server "broken" must set url or command`) {
		t.Fatalf("PromptCommand error = %v, want unsupported MCP server error", err)
	}
}

func TestNewServerPublishesAvailableCommands(t *testing.T) {
	client := acptest.NewClient(t, codexadapter.NewServer("test"))

	responses := client.Send(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "session/new",
		"params":  map[string]any{"cwd": t.TempDir()},
	})
	if len(responses) != 2 {
		t.Fatalf("responses = %#v, want available command update + session response", responses)
	}
	update := decodeSessionUpdate(t, responses[0])
	if update.Update.SessionUpdate != "available_commands_update" ||
		len(update.Update.AvailableCommands) != 2 ||
		update.Update.AvailableCommands[0].Name != "review" ||
		update.Update.AvailableCommands[0].Input.Unstructured.Hint != "optional review instructions" ||
		update.Update.AvailableCommands[1].Name != "init" ||
		update.Update.AvailableCommands[1].Input.Unstructured.Hint != "optional instruction focus" {
		t.Fatalf("available commands = %#v, want review/init commands", update)
	}
}

func TestNewServerRunsLogoutCommand(t *testing.T) {
	installFakeCommand(t, "codex", `
if [ "$1" != "logout" ]; then
  echo "unexpected command: $*" >&2
  exit 64
fi
printf 'logged out\n'
	`)
	client := acptest.NewClient(t, codexadapter.NewServer("test"))

	resp := client.Request("logout", map[string]any{})
	var result map[string]any
	resp.ResultInto(t, &result)
	if len(result) != 0 {
		t.Fatalf("logout result = %#v, want empty object", result)
	}
}

func TestNewServerRunsAuthenticateCommand(t *testing.T) {
	installFakeCommand(t, "codex", `
if [ "$1" != "login" ]; then
  echo "unexpected command: $*" >&2
  exit 64
fi
printf 'logged in\n'
	`)
	client := acptest.NewClient(t, codexadapter.NewServer("test"))

	resp := client.Request("authenticate", map[string]any{"methodId": "agent-login"})
	var result map[string]any
	resp.ResultInto(t, &result)
	if len(result) != 0 {
		t.Fatalf("authenticate result = %#v, want empty object", result)
	}
}

func TestNewServerWithRunnerUsesHostRunner(t *testing.T) {
	var got adapterprocess.Spec
	runner := commandbridge.RunnerFunc(func(_ context.Context, spec adapterprocess.Spec) (adapterprocess.Result, error) {
		got = spec
		return adapterprocess.Result{}, nil
	})
	client := acptest.NewClient(t, codexadapter.NewServerWithRunner("test", runner))

	resp := client.Request("authenticate", map[string]any{"methodId": "agent-login"})
	var result map[string]any
	resp.ResultInto(t, &result)
	if got.Command != "codex" || len(got.Args) != 1 || got.Args[0] != "login" {
		t.Fatalf("runner command = %q args=%#v, want provider login command", got.Command, got.Args)
	}
}

func TestNewServerMapsPromptAuthFailure(t *testing.T) {
	installFakeCommand(t, "codex", `
if [ "$1" != "exec" ]; then
  echo "unexpected command: $*" >&2
  exit 64
fi
echo "Authentication required. Please run codex login." >&2
exit 1
`)
	client := acptest.NewClient(t, codexadapter.NewServer("test"))
	client.Request("initialize", map[string]any{})
	createdResponses := client.Send(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "session/new",
		"params":  map[string]any{"cwd": t.TempDir()},
	})
	if len(createdResponses) != 2 {
		t.Fatalf("create responses = %#v, want available commands + session response", createdResponses)
	}
	var session struct {
		SessionID string `json:"sessionId"`
	}
	createdResponses[1].ResultInto(t, &session)

	responses := client.Send(map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "session/prompt",
		"params": map[string]any{
			"sessionId": session.SessionID,
			"prompt":    []map[string]any{{"type": "text", "text": "hello"}},
		},
	})
	if len(responses) != 3 {
		t.Fatalf("responses = %#v, want tool start + tool finish + auth error", responses)
	}
	if responses[2].Error == nil || responses[2].Error.Code != -32000 || responses[2].Error.Message != "Authentication required" {
		t.Fatalf("prompt error = %#v, want auth required", responses[2].Error)
	}
	raw, _ := json.Marshal(responses[2].Error.Data)
	if !strings.Contains(string(raw), "codex login") {
		t.Fatalf("auth error data = %s, want login hint", raw)
	}
}

func TestCommandAuthRequiredMapsCodexHTTP401(t *testing.T) {
	result := adapterprocess.Result{
		Stderr: []byte("401 Unauthorized: Missing bearer or basic authentication"),
	}
	if !codexadapter.CommandAuthRequired(result, errors.New("exit status 1")) {
		t.Fatal("CommandAuthRequired returned false, want auth required for Codex 401")
	}
}

func TestNewServerStreamsNativeCodexOutput(t *testing.T) {
	installFakeCommand(t, "codex", `
if [ "$1" != "exec" ]; then
  echo "unexpected command: $*" >&2
  exit 64
fi
printf '{"method":"item/started","params":{"item":{"type":"local_shell_call","id":"tool-1","command":"go test ./..."}}}\n'
sleep 0.05
printf '{"method":"item/reasoning/textDelta","params":{"item_id":"thought-1","delta":"checking"}}\n'
printf '{"method":"item/completed","params":{"item":{"type":"agent_message","id":"msg-1","text":"chunk one chunk two"}}}\n'
printf '{"method":"turn/completed","params":{"usage":{"input_tokens":10,"output_tokens":5,"context_window":100}}}\n'
printf '{"method":"item/completed","params":{"item":{"type":"local_shell_call","id":"tool-1","status":"completed","stdout":"ok"}}}\n'
	`)
	client := acptest.NewClient(t, codexadapter.NewServer("test"))
	client.Request("initialize", map[string]any{})
	createdResponses := client.Send(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "session/new",
		"params":  map[string]any{"cwd": t.TempDir()},
	})
	if len(createdResponses) != 2 {
		t.Fatalf("create responses = %#v, want available commands + session response", createdResponses)
	}
	var session struct {
		SessionID string `json:"sessionId"`
	}
	createdResponses[1].ResultInto(t, &session)
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
	if len(responses) < 4 {
		t.Fatalf("got %d responses, want tool start + streamed update(s) + tool finish + prompt response: %#v", len(responses), responses)
	}
	start := decodeSessionUpdate(t, responses[0])
	if start.Update.SessionUpdate != "tool_call" ||
		start.Update.Status != "in_progress" ||
		start.Update.ToolCallID == "" ||
		start.Update.Title != "Run codex" ||
		start.Update.RawInput["command"] == "" {
		t.Fatalf("tool start = %#v, want native Codex command metadata", start)
	}
	innerStart := decodeSessionUpdate(t, responses[1])
	if innerStart.Update.SessionUpdate != "tool_call" ||
		innerStart.Update.ToolCallID != "tool-1" ||
		innerStart.Update.Kind != "execute" ||
		innerStart.Update.Status != "in_progress" {
		t.Fatalf("inner tool start = %#v, want parsed Codex tool start", innerStart)
	}
	thought := decodeSessionUpdate(t, responses[2])
	if thought.Update.SessionUpdate != "agent_thought_chunk" || decodeChunkText(t, thought.Update.Content) != "checking" {
		t.Fatalf("thought = %#v, want parsed reasoning chunk", thought)
	}
	message := decodeSessionUpdate(t, responses[3])
	if message.Update.SessionUpdate != "agent_message_chunk" || decodeChunkText(t, message.Update.Content) != "chunk one chunk two" {
		t.Fatalf("message = %#v, want parsed Codex answer", message)
	}
	usage := decodeSessionUpdate(t, responses[4])
	if usage.Update.SessionUpdate != "usage_update" || usage.Update.Used != 15 || usage.Update.Size != 100 {
		t.Fatalf("usage = %#v, want parsed Codex usage", usage)
	}
	innerFinish := decodeSessionUpdate(t, responses[5])
	if innerFinish.Update.SessionUpdate != "tool_call_update" ||
		innerFinish.Update.ToolCallID != "tool-1" ||
		innerFinish.Update.Status != "completed" ||
		!strings.Contains(string(innerFinish.Update.Content), "ok") {
		t.Fatalf("inner tool finish = %#v, want parsed Codex tool finish", innerFinish)
	}
	finish := decodeSessionUpdate(t, responses[len(responses)-3])
	if finish.Update.SessionUpdate != "tool_call_update" ||
		finish.Update.ToolCallID != start.Update.ToolCallID ||
		finish.Update.Status != "completed" ||
		len(finish.Update.Content) != 0 {
		t.Fatalf("tool finish = %#v, want completed native Codex command", finish)
	}
	info := decodeSessionUpdate(t, responses[len(responses)-2])
	if info.Update.SessionUpdate != "session_info_update" ||
		info.Update.Title != "hello" ||
		info.Update.UpdatedAt == "" {
		t.Fatalf("session info = %#v, want transcript metadata", info)
	}
	var promptResult struct {
		StopReason string `json:"stopReason"`
	}
	responses[len(responses)-1].ResultInto(t, &promptResult)
	if promptResult.StopReason != "end_turn" {
		t.Fatalf("stop reason = %q, want end_turn", promptResult.StopReason)
	}
}

func TestNewServerRequestsPermissionFromCodexStream(t *testing.T) {
	installFakeCommand(t, "codex", `
if [ "$1" != "exec" ]; then
  echo "unexpected command: $*" >&2
  exit 64
fi
printf '{"method":"permission/requested","params":{"toolCall":{"toolCallId":"tool-1","title":"Run tests","kind":"execute","rawInput":{"command":"go test ./..."}},"options":[{"optionId":"allow","name":"Allow","kind":"allow_once"},{"optionId":"reject","name":"Reject","kind":"reject_once"}]}}\n'
printf '{"method":"item/completed","params":{"item":{"type":"agent_message","id":"msg-1","text":"allowed"}}}\n'
	`)
	client := acptest.NewClient(t, codexadapter.NewServer("test"))
	client.Request("initialize", map[string]any{})
	createdResponses := client.Send(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "session/new",
		"params":  map[string]any{"cwd": t.TempDir()},
	})
	var session struct {
		SessionID string `json:"sessionId"`
	}
	createdResponses[1].ResultInto(t, &session)

	responses := client.SendRaw(strings.Join([]string{
		`{"jsonrpc":"2.0","id":3,"method":"session/prompt","params":{"sessionId":"` + session.SessionID + `","prompt":[{"type":"text","text":"hello"}]}}`,
		`{"jsonrpc":"2.0","id":"server-1","result":{"outcome":{"outcome":"selected","optionId":"allow"}}}`,
	}, "\n") + "\n")
	if len(responses) != 6 {
		t.Fatalf("responses = %#v, want tool start + permission + answer + tool finish + session info + prompt result", responses)
	}
	permission := decodePermissionRequest(t, responses[1])
	if permission.SessionID != session.SessionID ||
		permission.ToolCall.ToolCallID != "tool-1" ||
		permission.ToolCall.Title != "Run tests" ||
		permission.ToolCall.Status != "pending" ||
		permission.ToolCall.RawInput["command"] != "go test ./..." ||
		len(permission.Options) != 2 ||
		permission.Options[0].OptionID != "allow" ||
		permission.Options[1].OptionID != "reject" {
		t.Fatalf("permission = %#v, want Codex stream permission request", permission)
	}
	answer := decodeSessionUpdate(t, responses[2])
	if answer.Update.SessionUpdate != "agent_message_chunk" || decodeChunkText(t, answer.Update.Content) != "allowed" {
		t.Fatalf("answer = %#v, want stream continuation after approval", answer)
	}
	info := decodeSessionUpdate(t, responses[4])
	if info.Update.SessionUpdate != "session_info_update" ||
		info.Update.Title != "hello" ||
		info.Update.UpdatedAt == "" {
		t.Fatalf("session info = %#v, want transcript metadata", info)
	}
	var promptResult struct {
		StopReason string `json:"stopReason"`
	}
	responses[5].ResultInto(t, &promptResult)
	if promptResult.StopReason != "end_turn" {
		t.Fatalf("stop reason = %q, want end_turn", promptResult.StopReason)
	}
}

func TestNewServerStopsCodexStreamWhenPermissionDenied(t *testing.T) {
	tests := []struct {
		name       string
		option     acptest.LiveClientOption
		wantReason string
	}{
		{
			name:       "rejected",
			option:     acptest.WithAutoRejectPermissions(),
			wantReason: "permission rejected for Run tests",
		},
		{
			name:       "cancelled",
			option:     acptest.WithAutoCancelPermissions(),
			wantReason: "permission cancelled for Run tests",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			installFakeCommand(t, "codex", `
if [ "$1" != "exec" ]; then
  echo "unexpected command: $*" >&2
  exit 64
fi
printf '{"method":"permission/requested","params":{"toolCall":{"toolCallId":"tool-1","title":"Run tests","kind":"execute","rawInput":{"command":"go test ./..."}},"options":[{"optionId":"allow","name":"Allow","kind":"allow_once"},{"optionId":"reject","name":"Reject","kind":"reject_once"}]}}\n'
printf '{"method":"item/completed","params":{"item":{"type":"agent_message","id":"msg-1","text":"should not continue"}}}\n'
	`)
			client := acptest.NewLiveClient(t, codexadapter.NewServer("test"), tt.option)
			client.Request("initialize", "initialize", map[string]any{}, time.Second)
			createdResponses := client.Request("new-session", "session/new", map[string]any{"cwd": t.TempDir()}, time.Second)
			var session struct {
				SessionID string `json:"sessionId"`
			}
			createdResponses[len(createdResponses)-1].ResultInto(t, &session)

			responses := client.PromptText("prompt", session.SessionID, "hello", time.Second)
			if len(responses) < 3 {
				t.Fatalf("responses = %#v, want tool start + permission request + prompt error", responses)
			}
			permission := decodePermissionRequest(t, responses[1])
			if permission.ToolCall.ToolCallID != "tool-1" ||
				permission.ToolCall.Title != "Run tests" ||
				permission.ToolCall.Kind != "execute" {
				t.Fatalf("permission = %#v, want Codex stream permission request", permission)
			}
			for _, response := range responses {
				if response.Method != "session/update" {
					continue
				}
				update := decodeSessionUpdate(t, response)
				if update.Update.SessionUpdate == "agent_message_chunk" {
					t.Fatalf("responses = %#v, did not expect assistant continuation after denied permission", responses)
				}
			}
			final := responses[len(responses)-1]
			if final.Error == nil ||
				final.Error.Message != "prompt command failed" ||
				!strings.Contains(fmt.Sprint(final.Error.Data), tt.wantReason) {
				t.Fatalf("final response = %#v, want prompt command failed with %q", final, tt.wantReason)
			}
		})
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

func TestCodexStreamParserMapsJSONL(t *testing.T) {
	parser := codexadapter.NewStreamParser(commandbridge.Session{}, runtimeacp.PromptParams{})
	chunks := []string{
		`{"method":"item/started","params":{"item":{"type":"apply_patch","id":"patch-1","title":"Apply patch","input":{"path":"main.go"}}}}` + "\n",
		`{"method":"item/reasoning/summaryTextDelta","params":{"item_id":"reason-1","delta":"thinking"}}` + "\n",
		`{"method":"item/completed","params":{"item":{"type":"agent_message","id":"msg-1","content":[{"type":"output_text","text":"done"}]}}}` + "\n",
		`{"method":"turn/completed","params":{"usage":{"input_tokens":2,"cached_input_tokens":3,"output_tokens":5,"reasoning_output_tokens":7,"context_window":1000}}}` + "\n",
		`{"method":"item/completed","params":{"item":{"type":"apply_patch","id":"patch-1","status":"completed","result":"patched"}}}` + "\n",
	}
	var events []commandbridge.StreamEvent
	for _, chunk := range chunks {
		parsed, err := parser.Parse([]byte(chunk))
		if err != nil {
			t.Fatalf("Parse returned error: %v", err)
		}
		events = append(events, parsed...)
	}
	flushed, err := parser.Flush()
	if err != nil {
		t.Fatalf("Flush returned error: %v", err)
	}
	events = append(events, flushed...)
	if len(events) != 5 {
		t.Fatalf("events len = %d, want 5: %#v", len(events), events)
	}
	if events[0].Update["sessionUpdate"] != "tool_call" ||
		events[0].Update["toolCallId"] != "patch-1" ||
		events[0].Update["kind"] != "edit" {
		t.Fatalf("tool start = %#v, want edit tool start", events[0].Update)
	}
	if events[1].Update["sessionUpdate"] != "agent_thought_chunk" {
		t.Fatalf("thought = %#v, want thought chunk", events[1].Update)
	}
	if events[2].Update["sessionUpdate"] != "agent_message_chunk" || parser.Transcript() != "done" {
		t.Fatalf("message = %#v transcript=%q, want answer transcript", events[2].Update, parser.Transcript())
	}
	if events[3].Update["sessionUpdate"] != "usage_update" ||
		events[3].Update["used"] != 17 ||
		events[3].Update["size"] != 1000 {
		t.Fatalf("usage = %#v, want summed usage", events[3].Update)
	}
	if events[4].Update["sessionUpdate"] != "tool_call_update" ||
		events[4].Update["toolCallId"] != "patch-1" ||
		events[4].Update["status"] != "completed" {
		t.Fatalf("tool finish = %#v, want completed tool", events[4].Update)
	}
}

func TestCodexStreamParserMapsPermissionRequest(t *testing.T) {
	parser := codexadapter.NewStreamParser(commandbridge.Session{}, runtimeacp.PromptParams{})

	events, err := parser.Parse([]byte(`{"method":"permission/requested","params":{"toolCall":{"toolCallId":"tool-1","title":"Run tests","kind":"execute","rawInput":{"command":"go test ./..."}},"options":[{"optionId":"allow","name":"Allow","kind":"allow_once"},{"optionId":"reject","name":"Reject","kind":"reject_once"}]}}` + "\n"))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events len = %d, want permission request: %#v", len(events), events)
	}
	req := events[0].PermissionRequest
	if req == nil {
		t.Fatalf("event = %#v, want permission request", events[0])
	}
	rawInput, _ := req.RawInput.(map[string]any)
	if req.ToolCallID != "tool-1" ||
		req.Title != "Run tests" ||
		req.Kind != "execute" ||
		rawInput["command"] != "go test ./..." {
		t.Fatalf("permission request = %#v, want Codex tool permission", req)
	}
	if len(req.Options) != 2 ||
		req.Options[0].OptionID != "allow" ||
		req.Options[0].Kind != "allow_once" ||
		req.Options[1].OptionID != "reject" ||
		req.Options[1].Kind != "reject_once" {
		t.Fatalf("permission options = %#v, want allow/reject", req.Options)
	}
}

func TestCodexStreamParserMapsPermissionRequestAliases(t *testing.T) {
	tests := []struct {
		name       string
		line       string
		wantID     string
		wantTitle  string
		wantKind   string
		wantRawKey string
		wantRawVal string
		wantOpts   []commandbridge.PermissionOption
	}{
		{
			name:       "snake case mcp tool call and permission options",
			line:       `{"method":"approval.requested","params":{"tool_call":{"tool_call_id":"mcp-1","type":"mcp_tool_call","server":"docs","tool":"search_docs","arguments":{"query":"permissions"}},"permission_options":[{"option_id":"allow-session","name":"Allow for session","kind":"allow_always"},{"option_id":"deny-once","name":"Deny once","kind":"reject_once"}]}}`,
			wantID:     "mcp-1",
			wantTitle:  "docs/search_docs",
			wantKind:   "mcp",
			wantRawKey: "query",
			wantRawVal: "permissions",
			wantOpts: []commandbridge.PermissionOption{
				{OptionID: "allow-session", Name: "Allow for session", Kind: "allow_always"},
				{OptionID: "deny-once", Name: "Deny once", Kind: "reject_once"},
			},
		},
		{
			name:       "call shape and choices options",
			line:       `{"event":"permission/requested","params":{"call":{"callId":"shell-1","type":"local_shell_call","command":"make test"},"choices":[{"value":"allow-once","title":"Allow once","type":"allow_once"},{"value":"reject-once","title":"Reject","type":"reject_once"}]}}`,
			wantID:     "shell-1",
			wantTitle:  "make test",
			wantKind:   "execute",
			wantRawKey: "command",
			wantRawVal: "make test",
			wantOpts: []commandbridge.PermissionOption{
				{OptionID: "allow-once", Name: "Allow once", Kind: "allow_once"},
				{OptionID: "reject-once", Name: "Reject", Kind: "reject_once"},
			},
		},
		{
			name:       "defaults permission options when missing",
			line:       `{"method":"permission/requested","params":{"toolCall":{"id":"read-1","type":"file_read","path":"README.md"}}}`,
			wantID:     "read-1",
			wantTitle:  "README.md",
			wantKind:   "read",
			wantRawKey: "",
			wantRawVal: "",
			wantOpts: []commandbridge.PermissionOption{
				{OptionID: "allow_once", Name: "Allow once", Kind: "allow_once"},
				{OptionID: "reject_once", Name: "Reject", Kind: "reject_once"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := codexadapter.NewStreamParser(commandbridge.Session{}, runtimeacp.PromptParams{})
			events, err := parser.Parse([]byte(tt.line + "\n"))
			if err != nil {
				t.Fatalf("Parse returned error: %v", err)
			}
			if len(events) != 1 || events[0].PermissionRequest == nil {
				t.Fatalf("events = %#v, want one permission request", events)
			}
			req := events[0].PermissionRequest
			if req.ToolCallID != tt.wantID ||
				req.Title != tt.wantTitle ||
				req.Kind != tt.wantKind {
				t.Fatalf("permission request = %#v, want id/title/kind %q/%q/%q", req, tt.wantID, tt.wantTitle, tt.wantKind)
			}
			if tt.wantRawKey != "" {
				rawInput, _ := req.RawInput.(map[string]any)
				if rawInput[tt.wantRawKey] != tt.wantRawVal {
					t.Fatalf("raw input = %#v, want %s=%q", rawInput, tt.wantRawKey, tt.wantRawVal)
				}
			}
			if len(req.Options) != len(tt.wantOpts) {
				t.Fatalf("options = %#v, want %#v", req.Options, tt.wantOpts)
			}
			for i := range tt.wantOpts {
				if req.Options[i] != tt.wantOpts[i] {
					t.Fatalf("option %d = %#v, want %#v", i, req.Options[i], tt.wantOpts[i])
				}
			}
		})
	}
}

func TestCodexStreamParserMapsTerminalStopReason(t *testing.T) {
	parser := codexadapter.NewStreamParser(commandbridge.Session{}, runtimeacp.PromptParams{})

	events, err := parser.Parse([]byte(`{"method":"turn/completed","params":{"final_answer":"partial answer","finish_reason":"max_tokens","usage":{"input_tokens":4,"output_tokens":6,"context_window":128}}}` + "\n"))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events len = %d, want message + usage: %#v", len(events), events)
	}
	if events[0].Update["sessionUpdate"] != "agent_message_chunk" || parser.Transcript() != "partial answer" {
		t.Fatalf("message = %#v transcript=%q, want final answer", events[0].Update, parser.Transcript())
	}
	if events[1].Update["sessionUpdate"] != "usage_update" ||
		events[1].Update["used"] != 10 ||
		events[1].Update["size"] != 128 {
		t.Fatalf("usage = %#v, want terminal usage", events[1].Update)
	}
	if got := parser.StopReason(); got != runtimeacp.StopReasonMaxTokens {
		t.Fatalf("StopReason() = %q, want max_tokens", got)
	}
}

func TestCodexStreamParserMapsCancelledStopReason(t *testing.T) {
	parser := codexadapter.NewStreamParser(commandbridge.Session{}, runtimeacp.PromptParams{})

	events, err := parser.Parse([]byte(`{"method":"turn/completed","params":{"finish_reason":"cancelled","usage":{"total_tokens":3,"context_window":128}}}` + "\n"))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events len = %d, want usage update: %#v", len(events), events)
	}
	if events[0].Update["sessionUpdate"] != "usage_update" ||
		events[0].Update["used"] != 3 ||
		events[0].Update["size"] != 128 {
		t.Fatalf("usage = %#v, want terminal usage", events[0].Update)
	}
	if got := parser.StopReason(); got != runtimeacp.StopReasonCancelled {
		t.Fatalf("StopReason() = %q, want cancelled", got)
	}
}

func TestCodexStreamParserMapsCurrentCLIJSONL(t *testing.T) {
	parser := codexadapter.NewStreamParser(commandbridge.Session{}, runtimeacp.PromptParams{})
	fixture := strings.Join([]string{
		`{"type":"thread.started","thread_id":"thread-1"}`,
		`{"type":"turn.started"}`,
		`{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"Confirmed: this is the Codex ACP adapter real CLI smoke."}}`,
		`{"type":"turn.completed","usage":{"input_tokens":10,"cached_input_tokens":3,"output_tokens":5,"reasoning_output_tokens":0}}`,
		"",
	}, "\n")

	events, err := parser.Parse([]byte(fixture))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events len = %d, want one assistant message: %#v", len(events), events)
	}
	if events[0].Update["sessionUpdate"] != "agent_message_chunk" ||
		updateText(events[0].Update) != "Confirmed: this is the Codex ACP adapter real CLI smoke." ||
		parser.Transcript() != "Confirmed: this is the Codex ACP adapter real CLI smoke." {
		t.Fatalf("message = %#v transcript=%q, want current Codex assistant message", events[0].Update, parser.Transcript())
	}
}

func TestCodexStreamParserMapsSourceShapedFixtures(t *testing.T) {
	parser := codexadapter.NewStreamParser(commandbridge.Session{}, runtimeacp.PromptParams{})
	fixture := strings.Join([]string{
		`{"method":"item/started","params":{"item":{"type":"local_shell_call","id":"shell-1","command":"go test ./...","status":"in_progress"}}}`,
		`{"event":"approval/requested","params":{"item":{"type":"local_shell_call","id":"shell-1","display_command":"go test ./...","input":{"command":"go test ./..."}},"permissionOptions":[{"id":"allow-session","label":"Allow for session","type":"allow_always"},{"id":"deny-once","label":"Deny once","type":"reject_once"}]}}`,
		`{"method":"item/reasoning/textDelta","params":{"itemId":"reason-1","text":"checking tests"}}`,
		`{"method":"item/completed","params":{"item":{"type":"local_shell_call","id":"shell-1","status":"completed","stdout":"ok"}}}`,
		`{"method":"turn/completed","params":{"finishReason":"max_tokens","usage":{"input_tokens":"4","output_tokens":6,"context_window_tokens":128}}}`,
		"",
	}, "\n")

	events, err := parser.Parse([]byte(fixture))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("events len = %d, want 5: %#v", len(events), events)
	}
	if events[0].Update["sessionUpdate"] != "tool_call" ||
		events[0].Update["toolCallId"] != "shell-1" ||
		events[0].Update["kind"] != "execute" ||
		events[0].Update["title"] != "go test ./..." {
		t.Fatalf("tool start = %#v, want shell start", events[0].Update)
	}
	req := events[1].PermissionRequest
	if req == nil {
		t.Fatalf("event = %#v, want permission request", events[1])
	}
	rawInput, _ := req.RawInput.(map[string]any)
	if req.ToolCallID != "shell-1" ||
		req.Title != "go test ./..." ||
		req.Kind != "execute" ||
		rawInput["command"] != "go test ./..." ||
		len(req.Options) != 2 ||
		req.Options[0].OptionID != "allow-session" ||
		req.Options[0].Kind != "allow_always" ||
		req.Options[1].OptionID != "deny-once" ||
		req.Options[1].Kind != "reject_once" {
		t.Fatalf("permission request = %#v, rawInput=%#v, want source-shaped shell permission", req, rawInput)
	}
	if events[2].Update["sessionUpdate"] != "agent_thought_chunk" ||
		updateText(events[2].Update) != "checking tests" {
		t.Fatalf("thought = %#v, want reasoning text", events[2].Update)
	}
	if events[3].Update["sessionUpdate"] != "tool_call_update" ||
		events[3].Update["status"] != "completed" ||
		events[3].Update["rawOutput"] != "ok" {
		t.Fatalf("tool finish = %#v, want completed shell output", events[3].Update)
	}
	if events[4].Update["sessionUpdate"] != "usage_update" ||
		events[4].Update["used"] != 10 ||
		events[4].Update["size"] != 128 {
		t.Fatalf("usage = %#v, want source-shaped usage", events[4].Update)
	}
	if got := parser.StopReason(); got != runtimeacp.StopReasonMaxTokens {
		t.Fatalf("StopReason() = %q, want max_tokens", got)
	}
}

func TestCodexStreamParserPreservesStructuredShellOutput(t *testing.T) {
	parser := codexadapter.NewStreamParser(commandbridge.Session{}, runtimeacp.PromptParams{})

	events, err := parser.Parse([]byte(`{"method":"item/completed","params":{"item":{"type":"local_shell_call","id":"shell-1","status":"completed","stdout":"ok\n","stderr":"warn\n","exit_code":2}}}` + "\n"))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events len = %d, want one shell finish: %#v", len(events), events)
	}
	if events[0].Update["sessionUpdate"] != "tool_call_update" ||
		events[0].Update["toolCallId"] != "shell-1" ||
		events[0].Update["status"] != "failed" {
		t.Fatalf("tool finish = %#v, want failed shell finish", events[0].Update)
	}
	rawOutput, ok := events[0].Update["rawOutput"].(map[string]any)
	if !ok {
		t.Fatalf("rawOutput = %#v, want structured map", events[0].Update["rawOutput"])
	}
	if rawOutput["stdout"] != "ok\n" || rawOutput["stderr"] != "warn\n" || rawOutput["exit_code"] != float64(2) {
		t.Fatalf("rawOutput = %#v, want stdout/stderr/exit_code preserved", rawOutput)
	}
}

func TestCodexStreamParserMarksProviderRejectedToolsFailed(t *testing.T) {
	tests := []struct {
		name string
		item string
	}{
		{
			name: "denied",
			item: `{"type":"local_shell_call","id":"tool-1","status":"denied","stdout":"permission denied"}`,
		},
		{
			name: "rejected",
			item: `{"type":"mcp_tool_call","id":"tool-1","state":"rejected","server":"docs","tool":"search"}`,
		},
		{
			name: "blocked",
			item: `{"type":"web_search_call","id":"tool-1","outcome":"blocked_by_policy","query":"secret"}`,
		},
		{
			name: "timed out",
			item: `{"type":"local_shell_call","id":"tool-1","resultStatus":"timed_out","command":"sleep 60"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := codexadapter.NewStreamParser(commandbridge.Session{}, runtimeacp.PromptParams{})

			events, err := parser.Parse([]byte(`{"method":"item/completed","params":{"item":` + tt.item + `}}` + "\n"))
			if err != nil {
				t.Fatalf("Parse returned error: %v", err)
			}
			if len(events) != 1 {
				t.Fatalf("events len = %d, want one tool finish: %#v", len(events), events)
			}
			if events[0].Update["sessionUpdate"] != "tool_call_update" ||
				events[0].Update["toolCallId"] != "tool-1" ||
				events[0].Update["status"] != "failed" {
				t.Fatalf("tool finish = %#v, want failed tool finish", events[0].Update)
			}
		})
	}
}

func TestCodexStreamParserClassifiesProviderTools(t *testing.T) {
	tests := []struct {
		name  string
		item  string
		kind  string
		title string
	}{
		{
			name:  "shell",
			item:  `{"type":"local_shell_call","id":"shell-1","command":"go test ./..."}`,
			kind:  "execute",
			title: "go test ./...",
		},
		{
			name:  "file read",
			item:  `{"type":"file_read","id":"file-1","path":"README.md"}`,
			kind:  "read",
			title: "README.md",
		},
		{
			name:  "file write",
			item:  `{"type":"file_write","id":"file-2","path":"README.md"}`,
			kind:  "edit",
			title: "README.md",
		},
		{
			name:  "patch",
			item:  `{"type":"apply_patch","id":"patch-1","title":"Apply patch"}`,
			kind:  "edit",
			title: "Apply patch",
		},
		{
			name:  "web search",
			item:  `{"type":"web_search_call","id":"web-1","query":"golang acp"}`,
			kind:  "fetch",
			title: "golang acp",
		},
		{
			name:  "mcp",
			item:  `{"type":"mcp_tool_call","id":"mcp-1","server":"docs","name":"search"}`,
			kind:  "mcp",
			title: "docs/search",
		},
		{
			name:  "image",
			item:  `{"type":"image_generation_call","id":"image-1","prompt":"diagram"}`,
			kind:  "image",
			title: "diagram",
		},
		{
			name:  "plan",
			item:  `{"type":"update_plan","id":"plan-1","title":"Update plan"}`,
			kind:  "plan",
			title: "Update plan",
		},
		{
			name:  "todo",
			item:  `{"type":"todo_write","id":"todo-1","title":"Update TODOs"}`,
			kind:  "todo",
			title: "Update TODOs",
		},
		{
			name:  "review",
			item:  `{"type":"code_review","id":"review-1","title":"Review changes"}`,
			kind:  "review",
			title: "Review changes",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := codexadapter.NewStreamParser(commandbridge.Session{}, runtimeacp.PromptParams{})
			events, err := parser.Parse([]byte(`{"method":"item/started","params":{"item":` + tt.item + `}}` + "\n"))
			if err != nil {
				t.Fatalf("Parse returned error: %v", err)
			}
			if len(events) != 1 {
				t.Fatalf("events len = %d, want one tool start: %#v", len(events), events)
			}
			if events[0].Update["sessionUpdate"] != "tool_call" ||
				events[0].Update["kind"] != tt.kind ||
				events[0].Update["title"] != tt.title {
				t.Fatalf("tool start = %#v, want %s %q", events[0].Update, tt.kind, tt.title)
			}
		})
	}
}

func TestCodexStreamParserCarriesToolMetadataToSparseCompletion(t *testing.T) {
	parser := codexadapter.NewStreamParser(commandbridge.Session{}, runtimeacp.PromptParams{})
	events, err := parser.Parse([]byte(
		`{"method":"item/started","params":{"item":{"type":"mcp_tool_call","id":"mcp-1","server":"docs","tool":"search","arguments":{"query":"acp"}}}}` + "\n" +
			`{"method":"item/completed","params":{"item":{"id":"mcp-1","status":"completed","output":"ok"}}}` + "\n"))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events len = %d, want tool start + finish: %#v", len(events), events)
	}
	finish := events[1].Update
	if finish["sessionUpdate"] != "tool_call_update" ||
		finish["toolCallId"] != "mcp-1" ||
		finish["title"] != "docs/search" ||
		finish["kind"] != "mcp" ||
		finish["status"] != "completed" ||
		finish["rawOutput"] != "ok" {
		t.Fatalf("tool finish = %#v, want MCP metadata carried from start", finish)
	}
}

type sessionUpdate struct {
	Update struct {
		SessionUpdate     string          `json:"sessionUpdate"`
		ToolCallID        string          `json:"toolCallId"`
		Title             string          `json:"title"`
		Kind              string          `json:"kind"`
		Status            string          `json:"status"`
		RawInput          map[string]any  `json:"rawInput"`
		Used              int             `json:"used"`
		Size              int             `json:"size"`
		Content           json.RawMessage `json:"content"`
		UpdatedAt         string          `json:"updatedAt"`
		AvailableCommands []struct {
			Name  string `json:"name"`
			Input struct {
				Unstructured struct {
					Hint string `json:"hint"`
				} `json:"unstructured"`
			} `json:"input"`
		} `json:"availableCommands"`
	} `json:"update"`
}

type permissionRequest struct {
	SessionID string `json:"sessionId"`
	ToolCall  struct {
		ToolCallID string         `json:"toolCallId"`
		Title      string         `json:"title"`
		Kind       string         `json:"kind"`
		Status     string         `json:"status"`
		RawInput   map[string]any `json:"rawInput"`
	} `json:"toolCall"`
	Options []struct {
		OptionID string `json:"optionId"`
		Name     string `json:"name"`
		Kind     string `json:"kind"`
	} `json:"options"`
}

func decodeSessionUpdate(t testing.TB, response acptest.Response) sessionUpdate {
	t.Helper()
	if response.Method != "session/update" {
		t.Fatalf("response method = %q, want session/update", response.Method)
	}
	var update sessionUpdate
	response.ParamsInto(t, &update)
	return update
}

func decodePermissionRequest(t testing.TB, response acptest.Response) permissionRequest {
	t.Helper()
	if response.Method != "session/request_permission" {
		t.Fatalf("response method = %q, want session/request_permission", response.Method)
	}
	var req permissionRequest
	response.ParamsInto(t, &req)
	return req
}

func decodeChunkText(t testing.TB, raw json.RawMessage) string {
	t.Helper()
	var content struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &content); err != nil {
		t.Fatalf("decode chunk content: %v\n%s", err, string(raw))
	}
	return content.Text
}

func updateText(update map[string]any) string {
	content, _ := update["content"].(map[string]any)
	text, _ := content["text"].(string)
	return text
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
