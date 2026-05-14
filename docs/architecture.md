# Architecture

Two processes. One Unix domain socket. JSON-lines on the wire. State machine in the daemon, render loop in the TUI.

## Process model

```
┌────────────────────┐   UDS, JSONL    ┌─────────────────────────────┐
│  rex (TUI client)  │ ◀───────────▶  │  rex-daemon                 │
│  Bubble Tea        │                  │  PTY supervisor             │
│  Mouse + keyboard  │                  │  State store                │
│  Audio playback    │                  │  Adapter dispatch           │
└────────────────────┘                  └──────────────┬──────────────┘
                                                        │ creack/pty
                                                        ▼
                                          ┌──────────────────────────────┐
                                          │ child agent processes        │
                                          │ claude · codex · gemini · …  │
                                          └──────────────────────────────┘
```

### `rex` (client)

- Stateless renderer over a snapshot + diff stream from the daemon.
- Plays audio on the local TTY (sounds belong to the user's session, not the daemon).
- Translates mouse and keyboard input into a small set of *intents* dispatched over the socket.
- Auto-starts `rex-daemon` if the socket isn't healthy: a bounded fork-and-wait that gives up after 2 s and prints a diagnostic.

### `rex-daemon`

- Long-running supervisor. Holds the live `Session` set and the merged registry in memory.
- For each session, runs an `Adapter` goroutine against the child PTY.
- Persists session metadata and rolling transcript to `~/.local/share/rex/sessions/<id>/`.
- Broadcasts state diffs to any connected clients (last-write-wins; v0 assumes single user).

## IPC

- **Transport.** Unix domain socket at `$XDG_RUNTIME_DIR/rex.sock` (fallback `~/.cache/rex/rex.sock`).
- **Encoding.** Newline-delimited JSON.
- **Debuggable.** `socat - UNIX-CONNECT:$XDG_RUNTIME_DIR/rex.sock | jq` works out of the box.
- **Vocabulary.** See [protocol.md](protocol.md).

## Persistence layout

```
~/.local/share/rex/
├── sessions/
│   └── 7d4f-3c8a/
│       ├── meta.json
│       └── transcript.log
└── audio/                  # optional user overrides
    ├── create.wav
    ├── done.wav
    └── delete.wav

~/.local/state/rex/
├── daemon.log              # rex-daemon log when auto-started
└── tui-state.json          # saved by `:bg` / `:detach`; consumed and removed on next `rex`

~/.config/rex/
├── config.yaml             # global preferences (audio, animation, theme) — YAML layer
├── init.lua                # optional Lua config; overrides config.yaml when present
└── tools.yaml              # registry overrides / additions

$XDG_RUNTIME_DIR/rex.sock
```

`tui-state.json` schema:

```json
{
  "selection": "9e51",
  "filter": "all",
  "scroll_offset": 0,
  "saved_at": "2026-05-14T19:34:42Z"
}
```

- `meta.json` is flushed on every state transition.
- Transcripts are append-only; rotation at 16 MB (`transcript.log` → `transcript.log.1`).
- On daemon start, prior sessions reload as `crashed` (their live PTYs are gone). Resumption via the agent's own `--resume`-style flag is registry-driven; v0 only ships a manual "relaunch with the same prompt" affordance.

## Adapter interface

```go
type Adapter interface {
    Spawn(ctx context.Context, p SpawnParams) (*Session, error)
    Detect(s *Session, chunk []byte, idle time.Duration) State
    Close(s *Session) error
}
```

Built-in implementations:

| Adapter | Triggered when | Behavior |
| --- | --- | --- |
| `ClaudeStructured` | `detect.kind == structured` and `format == claude_jsonl` | Spawns the tool with structured output, parses the event stream, derives state from message kinds (`tool_use`, `awaiting_input`, `end_of_turn`). |
| `HeuristicCLI` | `detect.kind == heuristic` | Watches PTY output, applies the registry-supplied `prompt_regex` and `idle_ms` to classify state. |

Adding a new detection scheme is a Go change: implement `Adapter`, register it under a unique `kind` name in `internal/registry/adapters.go`, then reference it from YAML. Adding a new tool or model that uses an existing scheme is YAML only.

## Concurrency and limits

- Each PTY runs in its own goroutine, supervised by an `errgroup` per session.
- A global `semaphore.Weighted` caps live PTYs (`max_concurrent_sessions`, default 16). Over the cap, `NewSession` returns `ErrTooManySessions`.
- Every blocking call takes a `context.Context`; daemon shutdown cancels the root context, then waits up to 5 s for graceful close.
- No unbounded queues. Output flows through a per-session bounded ring buffer (last ~4 KB kept in memory, the rest streams straight to `transcript.log`).

## Error handling

- `fmt.Errorf("...: %w", err)` for wrapping. No `pkg/errors`.
- Adapter `Spawn` errors surface to the wizard's confirm step.
- Transient PTY errors transition the session to `failed` and write the error string to `meta.json`.

## Failure model

| Failure | Behavior |
| --- | --- |
| Daemon crash | Live PTYs die. Sessions reload as `crashed` on next start. Transcripts remain readable. |
| Client crash | Daemon and all sessions continue. Next `rex` invocation re-subscribes from snapshot. |
| Adapter `Spawn` failure | Wizard step 5 surfaces the error; no row is created. |
| PTY EOF before "done" detected | Session transitions to `failed` (non-zero exit) or `done` (zero exit) according to wait status. |
| YAML parse error in registry | `rex-daemon` refuses to start; prints actionable line/column. Built-in registry alone is **not** sufficient if the user file is broken — fail loud. |
| Socket exists but daemon is dead | Client unlinks the stale socket and auto-starts a fresh daemon. |
