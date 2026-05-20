# Conventions

Observed from the codebase (not aspirational).

## Module layout

- Go module: `github.com/tristanbietsch/rex`, Go `1.26.3`.
- Binaries under `cmd/<name>/main.go` only.
- Libraries under `internal/<package>/`.
- One package per directory; tests as `*_test.go` in the same package.

## Naming

- JSON / wire fields: `snake_case` struct tags (`tool_id`, `short_id`).
- Go exported identifiers: `PascalCase`.
- Intent/event type strings: `PascalCase` matching const names (`IntentNewSession`, `EventSessionUpdated`).
- Session states: lowercase string enum (`needs_input`, `working`).
- Registry tool ids: lowercase (`claude`, `codex`).
- CLI commands: lowercase single words or hyphenated flags.

## Error handling

- Libraries return `error`; wrap with `%w` where context matters.
- CLI uses `cli.ExitError` + `ExitCoder` for stable exit codes.
- Daemon protocol errors use `EventError` + `ErrCode*` constants (stable for clients).
- Lua handler errors in `OnEvent` are logged, not returned to callers.
- Few panics; `ids.NewSessionID` may panic on RNG failure.

## Logging

- `log/slog` after `rexlog.Init` in daemon and TUI paths.
- Human-facing command output via `fmt` to stdout/stderr; TUI uses Bubble Tea views.
- Daemon startup banners may use stderr; ongoing daemon logs go to file.

## Tests

- Table-driven tests common in `protocol`, `registry`, `cli`.
- Server package has e2e tests with real UDS.
- TUI uses snapshot/string tests (`snapshot_test.go`, `splash_snapshot_test.go`).
- Integration test for summarizer under `cmd/rex-daemon`.

## Concurrency

- `state.Store` uses `sync.RWMutex`; per-session mutex on `Session`.
- Server: one goroutine per connected client; supervisor per PTY.
- `client.Client` not documented as goroutine-safe for concurrent `NextEvent`.
- `audio.Player` safe for concurrent `Play`.
- Lua `Runtime` serialized with internal mutex.

## Dependencies

- Charm stack for TUI (`bubbletea`, `lipgloss`, `x/ansi`).
- No cgo in module (pure Go; oto for audio).
- YAML via `gopkg.in/yaml.v3` for registry and settings.

## Documentation

- Human-oriented docs in repo `docs/` (cli, protocol, tui, etc.).
- Agent-oriented docs in `docs/agents/` (this tree).
- Code comments on exported symbols follow Go doc sentences.

## Git / repo

- `.gitignore` ignores build artifacts `rex`, `rex-daemon`. Agent docs under `docs/agents/` are tracked.
