# Codex ACP Adapter

This repository should stay thin and Codex-specific.

Keep shared protocol/runtime/CLI plumbing in
`github.com/hecatehq/acp-adapter-kit`, not copied into this repo. If a change is
useful for both Codex and Claude Code adapters, update the kit first, merge it,
then consume the new kit version here.

Codex-specific behavior belongs here:

- adapter name/title/capabilities;
- default Codex binary and doctor wording;
- Codex/OpenAI environment allowlist;
- Codex-specific ACP quirks, docs, and release workflow.

Do not add package-manager runtime launch paths. Process-backed paths must use
fixed argv arrays, explicit environment allowlists, bounded output capture, and
the kit runtime/process seams.

When changing code, add or update tests and run:

```sh
go test ./...
go vet ./...
```
