# Paths and environment

Canonical list of files and directories Rex uses. See [glossary.md](glossary.md) for terms.

## Directories

| Path | Contents |
|------|----------|
| `~/.config/rex/` | User configuration |
| `~/.local/share/rex/` | Session state (default `-state-dir`) |
| `~/.local/share/rex/sessions/<id>/` | Per-session `meta.json` and `transcript.log` |
| `~/.local/state/rex/` | Daemon stderr log, slog files, TUI resume state |
| `~/.cache/rex/` | Socket when `XDG_RUNTIME_DIR` is unset |
| `$XDG_RUNTIME_DIR/rex.sock` | Socket when `XDG_RUNTIME_DIR` is set |
| `~/.local/bin/` | Default install location for `rex` and `rex-daemon` |

`~/.local/share/rex` holds durable session data. `~/.local/state/rex` holds logs and ephemeral TUI state. They are not the same tree.

## Files

| Path | Written by | Purpose |
|------|------------|---------|
| `~/.config/rex/config.yaml` | `rex setup`, TUI settings, `rex config` | User settings (flat keys) |
| `~/.config/rex/tools.yaml` | User | Tool registry overlay |
| `~/.config/rex/init.lua` | User | Optional Lua hooks |
| `~/.local/state/rex/tui-state.json` | TUI on `:bg` / detach | Selection and filter; read once on next launch, then deleted |
| `~/.local/state/rex/daemon.log` | `rex daemon start` | Daemon stderr |
| `~/.local/state/rex/<name>.log` | `rexlog` | Structured logs (`daemon`, `tui`, `setup`) |

## Socket

Default Unix domain socket path:

1. `$XDG_RUNTIME_DIR/rex.sock` when `XDG_RUNTIME_DIR` is set
2. Otherwise `~/.cache/rex/rex.sock`

Override with `-socket` on `rex-daemon` or `-socket` on CLI commands that dial the daemon.

## Environment variables

| Variable | Read by | Effect |
|----------|---------|--------|
| `XDG_RUNTIME_DIR` | `daemonctl` | Default socket directory |
| `REX_LOG_LEVEL` | `rexlog` | `debug`, `info`, `warn`, `error` (default `info`) |
| `REX_LOG_DIR` | `rexlog` | Override log directory under state |
| `OLLAMA_HOST` | `rex-daemon` | Ollama base URL for summarizer |
| `EDITOR` | `rex config edit` | Program to edit `init.lua` |
| `GOPATH` | `rex update` | Module install path resolution |
| `SHELL` | `rex setup` | Shell profile target for PATH block |

`REX_SOCKET` is not used for production socket resolution.

## Install

| Method | Binaries |
|--------|----------|
| `./install.sh` | `$(PREFIX)/bin/rex`, `rex-daemon` (default `PREFIX=$HOME/.local`) |
| `make install PREFIX=…` | Same |
| `go install github.com/tristanbietsch/rex/cmd/rex@latest` | `$GOPATH/bin` or `$HOME/go/bin` |

`install.sh` stops a running daemon before replacing binaries. The daemon stays stopped until the next `rex` or `rex daemon start`.

## See also

- [settings.md](settings.md) — `config.yaml` keys
- [registry.md](registry.md) — `tools.yaml`
- [daemon.md](daemon.md) — daemon flags
- [cli.md](cli.md) — `-socket` on commands
