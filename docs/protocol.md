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

Reader rejects lines where `v` ≠ `1`. Maximum line buffer: 64 KiB.

## Handshake

1. Client connects to the socket.
2. Client sends `Hello` intent.
3. Daemon responds with `Snapshot` event (full session list).
4. Daemon may send live events (`SessionAdded`, `SessionUpdated`, …).
5. Further intents on the same connection.

Store broadcasts are gated until `Hello` completes on that connection.

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
| `Subscribe` | yes | `session_id`, `replay`, `mode_sync` |
| `SendInput` | yes | `session_id`, `bytes` (base64), `synthetic` (client-generated, e.g. the ^L redraw nudge; does not cancel a retire) |
| `Reply` | yes | `session_id`, `text` (daemon writes text, then `\r` as Enter ~120ms later) |
| `Rename` | yes | `session_id`, `slug`, `title` (empty = unchanged; at least one required). Confirmed by `SessionUpdated`; `bad_intent` echoing the request id if the slug is malformed or held by another session |
| `Delete` | yes | `session_id` |
| `Resize` | yes | `session_id`, `cols`, `rows` (0 ignored) |
| `SetSessionFleet` | yes | `session_id`, `fleet` |
| `SetMaxConcurrent` | yes | `n` (`n <= 0` uncaps) |
| `Complete` | yes | `session_id` |
| `SetSessionName` | yes | `session_id`, `name` (always sent; `""` clears; normalized, max 64 runes) |
| `Retire` | yes | `session_id`, `ttl_seconds` (0 = daemon default; 10 s – 365 d). Marks done; no-op if already marked |
| `Unretire` | yes | `session_id`. `bad_state` once the reaper has claimed the session |
| `FocusFilter` | no-op | Accepted; not stored |
| `OpenSession` | **no** | `bad_intent` "intent not implemented" |
| `Shutdown` | **no** | `bad_intent` "intent not implemented" |

### NewSession fields

| Field | Required |
|-------|----------|
| `tool_id`, `model_id`, `slug`, `cwd` | yes |
| `effort`, `title`, `initial_prompt`, `fleet` | no |

`slug` must be free of whitespace and control characters and unique among all sessions in the daemon (any state). The daemon does not rename on collision: it answers `bad_intent` `slug "x" already in use by <short_id>`, so clients that derive slugs disambiguate them first (`-2`, `-3`, …).

## Events (daemon → client)

| Type | Payload summary |
|------|-----------------|
| `Snapshot` | `sessions[]`, `filter` (cosmetic; often `"all"`) |
| `SessionAdded` | `SessionSummary` |
| `SessionUpdated` | `session_id`, `patch` (sparse map) |
| `SessionRemoved` | `session_id`, `reason` (`user` \| `retired`; empty from older daemons) |
| `SessionOutput` | `session_id`, `bytes` (base64) |
| `SummarizerHealth` | `available`, `reason` |
| `Error` | `id`, `code`, `message` |

### SessionSummary fields

`id`, `short_id`, `tool_id`, `model_id`, `effort`, `slug`, `title`, `cwd`, `state`, `started_at`, `last_event_at`, `last_line`, `description`, `exit_code`, `tokens`, `output_bytes`, `fleet`, `name`, `retire_at` (RFC 3339; omitted when not marked), `working_since` (start of the current working stretch; omitted unless working).

`last_event_at` is the last real activity — a state change, a new screen line, or the child's exit; stats and digest treat it as a finished session's end. Metadata edits (`rename`, fleet, `name`, retire, AI `description`) leave it unchanged and their patches omit it.

Patches for `name` always carry the key (`""` = cleared). Patches for `retire_at` and `working_since` send `null` to clear.

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
| `bad_intent` | Wrong kind, parse error, unimplemented intent, invalid/missing fields (incl. `NewSession`), slug already in use (`NewSession`, `Rename`) |
| `unknown_session` | No such session (or input buffer full) |
| `spawn_failed` | `NewSession` failed after validation (unknown tool/model, effort, PTY start) |
| `too_many_sessions` | Concurrency cap |
| `ambiguous_id` | Defined; not emitted by current server |
| `registry_invalid` | Defined; not emitted by current server |
| `bad_state` | `Unretire` after the reaper started clearing the session |

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

`FocusFilter` is documented in protocol comments as per-client filter state; the server accepts and ignores it. Board filtering is client-side (TUI).
