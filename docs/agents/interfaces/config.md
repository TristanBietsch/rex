# Configuration

Paths, flags, environment variables, and settings keys referenced in code.

## Paths (defaults)

| Path | Set by | Read in |
|------|--------|---------|
| `~/.config/rex/config.yaml` | user / setup | `settings.DefaultPath()`, `internal/surface/cli/setup.go` |
| `~/.config/rex/tools.yaml` | user | `rex-daemon -tools`, `cmd/rex-daemon/main.go` `defaultToolsPath()` |
| `~/.config/rex/init.lua` | user | setting `lua_config_path`; `internal/features/lua` |
| `~/.local/share/rex` | `rex-daemon -state-dir` | daemon, `internal/surface/cli/log.go` `defaultStateDir()` |
| `~/.local/state/rex/` | — | `rexlog`, `tui/persist.go` |
| `~/.local/state/rex/tui-state.json` | TUI exit | `internal/surface/tui/persist.go` |
| `$XDG_RUNTIME_DIR/rex.sock` or `~/.cache/rex/rex.sock` | — | `daemonctl.DefaultSocket`, `rex-daemon -socket` |
| `~/.local/bin/rex`, `rex-daemon` | `make install` / `install.sh` | install targets |

Legacy migration: `install.sh --migrate` mentions `~/.rex` → `~/.local/state/rex` (interactive).

## Environment variables

| Name | Where read | Effect |
|------|------------|--------|
| `XDG_RUNTIME_DIR` | `daemonctl`, `rex-daemon` | Default socket directory |
| `REX_LOG_LEVEL` | `internal/runtime/rexlog` | slog level: debug, info, warn, error (default info) |
| `REX_LOG_DIR` | `internal/runtime/rexlog` | Override log directory |
| `OLLAMA_HOST` | `cmd/rex-daemon/main.go` | Ollama base URL for summarizer (adds `http://` if missing) |
| `EDITOR` | `internal/surface/cli/config.go` | `rex config edit` |
| `GOPATH` | `internal/surface/cli/update.go` | upgrade path resolution |
| `SHELL` | `internal/surface/cli/setup.go`, `internal/surface/tui/setup_wizard.go` | shell profile block target |
| `LC_ALL`, `LANG` | `internal/surface/tui/splash_steps.go` | boot diagnostics display |
| `COLORTERM` | `internal/surface/tui/splash_steps.go` | boot diagnostics |

`REX_SOCKET` appears only in test fixture strings (`uninstall_test.go`), not in production socket resolution.

## rex-daemon flags

| Flag | Default | Code |
|------|---------|------|
| `-socket` | see socket paths above | `cmd/rex-daemon/main.go` |
| `-state-dir` | `~/.local/share/rex` | same |
| `-tools` | `~/.config/rex/tools.yaml` | same |
| `-max-concurrent-sessions` | `16` | same |
| `-version` | — | prints `v1` |

## Settings keys (`settings.Registry`)

Authoritative list and defaults: `internal/catalog/settings/registry.go`. Sections: Appearance, Audio, Behavior, Summary, Spawn, Advanced. See `modules/settings.md` for IDs.

Changing `max_concurrent_sessions` in YAML does not hot-reload daemon cap; use running daemon `SetMaxConcurrent` intent or restart with flag.

## CLI flags shared across commands

Many commands accept `-socket` (default `cli.DefaultSocket()`). Others are per-command — see `interfaces/cli.md`.

## install.sh / Makefile

| Variable | Default | Purpose |
|----------|---------|---------|
| `PREFIX` | `$HOME/.local` | Install prefix for binaries |
