# lua

**Path:** `internal/features/lua`
**Depends on:** `internal/wire/protocol` | external: `github.com/yuin/gopher-lua`, standard library
**Depended on by:** `cmd/rex-daemon`
**Entry points:** `New`, `(*Runtime).LoadFile`, `(*Runtime).OnEvent`

## Purpose

Optional in-process Lua scripting: user `init.lua` registers `rex.on` handlers for daemon events; narrow API for list/send/log.

## Public surface

### Types

- `Options` — `Logger`, `Sender func(sessionID, text string) error`, `Lister func() []protocol.SessionSummary`.
- `Runtime` — `L *lua.LState`, `New(opts Options) (*Runtime, error)`, `Close()`, `LoadFile(path string) error`, `OnEvent(eventType string, data any) error`.

## Internal structure

- `runtime.go` — mutex-serialized `LState`, event dispatch.
- `api.go` — `rex` table registration.
- `README.md`, `example_init.lua` — user-facing samples (not loaded automatically).
- `runtime_test.go`

## Invariants

`LState` is not goroutine-safe; all `LoadFile` / `OnEvent` calls hold an internal mutex. Injected `Sender` / `Lister` run under that mutex and must not re-enter `Runtime`.

Missing script file: `LoadFile` logs info and returns nil (no error).

## Side effects

Executes user Lua as daemon OS user. `Sender` may write to session PTY via daemon wiring.

## Error handling

Lua syntax/runtime errors from `LoadFile` propagate. `OnEvent` catches handler errors, logs, does not propagate.

## Tests

`runtime_test.go` — API and handler registration.

## Gotchas

Standard Lua `os`/`io` libraries are not loaded; script is still trusted code in-process. Path from setting `lua_config_path` (default `~/.config/rex/init.lua`). Daemon skips runtime entirely if load fails at startup (see `cmd/rex-daemon/main.go`).

## See also

- `modules/settings.md` — `lua_config_path`.
- `internal/features/lua/README.md` — API listing for humans.
