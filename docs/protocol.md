# IPC protocol

JSON-lines over a Unix domain socket. Each line is one message. The schema is intentionally small.

## Envelope

```json
{ "v": 1, "kind": "Intent" | "Event", "type": "<message-type>", "id": "<optional-correlation-id>", "data": { ... } }
```

- `v` — protocol version. v0 = 1.
- `kind` — `Intent` (client → daemon) or `Event` (daemon → client).
- `type` — discriminator for `data`.
- `id` — optional client-chosen correlation id, echoed in any direct response Event.

## Intents (client → daemon)

| Type | `data` fields | Notes |
| --- | --- | --- |
| `Hello` | `{ "client_version": "..." }` | First message after connect. Triggers `Snapshot`. |
| `Subscribe` | `{ "session_id": "..." \| null }` | `null` = board-wide updates only. A session id additionally streams `SessionOutput`. |
| `NewSession` | `{ "tool_id", "model_id", "effort"?, "slug", "title"?, "cwd", "initial_prompt"? }` | All fields except `tool_id`, `model_id`, `slug`, `cwd` are optional. |
| `OpenSession` | `{ "session_id" }` | Marks the session as the client's foregrounded one. Implicit when `Subscribe` is called with an id. |
| `SendInput` | `{ "session_id", "bytes": "<base64>" }` | Raw PTY input. Used by the modal. |
| `Reply` | `{ "session_id", "text" }` | Convenience for inline replies; daemon appends a trailing newline. |
| `Rename` | `{ "session_id", "slug"?, "title"? }` | |
| `Delete` | `{ "session_id" }` | Terminates the PTY and removes the session directory unless `soft_trash` is enabled in `config.yaml` (when enabled, the session dir is moved to `~/.local/share/rex/trash/<id>/` instead of unlinked). |
| `FocusFilter` | `{ "tool_id": "..." \| "all" }` | Persisted server-side so reconnects restore the filter; cosmetic only. |
| `Shutdown` | `{}` | Daemon stops accepting new sessions, flushes, and exits. |

## Events (daemon → client)

| Type | `data` fields | Notes |
| --- | --- | --- |
| `Snapshot` | `{ "sessions": [SessionSummary, ...], "filter": "..." }` | Full state. Sent in response to `Hello`. |
| `SessionAdded` | `SessionSummary` | |
| `SessionUpdated` | `{ "session_id", "patch": { ... } }` | Sparse JSON-merge-style patch over the existing summary. |
| `SessionRemoved` | `{ "session_id" }` | |
| `SessionOutput` | `{ "session_id", "bytes": "<base64>" }` | Incremental PTY output for a subscribed session. |
| `Error` | `{ "id", "code", "message" }` | Correlated to an `Intent` via `id`. |

## `SessionSummary`

```json
{
  "id": "7d4f3c8a-…",
  "short_id": "7d4f",
  "tool_id": "claude",
  "model_id": "opus",
  "effort": "high",
  "slug": "payment-migration",
  "title": "porting billing to new processor",
  "cwd": "/Users/tristan/code/acme",
  "state": "working",
  "started_at": "2026-05-14T19:32:11Z",
  "last_event_at": "2026-05-14T19:34:42Z",
  "last_line": "porting billing to the new processor — 12/14",
  "exit_code": null
}
```

`short_id` is the first four hex characters of `id`. It's rendered on the board as bare hex (e.g. `7d4f`) and accepted as a session selector by the CLI (`rex attach 7d4f`, `rex rm 7d4f`). The daemon guarantees `short_id` uniqueness across the active session set by extending the slice (5, 6, ... chars) on the rare collision.

`state` ∈ `working | needs_input | done | failed | crashed`.

## Versioning

`v` is bumped only on breaking changes. Additive fields are tolerated; unknown fields are ignored on read.

## Debugging recipes

```sh
# Watch raw traffic
socat - UNIX-CONNECT:$XDG_RUNTIME_DIR/rex.sock

# Pretty-print events
socat - UNIX-CONNECT:$XDG_RUNTIME_DIR/rex.sock | jq -c '.'

# Hand-craft an intent
printf '{"v":1,"kind":"Intent","type":"Hello","data":{"client_version":"manual"}}\n' \
  | socat - UNIX-CONNECT:$XDG_RUNTIME_DIR/rex.sock
```
