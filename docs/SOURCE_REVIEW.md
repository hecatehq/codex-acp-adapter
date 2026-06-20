# Source Review

This scaffold was created after inspecting the published adapter artifact and
upstream adapter source. The goal is to remove package-manager runtime launchers
without losing important protocol behavior.

## Sources Inspected

- published `zed-industries/codex-acp` package artifact, version 0.16.0
- `zed-industries/codex-acp` source repository, shallow clone

## What the Published Package Does

The published package is mostly a platform launcher:

- maps Node `process.platform` and `process.arch` to an optional dependency such
  as `@zed-industries/codex-acp-linux-x64`
- resolves `bin/codex-acp` from that platform package
- spawns the resolved binary with inherited stdio
- exits on unsupported platform/architecture or missing optional dependency

The security issue is not complex bridge logic in the package entrypoint; it is
the runtime dependency on package-manager resolution and optional binary
hydration.

## Behavior the Native Adapter Handles

The upstream native adapter is not a thin process wrapper. It bridges ACP to
Codex internals and currently handles:

- ACP initialization, auth, logout, session create/load/resume/list/close,
  prompt, cancel, session modes, and config options
- OpenAI/Codex auth detection and terminal/device login flows
- model/config option resolution
- MCP server merging from ACP session requests into Codex config
- session roots for filesystem sandboxing
- persisted thread store listing and restore
- permission modes for read-only, workspace/auto, and full-access
- execution and MCP permission requests
- tool-call rendering for shell, file, patch, web, MCP, image generation, plan,
  review, goals, and TODO updates
- terminal output streaming and parallel tool-call updates
- slash commands such as review/init/compact/logout/custom prompts
- shutdown and late permission-response handling

## Go Adapter Requirements

The Go replacement should not merely spawn a CLI and hope for the best. It must
either integrate with a stable Codex protocol surface or faithfully recreate the
ACP mapping above.

Minimum safety requirements:

- no runtime package-manager launchers
- no shell command construction
- fixed argv arrays
- explicit environment allowlist
- bounded JSON message size
- newline-delimited JSON-RPC only on stdout for subprocess protocol bridges
- bounded stdout/stderr capture for subprocess-backed paths
- deterministic platform release artifacts
- fake-Codex protocol tests for sessions, permissions, tools, cancellation, and
  MCP behavior before Hecate switches to this adapter by default
