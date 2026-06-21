//go:build real_cli

package codexadapter_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hecatehq/acp-adapter-kit/acptest"
	"github.com/hecatehq/codex-acp-adapter/codexadapter"
)

const realCLISmokeEnv = "ACP_ADAPTER_REAL_CLI_SMOKE"

func TestRealCodexCLISmoke(t *testing.T) {
	if os.Getenv(realCLISmokeEnv) != "1" {
		t.Skipf("set %s=1 to run the authenticated real Codex CLI smoke", realCLISmokeEnv)
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Fatalf("codex CLI not found in PATH: %v", err)
	}

	client := acptest.NewLiveClient(t, codexadapter.NewServer("real-cli-smoke"), acptest.WithAutoAllowPermissions())
	client.Request("initialize", "initialize", map[string]any{}, 4*time.Minute)

	session := newRealCLISession(t, client, t.TempDir())
	assertRealCLISessionLifecycle(t, client, session.SessionID, session.CWD)

	responses := client.PromptText("prompt-basic", session.SessionID, "Reply briefly with one sentence confirming the Codex ACP adapter real CLI smoke. Do not inspect files or run commands.", 4*time.Minute)
	assertRealCLIPromptCompleted(t, responses, "Codex")

	toolFile := filepath.Join(session.CWD, "acp-real-cli-tool.txt")
	toolResponses := client.PromptText("prompt-tool", session.SessionID, "Use a local shell command or file edit to create acp-real-cli-tool.txt in the current workspace containing exactly codex-acp-real-cli-tool. Then reply with one sentence starting with DONE.", 4*time.Minute)
	assertRealCLIPromptCompleted(t, toolResponses, "Codex")
	assertRealCLIToolFlow(t, toolResponses, "Codex")
	raw, err := os.ReadFile(toolFile)
	if err != nil {
		t.Fatalf("read tool-created file: %v", err)
	}
	if strings.TrimSpace(string(raw)) != "codex-acp-real-cli-tool" {
		t.Fatalf("tool-created file = %q, want codex-acp-real-cli-tool", string(raw))
	}

	cancelSession := newRealCLISession(t, client, t.TempDir())
	cancelResponses := client.PromptTextAndCancel("prompt-cancel", cancelSession.SessionID, "Run a local shell command that sleeps for 30 seconds, then reply with the word done.", 4*time.Second, 4*time.Minute)
	assertRealCLIPromptCancelled(t, cancelResponses, "Codex")
	client.AssertNoLateResponse("prompt-cancel", time.Second)
}

func assertRealCLIPromptCompleted(t testing.TB, responses []acptest.Response, provider string) {
	t.Helper()
	if len(responses) < 3 {
		t.Fatalf("%s prompt responses = %#v, want command start, assistant output, command finish, and prompt result", provider, responses)
	}

	var message strings.Builder
	for _, response := range responses {
		if response.Error != nil {
			t.Fatalf("%s prompt response error: %+v", provider, *response.Error)
		}
		if response.Method != "session/update" {
			continue
		}
		update := decodeSessionUpdate(t, response)
		if update.Update.SessionUpdate == "agent_message_chunk" {
			message.WriteString(decodeChunkText(t, update.Update.Content))
		}
	}
	if strings.TrimSpace(message.String()) == "" {
		t.Fatalf("%s prompt emitted no assistant message chunk: %#v", provider, responses)
	}

	var promptResult struct {
		StopReason string `json:"stopReason"`
	}
	responses[len(responses)-1].ResultInto(t, &promptResult)
	if promptResult.StopReason != "end_turn" {
		t.Fatalf("%s stop reason = %q, want end_turn", provider, promptResult.StopReason)
	}
}

type realCLISession struct {
	SessionID string
	CWD       string
}

func newRealCLISession(t testing.TB, client *acptest.LiveClient, cwd string) realCLISession {
	t.Helper()
	createdResponses := client.Request(acptest.UniqueID("session-new"), "session/new", map[string]any{"cwd": cwd}, 4*time.Minute)
	if len(createdResponses) < 1 {
		t.Fatalf("session/new responses = %#v, want at least a session response", createdResponses)
	}
	var session struct {
		SessionID string `json:"sessionId"`
	}
	createdResponses[len(createdResponses)-1].ResultInto(t, &session)
	if session.SessionID == "" {
		t.Fatal("session id is empty")
	}
	return realCLISession{SessionID: session.SessionID, CWD: cwd}
}

func assertRealCLISessionLifecycle(t testing.TB, client *acptest.LiveClient, sessionID, cwd string) {
	t.Helper()
	listResponses := client.Request("session-list", "session/list", map[string]any{}, 4*time.Minute)
	var list struct {
		Sessions []struct {
			SessionID string `json:"sessionId"`
			CWD       string `json:"cwd"`
		} `json:"sessions"`
	}
	listResponses[len(listResponses)-1].ResultInto(t, &list)
	var found bool
	for _, session := range list.Sessions {
		if session.SessionID == sessionID {
			found = true
			if session.CWD != cwd {
				t.Fatalf("listed cwd = %q, want %q", session.CWD, cwd)
			}
		}
	}
	if !found {
		t.Fatalf("session/list = %#v, want %q", list.Sessions, sessionID)
	}

	loadResponses := client.Request("session-load", "session/load", map[string]any{"sessionId": sessionID, "cwd": cwd}, 4*time.Minute)
	var load struct {
		ConfigOptions []struct {
			ID string `json:"id"`
		} `json:"configOptions"`
	}
	loadResponses[len(loadResponses)-1].ResultInto(t, &load)
	if len(load.ConfigOptions) == 0 {
		t.Fatalf("session/load result = %#v, want config options", load)
	}
}

func assertRealCLIToolFlow(t testing.TB, responses []acptest.Response, provider string) {
	t.Helper()
	var sawToolStart, sawToolFinish, sawPermission bool
	for _, response := range responses {
		if response.Error != nil {
			t.Fatalf("%s tool-flow response error: %+v", provider, *response.Error)
		}
		switch response.Method {
		case "session/request_permission":
			sawPermission = true
		case "session/update":
			update := decodeSessionUpdate(t, response)
			if strings.HasPrefix(update.Update.ToolCallID, "prompt-command-") {
				continue
			}
			switch update.Update.SessionUpdate {
			case "tool_call":
				sawToolStart = true
			case "tool_call_update":
				if update.Update.Status == "completed" {
					sawToolFinish = true
				}
			}
		}
	}
	if !sawToolStart || !sawToolFinish {
		t.Fatalf("%s tool-flow responses did not include completed provider tool updates: start=%v finish=%v permission=%v responses=%#v", provider, sawToolStart, sawToolFinish, sawPermission, responses)
	}
}

func assertRealCLIPromptCancelled(t testing.TB, responses []acptest.Response, provider string) {
	t.Helper()
	if len(responses) < 2 {
		t.Fatalf("%s cancel responses = %#v, want command lifecycle and prompt result", provider, responses)
	}
	var terminalResponses int
	for _, response := range responses {
		if response.Error != nil {
			t.Fatalf("%s cancel response error: %+v", provider, *response.Error)
		}
		if response.Method == "" {
			terminalResponses++
		}
	}
	if terminalResponses != 1 {
		t.Fatalf("%s terminal prompt responses = %d, want exactly one: %#v", provider, terminalResponses, responses)
	}
	var promptResult struct {
		StopReason string `json:"stopReason"`
	}
	responses[len(responses)-1].ResultInto(t, &promptResult)
	if promptResult.StopReason != "cancelled" {
		t.Fatalf("%s stop reason = %q, want cancelled", provider, promptResult.StopReason)
	}
}
