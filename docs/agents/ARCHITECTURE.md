# Architecture

Rex is a terminal kanban for parallel AI coding-agent sessions. Two processes: `rex` (client) and `rex-daemon` (supervisor).

## Process model

```
┌─────────────┐     JSONL/UDS      ┌──────────────────┐
│ rex (TUI/   │ ◄──────────────► │ rex-daemon       │
│  CLI)       │   protocol.      │  - server (UDS)  │
└─────────────┘   Envelope       │  - state.Store   │
                                 │  - N × pty.      │
                                 │    Supervisor    │
                                 └────────┬─────────┘
                                          │ spawn
                                          ▼
                                 ┌──────────────────┐
                                 │ agent CLIs       │
                                 │ (claude, codex,  │
                                 │  echo, …)        │
                                 └──────────────────┘
```

- **Client** (`internal/wire/client`): dial socket, `Hello`, subscribe, send intents.
- **Server** (`internal/daemon/server`): accept connections, mutate `state.Store`, spawn `pty.Supervisor` per session.
- **PTY** (`internal/daemon/pty`): one child process, adapter classification, transcript append.
- **Persistence** (`internal/daemon/state`): `~/.local/share/rex/sessions/<id>/meta.json` + `transcript.log`.

TUI boot (`internal/surface/tui/splash_steps.go`): if socket unreachable, `daemonctl.Start` execs `rex-daemon` beside `rex` binary.

## Main use case: spawn and monitor

1. User presses `i` or runs `rex new` → `IntentNewSession` with tool/model from registry + settings.
2. Server acquires concurrency slot, creates `state.Session`, starts supervisor with resolved `registry.Tool` command line.
3. Adapter reads PTY output → updates `Session.State` → store broadcasts `EventSessionUpdated`.
4. Subscribed clients (TUI) render board columns by state.
5. Optional: supervisor enqueues session id on `SummaryRequest` → summarizer patches `description`.

## Main use case: attach

1. `rex attach <sel>` or TUI attach: client `SubscribeReplay`, receives transcript tail + live `SessionOutput`.
2. Server forwards stdin via `SendInput` / dedicated input channel to supervisor.
3. Detach stops forwarding; daemon keeps child running.

## Module dependency graph (internal)

```
cmd/rex → cli → {client, tui, daemonctl, settings, …}
cmd/rex-daemon → {server, state, registry, summarizer, lua, settings, rexlog}

cli → client, protocol, tui, daemonctl
tui → client, protocol, audio, settings, registry, daemonctl

server → {protocol, state, registry, pty, adapter, ids}
pty → {adapter, state, protocol}
adapter → {protocol, registry}
client → protocol
summarizer → {state, protocol}
state → protocol
daemonctl → (stdlib)
```

## Concurrency

- **Daemon:** `Serve` accept loop; per-client handler goroutine; per-session supervisor goroutine; summarizer single worker goroutine; store broadcasts to subscriber callbacks (TUI event pump).
- **TUI:** Bubble Tea model on main thread; daemon events via `DaemonEventMsg` channel.
- **Session cap:** `server.TryAcquireSession` / `max_concurrent_sessions` flag and setting.

## Reload

`SIGHUP` on daemon reloads `tools.yaml` into `server.SetRegistry` without restarting PTYs. `rex reload` sends SIGHUP via CLI.

## External systems

| System | Package | Role |
|--------|---------|------|
| Agent CLIs | `registry` + `pty` | Child processes |
| Ollama | `summarizer` | Local HTTP descriptions |
| oto | `audio` | UI sounds |
| Lua (optional) | `lua` | Event hooks in daemon |

## Human docs

Human design notes: [../architecture.md](../architecture.md), [../protocol.md](../protocol.md). When they disagree with code, code wins — see `AGENTS.md`.
