# Wire protocol

Newline-delimited JSON over a Unix domain socket. One JSON object per line.

## NAME

rex — JSONL control protocol (version 1)

## DESCRIPTION

`rex` clients connect to `rex-daemon`, send **intents**, and receive **events**. Every line is an envelope. Binary fields encode as base64 in JSON.

## Envelope

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `v` | int | yes | Must be `1` (`ProtocolVersion`) |
| `kind` | string | yes | `Intent` or `Event` |
| `type` | string | yes | Intent or event name |
| `id` | string | no | Correlation id; echoed on errors |
| `data` | object | yes | Payload for `type` |

The daemon reads intent lines of at most 1 MiB (`MaxIntentLineBytes`). A line that is too long, is not a JSON envelope, or has `v` ≠ `1` gets an `Error` event (`bad_intent`, empty `id`, the reason as `message`) and then the daemon closes the connection. Lines from the daemon have no length limit: a `Snapshot` grows with the board.

## Handshake

1. Client connects to the socket.
2. Client sends `Hello` intent.
3. Daemon responds with `Snapshot` event (full session list).
4. Daemon may send live events (`SessionAdded`, `SessionUpdated`, …).
5. Further intents on the same connection.

Board events (store changes, `SummarizerHealth`) are not sent before `Hello`. Events that happen while the daemon takes the snapshot are held and sent right after `Snapshot`, so none are lost. A held patch may repeat what the snapshot already shows, so apply patches idempotently; a held `SessionAdded` for a session already in the snapshot is skipped.

### Slow clients

Each connection has an outbound queue of 2048 events, drained by its own writer. A client whose queue fills, or that accepts no bytes for 10 s, is disconnected (and logged), so one stalled client never blocks the daemon or other clients.

## Session states

| State | Meaning |
|-------|---------|
| `queued` | Accepted, not yet running |
| `working` | Agent active |
| `needs_input` | Waiting for user input |
| `done` | Finished successfully |
| `failed` | Exited with failure |
| `crashed` | Daemon restarted; prior live session |

## Intents (client → daemon)

| Type | Implemented | Payload summary |
|------|-------------|-----------------|
| `Hello` | yes | `client_version` |
| `NewSession` | yes | `tool_id`, `model_id`, `effort`, `slug`, `title`, `cwd`, `initial_prompt`, `fleet` |
| `Subscribe` | yes | `session_id` (`""` = board events only), `replay`, `mode_sync`. `unknown_session` for an id the daemon does not know. A repeat `Subscribe` for the same session replaces its output stream |
| `SendInput` | yes | `session_id`, `bytes` (base64), `synthetic` (client-generated, e.g. the ^L redraw nudge; does not cancel a retire). `bad_state` if the session's process has exited, `busy` if its input buffer is full |
| `Reply` | yes | `session_id`, `text`. The daemon drops control characters from `text` except newline and tab (C0 incl. ESC and CR, DEL, C1), types it, then sends `\r` as Enter ~120 ms later on a timer, so the connection keeps reading. Input from any client waits for that Enter. `bad_state` / `busy` as for `SendInput` |
| `Rename` | yes | `session_id`, `slug`, `title` (empty = unchanged; at least one required). Same slug and title rules as `NewSession`. Confirmed by `SessionUpdated`; `bad_intent` echoing the request id if the slug is malformed or held by another session |
| `Delete` | yes | `session_id` |
| `Resize` | yes | `session_id`, `cols`, `rows` (0 ignored) |
| `SetSessionFleet` | yes | `session_id`, `fleet` (flattened to one line; over 64 runes is `bad_intent`; `""` clears) |
| `SetMaxConcurrent` | yes | `n`, clamped to 1–64 |
| `Complete` | yes | `session_id`. `bad_state` if the session's process has already exited |
| `SetSessionName` | yes | `session_id`, `name` (always sent; `""` clears; normalized, max 64 runes) |
| `Retire` | yes | `session_id`, `ttl_seconds` (0 = daemon default; 10 s – 365 d). Marks done; no-op if already marked |
| `Unretire` | yes | `session_id`. `bad_state` once the reaper has claimed the session |
| `FocusFilter` | obsolete | Accepted and ignored, so older clients get no error. Filtering is client-side |
| `Shutdown` | yes | No fields. Stops the daemon, same as SIGTERM |
| `Reload` | yes | No fields. Re-reads `tools.yaml`, same as SIGHUP; `registry_invalid` with the load error on failure |

### NewSession fields

| Field | Required |
|-------|----------|
| `tool_id`, `model_id`, `slug`, `cwd` | yes |
| `effort`, `title`, `initial_prompt`, `fleet` | no |

