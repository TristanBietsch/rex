# Rex — agent documentation

Read this file first. For directory layout and “where does new code go?”, see [`../STRUCTURE.md`](../STRUCTURE.md).

Rex is a Go terminal kanban for parallel AI coding-agent sessions: `rex` (TUI/CLI) talks to `rex-daemon` (PTY supervisor) over newline-delimited JSON on a Unix socket.

## Modules

| Module | Description |
|--------|-------------|
| [cmd](modules/cmd.md) | `rex` and `rex-daemon` entrypoints |
| [cli](modules/cli.md) | All `rex` subcommands |
| [tui](modules/tui.md) | Bubble Tea board |
| [server](modules/server.md) | UDS server and session handlers |
| [client](modules/client.md) | Protocol client SDK |
| [protocol](modules/protocol.md) | Wire format |
| [state](modules/state.md) | Session store and persistence |
| [pty](modules/pty.md) | Per-session PTY supervisor |
| [adapter](modules/adapter.md) | Output → state classification |
| [registry](modules/registry.md) | tools.yaml + builtin agents |
| [settings](modules/settings.md) | config.yaml settings registry |
| [summarizer](modules/summarizer.md) | Ollama descriptions |
| [daemonctl](modules/daemonctl.md) | Spawn/check daemon |
| [audio](modules/audio.md) | UI sounds |
| [lua](modules/lua.md) | Optional daemon scripting |
| [ids](modules/ids.md) | Session UUID / short id |
| [rexlog](modules/rexlog.md) | File logging for slog |

## Where to start

| Task | Start here |
|------|------------|
| Add CLI command | `cmd/rex/main.go`, `internal/surface/cli/`, [interfaces/cli.md](interfaces/cli.md) |
| Change wire protocol | `internal/wire/protocol/`, [data/schemas.md](data/schemas.md), `internal/daemon/server/client.go` |
| TUI behavior / keys | `internal/surface/tui/update.go`, `keymap.go`, [modules/tui.md](modules/tui.md) |
| Spawn or agent command line | `internal/catalog/registry/`, `internal/daemon/server/client.go` `handleNewSession` |
| Session persistence | `internal/daemon/state/persist.go`, [data/schemas.md](data/schemas.md) |
| New user setting | `internal/catalog/settings/registry.go` only |
| Add built-in tool | `internal/catalog/registry/builtin.yaml`, [modules/registry.md](modules/registry.md) |
| Daemon lifecycle | `cmd/rex-daemon/main.go`, `internal/daemon/boot/`, [modules/server.md](modules/server.md) |

## Build, test, run

From [WORKFLOWS.md](WORKFLOWS.md):

```sh
make build
make test
rex
```

## Other docs

| File | Contents |
|------|----------|
| [ARCHITECTURE.md](ARCHITECTURE.md) | Processes, data flow, dependencies |
| [GLOSSARY.md](GLOSSARY.md) | Project terms |
| [CONVENTIONS.md](CONVENTIONS.md) | Code style observed in repo |
| [WORKFLOWS.md](WORKFLOWS.md) | Make targets and commands |
| [DECISIONS.md](DECISIONS.md) | Non-obvious design choices |
| [AGENTS.md](AGENTS.md) | Meta: authority, safe edits, gaps |
| [interfaces/cli.md](interfaces/cli.md) | CLI commands and flags |
| [interfaces/config.md](interfaces/config.md) | Paths, env, settings keys |
| [data/schemas.md](data/schemas.md) | Wire and disk JSON shapes |

## Human documentation

| File | Contents |
|------|----------|
| [../index.md](../index.md) | Documentation map |
| [../cli.md](../cli.md) | CLI reference |
| [../tui.md](../tui.md), [../slash.md](../slash.md) | TUI and command palette |
| [../settings.md](../settings.md), [../registry.md](../registry.md) | User YAML |
| [../protocol.md](../protocol.md), [../daemon.md](../daemon.md) | Wire and supervisor |
| [../architecture.md](../architecture.md) | System design |
| [../quickstart.md](../quickstart.md) | First session |

Go source wins on conflict.
