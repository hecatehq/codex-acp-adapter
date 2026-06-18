# Roadmap

## Phase 0: Scaffold

- stdio JSON-RPC harness
- source-review notes
- CI for tests

## Phase 1: Protocol Conformance Harness

- typed ACP request/response structs for the methods this adapter supports
- golden transcript tests
- fake ACP client test harness

## Phase 2: Fake Codex Runtime

- fake runtime events for assistant chunks, tool calls, permission requests,
  command output, cancellation, and model options
- tests proving ACP output shape before any real vendor/runtime process is used

## Phase 3: Codex Runtime Bridge

- choose a stable Codex integration boundary
- implement auth/session/prompt/cancel/config/mcp/tool mappings
- port the edge cases recorded in `SOURCE_REVIEW.md`

## Phase 4: Release and Hecate Integration

- signed multi-platform releases
- Hecate registry entry points at `codex-acp-adapter`
- legacy npm launcher becomes explicit opt-in only
