package codexadapter

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/hecatehq/acp-adapter-kit/commandbridge"
	"github.com/hecatehq/acp-adapter-kit/runtimeacp"
)

func NewStreamParser(commandbridge.Session, runtimeacp.PromptParams) commandbridge.StreamParser {
	return commandbridge.NewJSONLStreamParser(mapCodexStreamEvent)
}

func mapCodexStreamEvent(event map[string]any) (commandbridge.JSONLMapping, error) {
	method := firstString(event, "method", "type", "event")
	params := mapValue(event["params"])
	if len(params) == 0 {
		params = event
	}
	if isCodexPermissionRequest(method, params) {
		return mapCodexPermissionRequest(params), nil
	}
	switch method {
	case "item/reasoning/textDelta", "item/reasoning/summaryTextDelta":
		if text := firstText(params, "delta", "text", "summary_text", "summaryText"); text != "" {
			return commandbridge.JSONLMapping{
				Events: []commandbridge.StreamEvent{commandbridge.AgentThoughtChunk(firstString(params, "item_id", "itemId", "id"), text)},
			}, nil
		}
	case "item/started":
		return mapCodexItemStarted(mapValue(params["item"])), nil
	case "item/completed":
		return mapCodexItemCompleted(mapValue(params["item"])), nil
	case "turn/completed":
		return mapCodexTurnCompleted(params), nil
	}
	if text := firstText(params, "delta", "text", "message", "output_text", "final_answer"); text != "" && looksLikeCodexMessage(method) {
		return commandbridge.JSONLMapping{
			Events:         []commandbridge.StreamEvent{commandbridge.AgentMessageChunk(text)},
			TranscriptText: text,
		}, nil
	}
	return commandbridge.JSONLMapping{}, nil
}

func mapCodexItemStarted(item map[string]any) commandbridge.JSONLMapping {
	if !isCodexToolItem(item) {
		return commandbridge.JSONLMapping{}
	}
	id := codexToolID(item)
	if id == "" {
		return commandbridge.JSONLMapping{}
	}
	return commandbridge.JSONLMapping{Events: []commandbridge.StreamEvent{
		commandbridge.ToolCallStart(id, codexToolTitle(item), codexToolKind(item), "in_progress", codexToolRawInput(item)),
	}}
}

func mapCodexItemCompleted(item map[string]any) commandbridge.JSONLMapping {
	itemType := codexItemType(item)
	switch {
	case isCodexAgentMessage(item):
		if text := codexText(item); text != "" {
			return commandbridge.JSONLMapping{
				Events:         []commandbridge.StreamEvent{commandbridge.AgentMessageChunk(text)},
				TranscriptText: text,
			}
		}
	case strings.Contains(itemType, "reasoning"):
		if text := codexText(item); text != "" {
			return commandbridge.JSONLMapping{Events: []commandbridge.StreamEvent{commandbridge.AgentThoughtChunk(codexToolID(item), text)}}
		}
	case isCodexToolItem(item):
		id := codexToolID(item)
		if id == "" {
			return commandbridge.JSONLMapping{}
		}
		return commandbridge.JSONLMapping{Events: []commandbridge.StreamEvent{
			commandbridge.ToolCallFinish(id, codexToolTitle(item), codexToolKind(item), codexToolStatus(item), codexToolRawOutput(item)),
		}}
	}
	return commandbridge.JSONLMapping{}
}

func isCodexPermissionRequest(method string, params map[string]any) bool {
	method = strings.ToLower(method)
	if !strings.Contains(method, "permission") && !strings.Contains(method, "approval") {
		return false
	}
	if strings.Contains(method, "response") ||
		strings.Contains(method, "result") ||
		strings.Contains(method, "resolved") ||
		strings.Contains(method, "decision") {
		return false
	}
	return codexToolID(codexPermissionToolCall(params)) != ""
}

func mapCodexPermissionRequest(params map[string]any) commandbridge.JSONLMapping {
	toolCall := codexPermissionToolCall(params)
	id := codexToolID(toolCall)
	if id == "" {
		return commandbridge.JSONLMapping{}
	}
	title := codexToolTitle(toolCall)
	kind := firstString(toolCall, "kind")
	if kind == "" {
		kind = codexToolKind(toolCall)
	}
	rawInput := codexToolRawInput(toolCall)
	if rawInput == nil {
		rawInput = codexToolRawInput(params)
	}
	return commandbridge.JSONLMapping{Events: []commandbridge.StreamEvent{
		commandbridge.ToolCallPermissionRequest(id, title, kind, rawInput, codexPermissionOptions(params)),
	}}
}

