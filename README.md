# codex-acp-adapter

Neutral Go ACP adapter for Codex-compatible coding agents.

This repository is an early scaffold. It is intended to replace runtime npm
bridge launchers with a small, auditable Go adapter that speaks ACP over stdio.
It is not ready to use as a production Codex bridge yet.

## Goals

- Speak standard Agent Client Protocol over stdio.
- Keep the adapter independent from Hecate internals.
- Avoid runtime `npx`, shell wrappers, and broad environment inheritance.
- Preserve the important behavior exposed by the current Codex ACP adapter:
  sessions, auth, model/config options, permission requests, MCP servers,
  tool updates, terminal output, slash commands, review/init workflows, and
  cancellation.
- Ship deterministic, signed Go release binaries.

## Current Status

Implemented:

- stdlib-only JSON-RPC/NDJSON ACP transport scaffold
- `initialize` response with adapter metadata
- structured errors for unimplemented methods
- source-review notes for the current npm/native adapter stack
- unit tests for the protocol scaffold

Not implemented yet:

- Codex CLI/native runtime integration
- session creation/load/resume/list
- prompt streaming
- tool/permission mapping
- cancellation
- model discovery
- release packaging

## Development

```sh
go test ./...
go run ./cmd/codex-acp-adapter --version
```

See [docs/TESTING.md](docs/TESTING.md) for what is covered today and what must
be covered before this adapter can replace the current Codex ACP bridge.

## CLI Contract

The binary uses Cobra for human commands, but the root command with no arguments
is reserved for ACP stdio. Do not add default logging, banners, usage output, or
prompts to the no-argument path; stdout is the protocol stream.

## Source Review

Before implementing the real bridge, read [docs/SOURCE_REVIEW.md](docs/SOURCE_REVIEW.md).
It records the behavior found in the current npm package and upstream adapter
source that this project needs to preserve or deliberately replace.
ACP adapter for running Codex-compatible coding agents over stdio
