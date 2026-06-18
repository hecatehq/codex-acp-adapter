# Roadmap

## Phase 0: Scaffold

- stdio JSON-RPC harness
- source-review notes
- CI for tests

## Phase 1: Protocol Conformance Harness

- typed ACP request/response structs for the methods this adapter supports
- golden transcript tests
- fake ACP client test harness
- fake runtime method/notification dispatch through the real stdio path

## Phase 2: Fake Codex Runtime

- fake runtime events for assistant chunks, tool calls, permission requests,
  command output, cancellation, and model options
- tests proving ACP output shape before any real vendor/runtime process is used
- session lifecycle coverage for create, prompt, cancel, and close

## Phase 3: Codex Runtime Bridge

- use `internal/process` for every subprocess boundary
- use `internal/runtimeproc` as the only process-backed runtime launcher
- use `internal/runtimejsonrpc` for newline-delimited JSON-RPC over runtime
  stdio
- use `runtimejsonrpc.Client.Respond` for child runtime requests that need a
  JSON-RPC response instead of treating every child request as terminal
- use `acp.MethodContext.Request` when a runtime child request must be
  forwarded to the ACP client and answered before the prompt can finish
- use `internal/runtimeacp` for subprocess ACP lifecycle negotiation
- build real session bridges on the typed `runtimeacp` initialize/session
  calls before adding vendor-specific tool mappings
- use `internal/runtimebridge` as the ACP server handler seam for
  subprocess-backed sessions
- keep session load/resume/list/delete protocol forwarding in
  `runtimeacp`/`runtimebridge`; vendor-specific persistence belongs above it
- use `internal/runtimehost` to compose process launch, runtime initialize, and
  ACP server bridge options
- expose the subprocess-backed runtime path only through explicit root runtime
  flags until the Codex boundary is stable
- keep `doctor` green against the target Codex binary before enabling real
  runtime sessions
- choose a stable Codex integration boundary
- implement auth/session/prompt/cancel/config/mcp/tool mappings
- port the edge cases recorded in `SOURCE_REVIEW.md`

## Phase 4: Release and Hecate Integration

- signed multi-platform releases
- Hecate registry entry points at `codex-acp-adapter`
- legacy npm launcher becomes explicit opt-in only
