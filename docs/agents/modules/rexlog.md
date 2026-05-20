# rexlog

**Path:** `internal/runtime/rexlog`
**Depends on:** (none) | external: standard library (`log/slog`, `os`, `path/filepath`, `strings`)
**Depended on by:** `cmd/rex-daemon`, `internal/surface/cli`, `internal/surface/tui`
**Entry points:** `Init`, `Close`

## Purpose

Redirects `slog` default logger to a per-binary log file under `~/.local/state/rex/` because stdout/stderr are reserved (TUI alt-screen, daemon banners).

## Public surface

### Functions

- `Init(name string)` — opens `~/.local/state/rex/<name>.log` (or `REX_LOG_DIR/<name>.log`), installs slog default. Idempotent (first call wins).
- `Close()` — flushes and closes the log file.

Environment (read in `rexlog.go`, not exported):

- `REX_LOG_LEVEL` — `debug|info|warn|error` (default `info`).
- `REX_LOG_DIR` — overrides log directory.

## Internal structure

- `rexlog.go` — single file, no tests.

## Invariants

`Init` must run before meaningful `slog` usage in `rex` TUI path and `rex-daemon`.

## Side effects

Creates/appends log files on disk. Sets global `slog` default.

## Error handling

Open failures fall back to stderr-only logging (implementation detail in source).

## Tests

None in package.

## Gotchas

No `rexlog` calls in `internal/daemon/server` or PTY path; daemon logs via `Init("daemon")`, TUI via `Init("tui")` from `internal/surface/cli/tui.go`.

## See also

- `interfaces/config.md` — `REX_LOG_LEVEL`, `REX_LOG_DIR`.
- `WORKFLOWS.md` — log file locations.
