# internal/lua

Sandboxed Lua scripting runtime for the rex daemon.

## Overview

The daemon loads `~/.config/rex/init.lua` (path configured via `lua_config_path` setting) once at startup. The script can register handlers for session lifecycle events and send text to session PTYs.

## API

| Function | Signature | Description |
|---|---|---|
| `rex.on` | `rex.on(event_name, fn)` | Register a handler for an event. |
| `rex.send` | `rex.send(session_id, text)` | Send text to a session's PTY. |
| `rex.list` | `rex.list() -> table` | Snapshot of all current sessions. |
| `rex.log` | `rex.log(level, msg)` | Log via the daemon's slog logger. |

### Events

| Name | Argument fields |
|---|---|
| `session_added` | `id`, `short_id`, `slug`, `title`, `state`, `tool_id`, `model_id`, `cwd` |
| `session_updated` | same as above (sparse — fields that did not change may still be present) |
| `session_removed` | `session_id` |

### Session states

`queued`, `working`, `needs_input`, `done`, `failed`, `crashed`

## Threading model

`*lua.LState` is not goroutine-safe. All entry points (`LoadFile`, `OnEvent`) are serialized via an internal mutex. Injected Go callbacks (`Sender`, `Lister`) are invoked while the mutex is held — they must not call back into the `Runtime`.

## Standard libraries loaded

`base`, `table`, `string`, `math`, `package`. The `os`, `io`, `coroutine`, and `debug` libraries are **not** loaded to limit the attack surface.

## Security caveats

- The script runs **in-process** as the daemon user with full access to the `rex.*` Go callbacks.
- `rex.send` can inject arbitrary text into any session's PTY.
- Do **not** load untrusted scripts. The restricted stdlib reduces but does not eliminate risk.
- There is no resource limit on CPU or memory within the Lua state.

## Wiring (daemon integration)

The package is self-contained. To wire it into the daemon:

1. Instantiate a `*lua.Runtime` via `lua.New(lua.Options{...})` with:
   - `Sender`: submit a `protocol.Reply` or `protocol.SendInput` intent through the server.
   - `Lister`: return a snapshot of `[]protocol.SessionSummary` from the state store.
2. Call `runtime.LoadFile(resolvedLuaConfigPath)` after the state store is ready.
3. In the event broadcast path, call `runtime.OnEvent(eventType, data)` for `SessionAdded`, `SessionUpdated`, and `SessionRemoved` events.
4. Call `runtime.Close()` on daemon shutdown.
