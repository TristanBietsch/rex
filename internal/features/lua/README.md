# internal/lua

Embedded Lua scripting runtime for the rex daemon.

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
| `session_updated` | same as above: the session's full current state, not just what changed |
| `session_removed` | `session_id` |

### Session states

`queued`, `working`, `needs_input`, `done`, `failed`, `crashed`

## Event delivery

Handlers never run on the daemon's state-store path. Store events go into a `Dispatcher`: a bounded queue (`DefaultQueueSize`, 256) drained by one goroutine, so handlers run one at a time, in order.

- `session_updated` is coalesced per session: while an update for a session is still queued, a newer one replaces it in place. A busy session takes one slot, and handlers see its latest state rather than every intermediate patch.
- When the queue is full, new events are dropped and counted. The daemon logs `lua: event queue full; dropping events` at most once every 10 seconds, with the number dropped.
- Events for one session keep their order.

## Timeouts

| What | Bound | Constant |
|---|---|---|
| Top-level run of `init.lua` at startup | 5 s | `DefaultLoadTimeout` |
| Each handler call | 3 s | `DefaultHandlerTimeout` |
| `rex.send` waiting for a full input channel | 2 s | `DefaultSendTimeout` |

A script that runs past its bound (for example `while true do end`) is interrupted with a Lua error and logged (`lua: handler timed out`). The runtime stays usable and later events are still delivered. A load that times out keeps any handlers registered before the interruption. Daemon shutdown interrupts a handler in flight.

These bounds protect the daemon's **liveness**, not its security. They do not limit memory, and time spent blocked inside a Go callback is bounded only by that callback's own timeout.

## Threading model

`*lua.LState` is not goroutine-safe. All entry points (`LoadFile`, `OnEvent`, `OnEventContext`) are serialized via an internal mutex. Injected Go callbacks (`Sender`, `Lister`) are invoked while the mutex is held — they must not call back into the `Runtime`.

## Standard libraries loaded

`base`, `table`, `string`, `math`, `package`. The `os`, `io`, `coroutine`, and `debug` libraries are not loaded. That keeps the API small; it is **not** a sandbox (see below).

## Trust model

`init.lua` is **trusted user code**, like a shell rc file:

- `LoadFile` reads it through `internal/runtime/privfile`: a regular file (or a link to one) owned by the daemon user, not group- or world-writable, at most 1 MiB, in a folder with the same rule. Anything else is refused with a `lua: refusing init.lua` log line.

- It runs in-process as the daemon user, with full access to the `rex.*` Go callbacks.
- `require`, `dofile`, `loadfile` and `load` are available, so it can run any Lua file the user can read.
- `rex.send` can type arbitrary text, and therefore commands, into any session's PTY.
- Only load a script you wrote or reviewed. The timeouts above keep a buggy script from hanging the daemon; they do not contain a malicious one.

## Not to be confused with `config.lua`

`~/.config/rex/config.lua` (settings and themes, read by every rex process) is evaluated by `internal/catalog/settings/luacfg`, not by this runtime: a fresh, restricted state per evaluation with only `rex.set`, `rex.theme`, `rex.platform` and a `require` limited to `~/.config/rex/lua/`. See `docs/howto/lua-config.md`.

## Wiring (daemon integration)

`boot.StartLuaHost` does this for the daemon (and rebuilds it on `rex reload`):

1. Instantiate a `*lua.Runtime` via `lua.New(lua.Options{...})` with:
   - `Sender`: deliver the text to the session's PTY input channel.
   - `Lister`: return a snapshot of `[]protocol.SessionSummary` from the state store.
2. Call `runtime.LoadFile(resolvedLuaConfigPath)` after the state store is ready.
3. Create a `lua.NewDispatcher(runtime, lua.DispatcherOptions{})` and, from a store subscriber, call `dispatcher.Enqueue(eventType, sessionID, data)` for `SessionAdded`, `SessionUpdated`, and `SessionRemoved` events. `Enqueue` never blocks.
4. On daemon shutdown, unsubscribe, call `dispatcher.Close()` (it waits for the delivery goroutine), and call `runtime.Close()`. Either close may come first.
