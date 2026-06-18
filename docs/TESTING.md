# Testing

This repository currently tests the adapter scaffold, not a complete Codex
runtime bridge.

## Covered Today

- CLI version output
- ACP `initialize` response shape
- request ID preservation
- malformed JSON errors without stopping later requests
- invalid JSON-RPC version errors
- notification dispatch without responses
- fake runtime method dispatch through the stdio transport
- fake runtime error propagation
- scaffold `session/prompt` not-implemented errors
- 1 MiB inbound message cap

## Not Covered Yet

These must be tested before Hecate switches from the current Codex ACP adapter to
this one:

- session create/load/resume/list/close
- prompt streaming with assistant chunks and terminal prompt results
- cancellation and no double-settle behavior
- auth methods and auth-required errors
- model/config option discovery and updates
- permission modes
- shell, file, patch, web, MCP, image, plan, review, TODO, and goal tool
  mappings
- permission requests, late permission responses, and rejected/denied tools
- MCP server merging and MCP tool approval elicitations
- environment allowlisting and process hardening
- deterministic release binaries

## Test Strategy

Use `internal/acptest` for all protocol-level tests. It drives the real stdio
JSON-RPC path, so fake runtime tests exercise the same transport Hecate and
other ACP hosts will use.