func codexPermissionToolCall(params map[string]any) map[string]any {
	for _, key := range []string{"toolCall", "tool_call", "item", "call"} {
		if value := mapValue(params[key]); len(value) > 0 {
			return value
		}
	}
	return params
}

func codexPermissionOptions(params map[string]any) []commandbridge.PermissionOption {
	for _, key := range []string{"options", "permission_options", "permissionOptions", "choices"} {
		raw := sliceValue(params[key])
		if len(raw) == 0 {
			continue
		}
		options := make([]commandbridge.PermissionOption, 0, len(raw))
		for _, value := range raw {
			option := mapValue(value)
			if len(option) == 0 {
				continue
			}
			options = append(options, commandbridge.PermissionOption{
				OptionID: firstString(option, "optionId", "option_id", "id", "value"),
				Name:     firstString(option, "name", "label", "title"),
				Kind:     firstString(option, "kind", "type"),
			})
		}
		if len(options) > 0 {
			return options
		}
	}
	return nil
}

func mapCodexTurnCompleted(params map[string]any) commandbridge.JSONLMapping {
	mapping := commandbridge.JSONLMapping{StopReason: codexStopReason(params)}
	if text := firstText(params, "final_answer", "output_text", "message"); text != "" {
		mapping.Events = append(mapping.Events, commandbridge.AgentMessageChunk(text))
		mapping.TranscriptText = text
	}
	usage := mapValue(params["usage"])
	if len(usage) == 0 {
		usage = params
	}
	used := sumInts(usage, "input_tokens", "cached_input_tokens", "output_tokens", "reasoning_output_tokens", "total_tokens")
	size := firstInt(usage, "context_window", "context_window_tokens", "size")
	if used > 0 && size > 0 {
		mapping.Events = append(mapping.Events, commandbridge.UsageUpdate(used, size))
	}
	return mapping
}

func looksLikeCodexMessage(method string) bool {
	method = strings.ToLower(method)
	return strings.Contains(method, "message") || strings.Contains(method, "output_text") || strings.Contains(method, "final")
}

func codexStopReason(values map[string]any) runtimeacp.StopReason {
	reason := strings.ToLower(firstString(values, "stop_reason", "stopReason", "finish_reason", "finishReason", "reason", "status", "state"))
	switch {
	case reason == "":
		return ""
	case strings.Contains(reason, "max_turn"):
		return runtimeacp.StopReasonMaxTurnRequests
	case strings.Contains(reason, "max_token"), strings.Contains(reason, "token_limit"), strings.Contains(reason, "length"):
		return runtimeacp.StopReasonMaxTokens
	case strings.Contains(reason, "refusal"), strings.Contains(reason, "refused"), strings.Contains(reason, "safety"):
		return runtimeacp.StopReasonRefusal
	case strings.Contains(reason, "cancel"):
		return runtimeacp.StopReasonCancelled
	case strings.Contains(reason, "end"), strings.Contains(reason, "complete"), strings.Contains(reason, "success"), strings.Contains(reason, "done"):
		return runtimeacp.StopReasonEndTurn
	default:
		return ""
	}
}

func isCodexAgentMessage(item map[string]any) bool {
	itemType := codexItemType(item)
	return itemType == "agent_message" || itemType == "message" || itemType == "assistant" || itemType == "assistant_message"
}

func isCodexToolItem(item map[string]any) bool {
	itemType := codexItemType(item)
	if itemType == "" {
		return firstString(item, "tool_call_id", "toolCallId", "call_id", "callId") != ""
	}
	return strings.Contains(itemType, "tool") ||
		strings.Contains(itemType, "function_call") ||
		strings.Contains(itemType, "exec") ||
		strings.Contains(itemType, "shell") ||
		strings.Contains(itemType, "file") ||
		strings.Contains(itemType, "apply_patch") ||
		strings.Contains(itemType, "write_stdin") ||
		strings.Contains(itemType, "web_search") ||
		strings.Contains(itemType, "mcp") ||
		strings.Contains(itemType, "image") ||
		strings.Contains(itemType, "plan") ||
		strings.Contains(itemType, "todo") ||
		strings.Contains(itemType, "goal") ||
		strings.Contains(itemType, "review")
}

func codexItemType(item map[string]any) string {
	return strings.ToLower(firstString(item, "type", "kind", "item_type", "itemType"))
}

