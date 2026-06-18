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
- `doctor` command for probing the local Codex binary boundary
- process-backed runtime launcher seam
- subprocess JSON-RPC client for ACP-style stdio runtime bridges
- ACP initialize client for subprocess runtime negotiation
- typed ACP session lifecycle calls for subprocess runtimes
- ACP server-to-runtime bridge for session methods and streamed updates
- runtime host seam that launches, initializes, and exposes the bridged child

Not implemented yet:

- Codex CLI/native runtime integration
- real runtime-backed session load/resume/list
- vendor-specific prompt/tool/permission mapping
- runtime config/auth/model discovery
- release packaging

## Development

```sh
go test ./...
go test -race ./...
go vet ./...
go run ./cmd/codex-acp-adapter --version
go run ./cmd/codex-acp-adapter doctor
```

See [docs/TESTING.md](docs/TESTING.md) for what is covered today and what must
be covered before this adapter can replace the current Codex ACP bridge.

## CLI Contract

The binary uses Cobra for human commands, but the root command with no arguments
is reserved for ACP stdio. Do not add default logging, banners, usage output, or
prompts to the no-argument path; stdout is the protocol stream.

Use `doctor` before wiring this adapter into an ACP host. It resolves the Codex
binary, runs a fixed-argv version probe through the hardened process runner, and
reports selected environment variable presence without printing secret values.
Use `--binary` to point at a non-default Codex executable and `--json` for
machine-readable output.

## Source Review

Before implementing the real bridge, read [docs/SOURCE_REVIEW.md](docs/SOURCE_REVIEW.md).
It records the behavior found in the current npm package and upstream adapter
source that this project needs to preserve or deliberately replace.
ACP adapter for running Codex-compatible coding agents over stdio
