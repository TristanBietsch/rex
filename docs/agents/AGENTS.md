# Agent hints

Router for AI agents working in this repository.

## Where to start

| Task type | Start |
|-----------|--------|
| Navigation / "what is this?" | [README.md](README.md) |
| Layout / new file placement | [../STRUCTURE.md](../STRUCTURE.md) |
| Edit CLI | [modules/cli.md](modules/cli.md), [interfaces/cli.md](interfaces/cli.md) |
| Edit TUI | [modules/tui.md](modules/tui.md) |
| Edit daemon / sessions | [modules/server.md](modules/server.md), [modules/pty.md](modules/pty.md) |
| Protocol or API shape | [modules/protocol.md](modules/protocol.md), [data/schemas.md](data/schemas.md) |
| Config / env | [interfaces/config.md](interfaces/config.md) |
| Build & test | [WORKFLOWS.md](WORKFLOWS.md) |

## Authority

| Source | Role |
|--------|------|
| Go source | Wins on any conflict |
| `docs/agents/` | Agent-oriented map of current code |
| Repo `docs/` | Human specs and design history; may be stale |

If human `docs/protocol.md` (etc.) disagrees with `internal/wire/protocol`, trust source and note discrepancy in your PR message; do not "fix" code to match old specs without user intent.

## Safe vs careful edits

| Safe (usual feature work) | Careful |
|---------------------------|---------|
| `internal/surface/cli/*.go`, `internal/surface/tui/*.go` | `internal/wire/protocol` — breaking wire changes |
| `internal/catalog/settings/registry.go` (add settings) | `internal/catalog/registry/builtin.yaml` — affects all installs |
| `internal/catalog/registry/builtin.yaml` (new tools) | `go.mod` version bumps |
| Tests alongside changes | `.gitignore` (currently ignores `docs/agents/`) |

No generated Go in repo. No vendored tree. `install.sh` and `Makefile` affect user installs.

## Thin documentation (read source)

| Area | Why |
|------|-----|
| `internal/surface/tui/keymap.go` | Key bindings not fully listed in module doc |
| `internal/daemon/server/client.go` | Full intent switch is long; read for handler behavior |
| `internal/surface/cli/*` per-command flags | Only common flags in [interfaces/cli.md](interfaces/cli.md) |
| Slash commands | See repo `docs/slash.md` + `internal/surface/tui/command.go` |

## Commit / PR conventions

No `CONTRIBUTING.md` in repo. Commit history uses short imperative subjects (`Add …`, `Fix …`, `Refactor …`). No enforced conventional-commits types.

## Known doc gaps (this pass)

- `IntentOpenSession`, `IntentShutdown` documented as unimplemented in server.
- `TestLoad_UserExtends` failing in `internal/catalog/registry` at time of doc write.
- `docs/agents/` directory is gitignored; these docs may not be tracked unless `.gitignore` changes.
