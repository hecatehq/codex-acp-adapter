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
- ACP model, reasoning, sandbox, and web-search config options for the
  command-backed path
- per-session ACP HTTP and stdio MCP server config propagation into Codex CLI
  `-c mcp_servers.<name>=...` overrides
- in-memory command-backed session load/resume/fork plus bounded transcript
  replay for multi-turn continuity while the adapter process is alive
- command-backed `session/list` metadata, `config_option_update`
  notifications for config changes, and `session_info_update` notifications
  when transcript metadata changes
- command-backed `/review` advertisement and mapping to `codex review
  --uncommitted`, plus `/init` advertisement through the normal `codex exec`
  prompt path
- Codex `exec --json` stream translation into ACP assistant text, reasoning,
  tool-call, and usage updates, including provider-native shell, file, patch,
  web, MCP, image, plan, TODO, goal, and review tool classifications, plus
  generic command `tool_call` activity for the native Codex process
- ACP `logout` mapped to the native `codex logout` command
- CI and tag-driven release packaging for unsigned alpha binaries

Not implemented yet:

- vendor-specific durable/native persistent session semantics across adapter
  process restarts
- complete vendor-specific permission/auth/slash-command mapping beyond ACP
  `logout` and the adapter-owned `/review` and `/init` commands
- vendor-native MCP tool permission and connection-lifecycle mapping beyond
  passing per-session MCP server config into Codex
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
exposes ACP config options for model, reasoning effort, Codex sandbox mode, and
Codex live web search, translates session MCP servers into temporary Codex
`mcp_servers` config overrides, passes only provider-specific environment
variables through the shared process runner, and runs Codex with `exec --json`.
Known Codex JSONL events are
translated into ACP assistant text, reasoning, tool-call, and usage updates;
unknown JSONL events are ignored rather than shown as raw chat text. A generic
`tool_call` still wraps the native Codex process execution so hosts can show the
outer command boundary. The session state is in-memory: `session/load`,
`session/resume`, and `session/fork` work while the adapter process is alive,
`session/list` returns the in-memory session metadata, and later prompts receive
a bounded transcript prelude so command-backed turns keep conversational context
without claiming vendor-native durable history. Config changes return the
current config option list and publish `config_option_update` notifications.
Completed command-backed prompts publish `session_info_update` notifications
with the in-memory title and updated timestamp when transcript metadata changes.
The adapter advertises `/review` and `/init` as ACP available commands:
`/review` maps to Codex's native `review --uncommitted` command, while `/init`
is passed through `codex exec` as a normal Codex slash prompt so Codex can
inspect the workspace and create or update repository agent instructions.
The ACP `logout` method maps to `codex logout`.

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
