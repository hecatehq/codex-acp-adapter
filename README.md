# codex-acp-adapter

Neutral Go ACP adapter for Codex-compatible coding agents.

This repository is an alpha Go ACP adapter for Codex-compatible coding agents.
It runs as a small, auditable binary that speaks ACP over stdio. The adapter can
run Codex CLI prompts through its native command bridge, but full parity with
the previous Codex ACP adapter is still in progress.

## Goals

- Speak standard Agent Client Protocol over stdio.
- Keep the adapter independent from Hecate internals.
- Avoid package-manager launchers, shell wrappers, and broad environment
  inheritance.
- Preserve the important behavior exposed by the previous Codex ACP adapter:
  sessions, auth, model/config options, permission requests, MCP servers,
  tool updates, terminal output, slash commands, review/init workflows, and
  cancellation.
- Ship deterministic, signed Go release binaries.

## Current Status

Implemented:

- stdlib-only JSON-RPC/NDJSON ACP transport scaffold
- `initialize` response with adapter metadata
- structured errors for unimplemented methods
- source-review notes for the previous adapter behavior
- unit tests for the protocol scaffold
- `doctor` command for probing the local Codex binary boundary
- process-backed runtime launcher seam
- subprocess JSON-RPC client for ACP-style stdio runtime bridges
- ACP initialize client for subprocess runtime negotiation
- typed ACP session lifecycle calls for subprocess runtimes
- ACP server-to-runtime bridge for session methods and streamed updates
- runtime host seam that launches, initializes, and exposes the bridged child
- protocol forwarding for session load, resume, fork, list, delete, and
  MCP-over-ACP message payloads
- command-backed native Codex CLI session path using `codex exec`
- ACP model and reasoning config options for the command-backed path
- CI and tag-driven release packaging for unsigned alpha binaries

Not implemented yet:

- vendor-specific persistent session semantics
- vendor-specific prompt/tool/permission mapping
- runtime config/auth/model discovery
- production signing/provenance for release artifacts

## Development

Shared ACP transport, runtime JSON-RPC, bridge, host, process, doctor runner,
and fake-runtime test code lives in
[acp-adapter-kit](https://github.com/hecatehq/acp-adapter-kit). Keep this repo
focused on the Codex-specific CLI boundary, doctor defaults, docs, release
workflow, and vendor behavior.

The binary remains the primary integration mode. Hosts that need an embedded
adapter can import `github.com/hecatehq/codex-acp-adapter/codexadapter` to build
the same ACP server, info/options, CLI spec, config options, environment
allowlists, and Codex prompt command without shelling out to
`codex-acp-adapter`. The embedded path still launches the underlying `codex` CLI
for prompts; it only removes the extra adapter process boundary.

```sh
make release-check
make snapshot
go test ./...
go test -race ./...
go vet ./...
go run ./cmd/codex-acp-adapter --version
go run ./cmd/codex-acp-adapter doctor
```

See [docs/TESTING.md](docs/TESTING.md) for what is covered today and what must
be covered before this adapter can be used as the default production Codex ACP
bridge.
See [docs/RELEASE.md](docs/RELEASE.md) for the tag-driven release flow.

## CLI Contract

The binary uses Cobra for human commands, but the root command with no arguments
is reserved for ACP stdio. Do not add default logging, banners, usage output, or
prompts to the no-argument path; stdout is the protocol stream.

Use `doctor` before wiring this adapter into an ACP host. It resolves the Codex
binary, runs a fixed-argv version probe through the hardened process runner, and
reports selected environment variable presence without printing secret values.
Use `--binary` to point at a non-default Codex executable and `--json` for
machine-readable output.

By default, the root ACP server owns lightweight ACP sessions and runs each
prompt through `codex exec` in the session workspace. The command-backed path
exposes ACP config options for model and reasoning effort, passes only
provider-specific environment variables through the shared process runner, and
converts command stdout into ACP assistant text while emitting a generic
`tool_call` activity for the native Codex command execution.

The root ACP server can also launch an explicit subprocess-backed ACP runtime
with `--runtime-binary`, `--runtime-workdir`, and repeated `--runtime-arg`
flags. That runtime process receives only the Codex adapter's explicit
environment allowlist (`PATH`, `HOME`, `XDG_CONFIG_HOME`, `TMPDIR`,
`CODEX_HOME`, `OPENAI_API_KEY`, and `OPENAI_BASE_URL`); the parent environment
is not inherited wholesale. Runtime flags override the native command-backed
path and are mostly useful for protocol parity testing.

## Source Review

Before implementing the real bridge, read [docs/SOURCE_REVIEW.md](docs/SOURCE_REVIEW.md).
It records historical package/source behavior that this project needs to
preserve or deliberately replace.