func codexToolID(item map[string]any) string {
	return firstString(item, "tool_call_id", "toolCallId", "call_id", "callId", "id", "response_item_id")
}

func codexToolTitle(item map[string]any) string {
	if title := firstString(item, "title", "display_command", "displayCommand", "command", "name", "namespace"); title != "" {
		return title
	}
	if action := mapValue(item["action"]); len(action) > 0 {
		if title := firstString(action, "display_command", "displayCommand", "command", "name", "namespace"); title != "" {
			return title
		}
	}
	if title := firstString(item, "query", "path", "prompt", "url"); title != "" {
		return title
	}
	if kind := codexItemType(item); kind != "" {
		return strings.ReplaceAll(kind, "_", " ")
	}
	return "Codex tool"
}

func codexToolKind(item map[string]any) string {
	kind := codexItemType(item)
	title := strings.ToLower(codexToolTitle(item))
	switch {
	case strings.Contains(kind, "mcp"):
		return "mcp"
	case strings.Contains(kind, "web_search"), strings.Contains(kind, "web_fetch"), strings.Contains(kind, "webpage"), strings.Contains(title, "web search"):
		return "fetch"
	case strings.Contains(kind, "file_read"), strings.Contains(kind, "read_file"), strings.Contains(title, "read file"):
		return "read"
	case strings.Contains(kind, "apply_patch"), strings.Contains(kind, "write_stdin"), strings.Contains(kind, "file_write"), strings.Contains(kind, "write_file"), strings.Contains(title, "apply_patch"), strings.Contains(title, "edit"):
		return "edit"
	case strings.Contains(kind, "image"):
		return "image"
	case strings.Contains(kind, "plan"):
		return "plan"
	case strings.Contains(kind, "todo"):
		return "todo"
	case strings.Contains(kind, "goal"):
		return "goal"
	case strings.Contains(kind, "review"):
		return "review"
	case strings.Contains(kind, "tool_search"), strings.Contains(title, "search"):
		return "search"
	case strings.Contains(kind, "exec"), strings.Contains(kind, "shell"), strings.Contains(title, "command"), strings.Contains(title, "bash"):
		return "execute"
	case strings.Contains(kind, "reasoning"):
		return "think"
	default:
		return "other"
	}
}

func codexToolStatus(item map[string]any) string {
	status := strings.ToLower(firstString(item, "status", "state"))
	if status == "failed" || status == "error" || status == "cancelled" {
		return "failed"
	}
	if code := firstInt(item, "exit_code", "exitCode"); code != 0 {
		return "failed"
	}
	return "completed"
}

func codexToolRawInput(item map[string]any) any {
	for _, key := range []string{"raw_input", "rawInput", "input", "arguments", "action"} {
		if value, ok := item[key]; ok {
			return value
		}
	}
	if command := firstString(item, "display_command", "displayCommand", "command"); command != "" {
		return map[string]any{"command": command}
	}
	return nil
}

func codexToolRawOutput(item map[string]any) any {
	for _, key := range []string{"raw_output", "rawOutput", "output", "result", "stdout", "stderr"} {
		if value, ok := item[key]; ok {
			return value
		}
	}
	return nil
}

func codexText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		var parts []string
		for _, item := range typed {
			if text := codexText(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "")
	case map[string]any:
		for _, key := range []string{"text", "delta", "output_text", "summary_text", "message", "content"} {
			if text := codexText(typed[key]); text != "" {
				return text
			}
		}
	}
	return ""
}

func firstText(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if text := codexText(values[key]); text != "" {
			return text
		}
	}
	return ""
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			switch typed := value.(type) {
			case string:
				if trimmed := strings.TrimSpace(typed); trimmed != "" {
					return trimmed
				}
			case fmt.Stringer:
				if trimmed := strings.TrimSpace(typed.String()); trimmed != "" {
					return trimmed
				}
			}
		}
	}
	return ""
}

func firstInt(values map[string]any, keys ...string) int {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			if out := intValue(value); out != 0 {
				return out
			}
		}
	}
	return 0
}

func sumInts(values map[string]any, keys ...string) int {
	total := 0
	for _, key := range keys {
		total += intValue(values[key])
	}
	return total
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case jsonNumber:
		n, _ := strconv.Atoi(string(typed))
		return n
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(typed))
		return n
	default:
		return 0
	}
}

func mapValue(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return nil
}

func sliceValue(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	case []map[string]any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, item)
		}
		return out
	default:
		return nil
	}
}

type jsonNumber string