`slug` must be at most 64 runes, valid UTF-8, free of whitespace and control characters, and unique among all sessions in the daemon (any state). `cwd` must be an existing directory. `fleet` is flattened to one line and must be at most 64 runes. Any of these failing is `bad_intent`. `title` is flattened to one line (escapes and control characters dropped) and cut to 512 runes; `initial_prompt` is not cut: `claude`, `codex`, and `gemini` get it on their command line; other tools get it pasted into the PTY once ready, with escapes and control characters other than newline and tab removed. The daemon does not rename on collision: it answers `bad_intent` `slug "x" already in use by <short_id>`, so clients that derive slugs disambiguate them first (`-2`, `-3`, …).

## Events (daemon → client)

| Type | Payload summary |
|------|-----------------|
| `Snapshot` | `sessions[]`, `filter` (always `"all"`; kept for clients that read it) |
| `SessionAdded` | `SessionSummary` |
| `SessionUpdated` | `session_id`, `patch` (sparse map) |
| `SessionRemoved` | `session_id`, `reason` (`user` \| `retired`; empty from older daemons) |
| `SessionOutput` | `session_id`, `bytes` (base64) |
| `SummarizerHealth` | `available`, `reason` |
| `Error` | `id`, `code`, `message` |

### SessionSummary fields

`id`, `short_id`, `tool_id`, `model_id`, `effort`, `slug`, `title`, `cwd`, `state`, `started_at`, `last_event_at`, `last_line`, `description`, `exit_code`, `tokens`, `output_bytes`, `fleet`, `name`, `retire_at` (RFC 3339; omitted when not marked), `retired_at` (RFC 3339; when the session was marked done; omitted when not marked and for marks persisted before the field existed), `working_since` (start of the current working stretch; omitted unless working).

`last_event_at` is the last real activity — a state change, a new screen line, or the child's exit; stats and digest treat it as a finished session's end. Metadata edits (`rename`, fleet, `name`, retire, AI `description`) leave it unchanged and their patches omit it.

Patches for `name` always carry the key (`""` = cleared). Patches for `retire_at`, `retired_at` and `working_since` send `null` to clear; a mark patches `retire_at` and `retired_at` together.

On disk the same shape is stored in `meta.json`. See [paths.md](paths.md).

### Subscribe replay

When `replay` is true, the daemon sends up to **256 KiB** of transcript tail as `SessionOutput` events before live output.

### Subscribe mode_sync

When `mode_sync` is true and `replay` is false, the daemon first sends one `SessionOutput` holding escape sequences that re-create the child's current terminal modes (alternate screen `?1049h`, mouse tracking `?9/1000/1002/1003h`, SGR mouse `?1006h`, application cursor keys `?1h`). A client emulator that never saw the child's startup bytes then agrees with it about how to deliver mouse input.

### Compatibility

Additive changes (new optional fields, new intents, new patch keys) keep `v: 1`; both sides ignore unknown fields. An older daemon answers new intents with `bad_intent "intent not implemented"`; clients tell the user to run `rex daemon restart`.

## Error codes

| Code | Emitted when |
|------|----------------|
| `bad_intent` | Wrong kind, parse error, unimplemented intent, invalid/missing fields (incl. `NewSession` slug, `cwd`, `fleet`), slug already in use (`NewSession`, `Rename`); also an unreadable line (too long, bad JSON, wrong `v`) just before the daemon closes the connection |
| `unknown_session` | No such session (including `Subscribe` to an unknown id) |
| `busy` | Transient: the session's input buffer is full (`SendInput`, `Reply`); retry |
| `spawn_failed` | `NewSession` failed after validation (unknown tool/model, effort, PTY start) |
| `too_many_sessions` | Concurrency cap |
| `registry_invalid` | `Reload` could not load `tools.yaml` |
| `bad_state` | The session exists but can't take the intent now: `SendInput`, `Reply`, or `Complete` after its process exited; `Unretire` after the reaper started clearing it |

## EXAMPLES

Hello (client line):

```json
{"v":1,"kind":"Intent","type":"Hello","data":{"client_version":"rex-cli"}}
```

Snapshot (daemon line, abbreviated):

```json
{"v":1,"kind":"Event","type":"Snapshot","data":{"sessions":[],"filter":"all"}}
```

Debug with socat:

```sh
socat - UNIX-CONNECT:$HOME/.cache/rex/rex.sock
```

Paste one intent line; read one event line per response phase.

## Go client

`internal/wire/client` provides `Dial`, `Hello`, intent helpers, and `NextEvent`. Intended for in-repo use; not a stable public Go API.

## SEE ALSO

- [daemon.md](daemon.md)
- [paths.md](paths.md) — socket path
- [agents/data/schemas.md](agents/data/schemas.md) — field-level mirror for agents

## BUGS

None known. `OpenSession` was never implemented and is gone from the protocol; a daemon still answers it, like any unknown intent, with `bad_intent "intent not implemented"`.
