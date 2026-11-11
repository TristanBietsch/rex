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
| `id` | string | no | Correlation id. An intent with an `id` gets exactly one reply carrying it (see [Replies](#replies)) |
| `data` | object | yes | Payload for `type` |

Reader rejects lines where `v` ≠ `1`. A line may be at most 16 MiB, newline included (`MaxLineBytes`).

## Handshake

1. Client connects to the socket.
2. Client sends `Hello` intent with its release (`client_version`) and program name (`client`).
3. Daemon responds with `Snapshot` event (full session list and `daemon_version`).
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
| `Hello` | yes | `client_version` (release label; older clients sent their name here), `client` (program name, optional) |
| `NewSession` | yes | `tool_id`, `model_id`, `effort`, `slug`, `title`, `cwd`, `initial_prompt`, `fleet`, `trust_folder`, `name`, `skills` |
| `Subscribe` | yes | `session_id`, `replay`, `mode_sync`, `repaint` |
| `SendInput` | yes | `session_id`, `bytes` (base64), `synthetic` (client-generated, e.g. the ^L redraw nudge; does not cancel a retire) |
| `Reply` | yes | `session_id`, `text` (daemon writes text, then `\r` as Enter ~120ms later) |
| `Rename` | yes | `session_id`, `slug`, `title` (empty = unchanged; at least one required). Broadcast as `SessionUpdated`; `bad_intent` echoing the request id if the slug is malformed or held by another session |
| `Delete` | yes | `session_id` |
| `Resize` | yes | `session_id`, `cols`, `rows` (0 ignored) |
| `SetSessionFleet` | yes | `session_id`, `fleet` |
| `SetMaxConcurrent` | yes | `n` (`n <= 0` uncaps) |
| `Complete` | yes | `session_id` |
| `SetSessionName` | yes | `session_id`, `name` (always sent; `""` clears; normalized, max 64 runes) |
| `Retire` | yes | `session_id`, `ttl_seconds` (0 = daemon default; 10 s – 365 d). Marks done; no-op if already marked |
| `Unretire` | yes | `session_id`. `bad_state` once the reaper has claimed the session |
| `Resume` | yes | `session_id`. Makes the session live and not marked done; see [Resume](#resume) |
| `Undo` | yes | `session_id`, `turns` (0 = 1; at most 1000), `force`, `dry_run`. Restores the folder from the turn snapshots; see [Undo and Redo](#undo-and-redo) |
| `Redo` | yes | `session_id`, `force`, `dry_run`. Brings back what the last `Undo` replaced |
| `FocusFilter` | no-op | Accepted (and acked); not stored |
| `OpenSession` | **no** | `bad_intent` "intent not implemented" |
| `Shutdown` | yes | No fields. Stops the daemon, same as SIGTERM. Its `Ack` is best effort (see [Replies](#replies)) |
| `UsageSample` | no | Sent by `rex-daemon statusline-tap`, fire and forget (no `id`, no reply, no `Hello`). `session_id` (the rex session), optional `claude_session_id`, `five_hour` and `seven_day` (`used_pct`, `resets_at` in Unix epoch seconds), `spend_limit` (the same plus `used_usd`, `limit_usd`, `period`), `cost_usd`, `model_id`, `model_name`. Every field may be absent. A sample for an unknown session, one with nothing in it, or one that arrives while `usage` is off is dropped without an error. Nothing else of Claude's status-line JSON is ever sent |
| `UsageQuery` | yes | `range` (`today`, `7d` or `30d`; empty is `today`) and optional `tool`. Reply is a `UsageResult` event with the intent's `id` (see below); `bad_intent` `usage is off` while the setting is off, and for a bad range. `(*client.Client).UsageQuery(ctx, q)` |
| `Reload` | yes | No fields. Re-reads `tools.yaml`, same as SIGHUP. `Ack` once the new registry is in use; `bad_intent` with the load error on failure |

### NewSession fields

| Field | Required |
|-------|----------|
| `tool_id`, `model_id`, `slug`, `cwd` | yes |
| `effort`, `title`, `initial_prompt`, `fleet`, `trust_folder`, `name`, `skills` | no |

`trust_folder` (bool, default false) trusts `cwd` for this session as if it were listed in `auto_trust_dirs`: the agent's folder-trust dialog is answered (or avoided with `trust_args`) and `skip_permissions` may apply. `cwd` must be absolute, without `..`, and neither a filesystem root nor the home folder; otherwise the intent is `bad_intent` with a `trust_folder: ...` message. The choice is stored in `meta.json` and shown as `trust_folder` in session summaries, and `Resume` applies it again (re-checking the stored `cwd`; a folder that no longer passes is not trusted). Nothing is written to the agent's own config by rex.

`name` is the display name, normalized like `SetSessionName` (whitespace collapsed, control characters dropped, at most 64 characters; too long is `bad_intent`). Empty shows the slug.

`skills` is a list of skill names (at most 16, unique, each `[a-z0-9][a-z0-9-]{0,63}`) from the daemon's skill library (`-skills-dir`, default `~/.config/rex/skills`). The daemon copies them into `sessions/<id>/plugin` (regular files only, at most 256 files and 4 MiB; a symlink is an error) and adds the tool's `skill_args` with `{plugin_dir}` after the resume args and before the hook args. It never takes a path from a client. The intent is `bad_intent` when the `spawn_skills` setting is off (`skills are off`), the tool has no `skill_args` (`tool X cannot load skills per session`), a name is invalid or missing from the library, or a limit is exceeded; nothing is created then. The names are stored in `meta.json`, shown as `skills` in session summaries, and `Resume` loads the same plugin folder again (staging it from the library if it is gone). A daemon that predates these fields ignores them silently, so a client that sent `skills` should check the `SessionAdded` summary for them.

`cwd` must be an absolute path (`bad_intent` otherwise); the daemon never resolves it against its own working directory.

`slug` must be free of whitespace and control characters and unique among all sessions in the daemon (any state). The daemon does not rename on collision: it answers `bad_intent` `slug "x" already in use by <short_id>`, so clients that derive slugs disambiguate them first (`-2`, `-3`, …).

### Resume

`Resume{session_id}` means "make this session live and not marked done".

- **Running session** (including one that is starting): clears a done mark (`retire_at` and `retired_at` patch `null`, as `Unretire` does) and acks. Nothing is spawned. An unmarked running session just acks.
- **Exited session** (done, failed or crashed): respawned in place with the tool's `resume.args` and the stored `resume_id`. The row keeps its id, slug, start time, tokens, description and transcript, and the transcript gains the new run. The daemon:
  1. checks the tool and model are still in the registry (`bad_intent` `tool <tool>/<model> not in registry`, the same error `NewSession` gives), the stored effort is still offered and the tool has a `resume` block and the session has a valid `resume_id` (`bad_state` `session exited and <tool> cannot be resumed`), and the working directory is still a directory (`spawn_failed`);
  2. takes the session's slot atomically, so concurrent `Resume`s start one process and the others ack as for a running session, then a concurrency slot (`too_many_sessions`);
  3. moves the session to `queued` and broadcasts one patch, `{state: "queued", last_event_at, ended_at: null, exit_code: null, retire_at: null, retired_at: null}`, after writing `meta.json`;
  4. truncates the session's hook file, recomputes folder trust from the current config and the session's `trust_folder`, and starts the agent without an initial prompt. The usual state patches follow (`working`, ...), and `ended_at` is set again when the run ends.
- `bad_state` `session is already being cleared` when the retire sweep has claimed the session (also for a running one).
- A `Delete` that lands while a resume is starting wins: the session is removed, no process is started and no directory is left behind. The `Resume` fails with `unknown_session`.
- If the agent cannot start, the session ends `failed` and the `Resume` fails with `spawn_failed`. An agent that starts but cannot find its conversation (never saved) fails after the ack, like any exit.

While the `resume` feature is off (Settings → Features) the daemon answers every `Resume` with `bad_intent` `resume is off`, and assigns or captures no resume ids.

Clients: `(*client.Client).Resume(sessionID, reqID)`. An older daemon answers `bad_intent` "intent not implemented".

### Undo and Redo

`Undo{session_id, turns, force, dry_run}` restores the session's folder to how it was before its last `turns` agent turns, from the turn snapshots (the `turn_snapshots` setting; see [snapshots](snapshots.md)). `Redo{session_id, force, dry_run}` restores the capture `Undo` took first. Both are additive: no `ProtocolVersion` bump, and an older daemon answers `bad_intent` "intent not implemented".

The reply is an `UndoResult` event (not an `Ack`): `session_id`, `restored` and `deleted` (sorted slash paths relative to the session's folder, never null), `safety`, the tree id of the capture taken just before the restore, and `warnings` (absent when empty): the folder changed while the restore ran, or the session started working during it. The folder is already changed when the reply is sent. With `dry_run` nothing changes and nothing is captured: the same checks run and the reply lists what would be restored and deleted, with an empty `safety`.

Checks, in order:

1. The feature is on, else `bad_intent` "off: enable turn_snapshots in Settings → Features". `turns` outside 0 to 1000 is `bad_intent`.
2. The session exists (`unknown_session`).
3. It is not `working` (`bad_state`).
4. No other live session (state not `done`, `failed` or `crashed`) has a `cwd` that is the same folder, inside it or around it (paths cleaned and symlink-resolved), unless `force` (`bad_state`, naming the others' slugs). Checks 3 and 4 run again after the safety capture, just before the first file changes; `force` never overrides check 3.
5. The folder is inside a git worktree (git 2.32 or newer), and there is a turn to undo (or an undo to redo): else `bad_state` with the reason ("snapshots need a git repo", "nothing to undo", "nothing to redo", "that turn was not captured, so it cannot be undone").
6. Nothing a restore would overwrite or remove is missing from the snapshots (a nested repo or clone, a file over 2 MiB, an ignored folder or a symlink at a path it must write): else `bad_state` naming the blocking paths, with nothing changed. `force` does not override it.

The restore runs on a context detached from the connection, so a client that disconnects does not stop it half way. After the final check the restore and its verification run without a deadline. If it fails part way, the `bad_state` message says `rex redo` brings back the state from before. A session whose folder ledger was found corrupt and reset gets a `bad_state` "history for this folder was reset at <time> (the old ledger is kept as <file>); older turns can't be undone".

Snapshots are taken by the daemon at turn boundaries and are not an intent. Redo history is dropped when a session starts another turn. Clients: `(*client.Client).Do(ctx, protocol.IntentUndo, protocol.Undo{...})`.

## Replies

An intent whose envelope carries a non-empty `id` gets exactly one terminal reply, an event with the same `id`:

| Intent | Reply on success | Notes |
|--------|------------------|-------|
| `Hello` | `Snapshot` | Sent even without an `id` |
| `NewSession` | `Ack` with the new `session_id` | The `SessionAdded` broadcast arrives first |
| `Subscribe` | `Ack` (`session_id` when given) | After any replay, repaint or mode-sync `SessionOutput` |
| `SendInput` | `Ack` | The bytes are queued to the PTY |
| `Reply` | `Ack` | The text and the Enter are queued |
| `Rename`, `SetSessionName` | `Ack` | The `SessionUpdated` broadcast arrives first |
| `SetSessionFleet` | `Ack` | |
| `Retire`, `Unretire` | `Ack` | Also when nothing changed |
| `Resume` | `Ack` | The `queued` patch arrives first when an exited session respawns; also when it was already running |
| `Delete` | `Ack` | The `SessionRemoved` broadcast arrives first |
| `Complete` | `Ack` | Termination requested, not finished |
| `Resize` | `Ack` | Also for the ignored 0×0 size |
| `FocusFilter`, `SetMaxConcurrent` | `Ack` | |
| `Undo`, `Redo` | `UndoResult` | The folder is changed before the reply; with `dry_run` it is not |
| `Reload` | `Ack` | After the new registry is in use |
| `Shutdown` | `Ack` | Queued before shutdown starts; the daemon may close the connection first, so treat the close as success |
| `UsageQuery` | `UsageResult` | Not an `Ack`: the payload is the aggregates |
| `UsageSample` | `Ack` only if it carried an `id` | The tap sends none |
| `OpenSession`, unknown types | — | `Error` `bad_intent` "intent not implemented" |

- Any failure is an `Error` event with the intent's `id` instead.
- `Ack` carries the intent type it answers (`intent`) and, for intents about one session, its `session_id`.
- "Arrives first" holds on a connection that has sent `Hello`: broadcasts and replies share one ordered queue per connection.
- An intent **without** an `id` gets no `Ack`. It still gets an `Error` (with an empty `id`) on failure, and `Hello` still gets its `Snapshot`, as before acks existed.
- A protocol violation (a line over 16 MiB, an unsupported `v`, a malformed line) is answered with an `Error` with an empty `id`. After `line_too_long` or `unsupported_version` the daemon closes the connection.

## Events (daemon → client)

| Type | Payload summary |
|------|-----------------|
| `Snapshot` | `sessions[]`, `filter` (cosmetic; often `"all"`), `daemon_version` (absent from daemons that predate acks), `overview` (absent until the daemon publishes one, and from daemons that predate it), `usage` (the live quotas, below; absent while `usage` is off, before the first one and from daemons that predate it) |
| `Ack` | `intent`, `session_id` (optional). Success reply to an intent with an `id` |
| `SessionAdded` | `SessionSummary` |
| `SessionUpdated` | `session_id`, `patch` (sparse map) |
| `SessionRemoved` | `session_id`, `reason` (`user` \| `retired`; empty from older daemons) |
| `SessionOutput` | `session_id`, `bytes` (base64), `repaint` (true only on the one that answers `Subscribe` `repaint`; omitted otherwise) |
| `SummarizerHealth` | `available`, `reason` |
| `Overview` | `enabled`, `headline` (sanitized model text, at most 200 runes), `generated_at`. Sent to every connection that has sent `Hello`; the latest value is also in `Snapshot.overview` |
| `Usage` | `enabled`, `quotas[]`: the latest plan-quota reading per tool and window, `{tool, model, window, used_pct, resets_at, used_usd, limit_usd, at, stale}`. Window is `5h`, `week` or a spend period such as `month`; `model` is empty for a plan-wide quota; `resets_at` is RFC 3339 and omitted when unknown; every number may be absent (an API-key plan has no quota). Sent to every connection that has sent `Hello` when a quota changes (a reading that repeats the same numbers is not republished) and when `usage` is switched on or off (`enabled: false`: drop what you hold). A reading with `at` older than 15 minutes is stale. The latest value is also in `Snapshot.usage` |
| `UsageResult` | Reply to `UsageQuery`: `range`, `since`, `tool`, `total`, `tools[]` (each with `models[]`) and `quotas[]`. A row is `{sessions, tokens, tokens_estimated, cost_usd}`; `cost_usd` is absent when no counted session reported a cost. Rows count each session once, by its latest ledger line (or its live totals) falling inside the range |
| `Error` | `id`, `code`, `message` |

### SessionSummary fields

`id`, `short_id`, `tool_id`, `model_id`, `effort`, `slug`, `title`, `cwd`, `state`, `started_at`, `last_event_at`, `last_line`, `description`, `exit_code`, `tokens`, `output_bytes`, `fleet`, `name`, `retire_at` (RFC 3339; omitted when not marked), `retired_at` (RFC 3339; when the session was marked done; omitted when not marked and for marks persisted before the field existed), `working_since` (start of the current working stretch; omitted unless working), `ended_at` (RFC 3339; the first time the session reached done, failed or crashed; omitted while live; a daemon that predates it never sends it, so clients fall back to `last_event_at` for terminal sessions), `subagents` (subagents the agent is running now; Claude Code only; omitted when 0, and patches send `0` when the last one stops), `waiting_on` (`shell`, `subagent` or `web`: what a **working** session's main thread is blocked on, from Claude Code's tool hooks; omitted when not waiting; the state stays `working`, so a client that predates the field shows the session as working; patches send `null` to clear it, and the daemon clears it whenever the state leaves `working`; clients ignore a kind they do not know), `waiting_since` (RFC 3339; when that tool call began; omitted unless `waiting_on` is set; patched together with `waiting_on`; the TUI shows the session as waiting only after 3 seconds), `description_at` (RFC 3339; when `description` was last written by the summarizer; omitted until the first summary, and a daemon that predates it never sends it; patches for `description` carry it too), `resume_id` (the agent's own conversation id, a canonical UUID in lowercase; omitted when unknown; a patch carries it when it becomes known or changes; an older client ignores it), `trust_folder` (true when the folder was trusted for the session at creation; omitted otherwise), `skills` (names of the library skills loaded into the session; omitted when none), `wait_reason` (why a `needs_input` session waits: `review` when the agent finished a turn, `input` when it asks for an answer or approval; omitted when unknown, which is always so for tools without hooks; cleared when the session works again; unrelated to `waiting_on`, which is only for working sessions).

`last_event_at` is the last real activity — a state change, a new screen line, or the child's exit; stats and digest treat it as a finished session's end. Metadata edits (`rename`, fleet, `name`, retire, AI `description`) leave it unchanged and their patches omit it.

Patches for `name` always carry the key (`""` = cleared). Patches for `retire_at`, `retired_at`, `working_since` and `ended_at` send `null` to clear (`ended_at` clears when a session goes live again; every terminal state change carries `ended_at` and `working_since: null`; a `Resume` of an exited session also patches `exit_code: null`); a mark patches `retire_at` and `retired_at` together.

On disk the same shape is stored in `meta.json`. See [paths.md](paths.md).

### Subscribe replay

When `replay` is true, the daemon sends up to **256 KiB** of transcript tail as `SessionOutput` events before live output.

### Subscribe mode_sync

When `mode_sync` is true and `replay` is false, the daemon first sends one `SessionOutput` holding escape sequences that re-create the child's current terminal modes (alternate screen `?1049h`, mouse tracking `?9/1000/1002/1003h`, SGR mouse `?1006h`, application cursor keys `?1h`). A client emulator that never saw the child's startup bytes then agrees with it about how to deliver mouse input. With `repaint` it sends nothing: the repaint carries the modes.

### Subscribe repaint

When `repaint` is true and the session is running, the daemon sends, after the replay (if any) and before live output, one `SessionOutput` with `repaint: true`. Its bytes redraw the session's current screen from the daemon's per-session screen mirror: every visible cell with its colors and attributes, the primary screen under an active alternate screen, the terminal modes (alternate screen, autowrap, insert, mouse tracking and encoding, cursor keys, keypad, focus reporting, reverse video), the scroll region, the saved cursor, the cursor position and pen, the cursor's visibility, and the first bytes of a rune the child's last write split (the next output completes it). Each row is drawn in place (cursor position, cells, erase to end of line) with autowrap off, so the repaint never scrolls a terminal or feeds its scrollback. A full-screen agent that has nothing new to draw then still shows its last frame in a client that attaches late.

The repaint is drawn at the mirror's size, which follows the last `Resize`; send `Resize` before `Subscribe` to get it at your size. While the daemon reads the transcript tail and renders the repaint it holds the session's output, so each PTY chunk lands exactly once: in the replay and the repaint, or live.

Colors come back as the mirror stores them. Palette colors 0–255 and default colors are exact. A 24-bit color is exact unless its red and green are both 0: the mirror keeps rgb(0,0,b) as palette color b. Bold text in colors 0–7 is stored, and repainted, as bright colors 8–15.

Not reproduced: tab stops, the window title, palette changes (cells carry the colors they resolve to), a pending wrap after a wide rune in the last column, and an escape sequence the child's last write left unfinished (the client then shows the rest of it as text until the agent redraws). Bracketed paste (`?2004`) is not tracked by the mirror, so it is not re-sent. The mirror is a vt10x emulator, which gives every rune one cell; on a real terminal a wide rune pushes the rest of its row right, as in the TUI's attach popup.

Nothing is sent for a session that is not running.

### Compatibility

Additive changes (new optional fields, new intents, new events, new patch keys) keep `v: 1`; both sides ignore unknown fields and event types. An older daemon answers new intents with `bad_intent "intent not implemented"`; clients tell the user to run `rex daemon restart`.

The overview is in the daemon's memory only. An `Overview` published between a client's `Snapshot` and the start of its event stream is missed until the next publish. An older client ignores the event and `snapshot.overview`; against an older daemon a newer client sees neither and shows only what it computes itself.

Usage is the same: the quotas are in the daemon's memory only, and a `Usage` published between a client's `Snapshot` and the start of its event stream is missed until the next change. An older daemon answers `UsageQuery` with `bad_intent` "intent not implemented" (the CLI says to run `rex daemon restart`) and ignores `UsageSample`, so a tap talking to it just loses its usage; an older client ignores the `Usage` event and `snapshot.usage`. `ProtocolVersion` is unchanged.

A daemon that predates acks never sends `Ack`, and its `Snapshot` has no `daemon_version`. A client that sees no `daemon_version` must not wait for an `Ack`; the Go client's `Do` then fails at once with `ErrNoAcks`.

A daemon that predates `repaint` ignores the field and answers `mode_sync` as before. A client tells by the `Subscribe`'s reply: send it with an `id`, and if the `Ack` arrives with no `repaint: true` output before it, there was no repaint. A daemon that predates acks sends no reply at all, so wait for it only briefly. rex's attach clients (`client.RepaintWatch`: 250 ms, or 2 s after a replay) then fall back to the `^L` redraw nudge. Clients that do not set `repaint` get exactly what they got before, and no `repaint` key on the wire.

## Error codes

| Code | Emitted when |
|------|----------------|
| `bad_intent` | Wrong kind, parse error, unimplemented intent, invalid/missing fields (incl. `NewSession`), slug already in use (`NewSession`, `Rename`), tool or model not in the registry (`NewSession`, `Resume`) |
| `unknown_session` | No such session (or input buffer full); `Resume` of a session deleted while it started |
| `spawn_failed` | `NewSession` failed after validation (effort, PTY start); `Resume` with a missing working directory or an agent that did not start |
| `too_many_sessions` | Concurrency cap |
| `ambiguous_id` | Defined; not emitted by current server |
| `registry_invalid` | Defined; not emitted by current server |
| `bad_state` | `Undo` or `Redo` refused (session working, another live session in the folder without `force`, folder not in a git repo, nothing to undo or redo, a turn that was not captured); `Unretire` or `Resume` after the reaper started clearing the session; `Resume` of an exited session whose tool cannot resume (no `resume` block or no known `resume_id`, or the stored effort is no longer offered by the model) |
| `line_too_long` | A line exceeded 16 MiB; the daemon closes the connection |
| `unsupported_version` | The envelope's `v` is not `1`; the daemon closes the connection |
| `internal_error` | A daemon bug while handling the intent; the daemon closes the connection |

## EXAMPLES

Hello (client line):

```json
{"v":1,"kind":"Intent","type":"Hello","id":"h1","data":{"client_version":"v1","client":"rex-cli"}}
```

Snapshot (daemon line, abbreviated):

```json
{"v":1,"kind":"Event","type":"Snapshot","id":"h1","data":{"sessions":[],"filter":"all","daemon_version":"v1"}}
```

Rename with an id, and its Ack:

```json
{"v":1,"kind":"Intent","type":"Rename","id":"r1","data":{"session_id":"<id>","title":"new title"}}
{"v":1,"kind":"Event","type":"Ack","id":"r1","data":{"intent":"Rename","session_id":"<id>"}}
```

Debug with socat:

```sh
socat - UNIX-CONNECT:$HOME/.cache/rex/rex.sock
```

Paste one intent line; read one event line per response phase.

## Go client

`internal/wire/client` is intended for in-repo use; it is not a stable public Go API.

- `Dial`, `DialTimeout` and `DialContext` connect. A dial error names the socket once (`dial unix <path>: connect: …`).
- `Hello` / `HelloContext` perform the handshake. `Hello` is bounded by `HelloTimeout`. `DaemonVersion` reports the daemon's version afterwards.
- `Do(ctx, intentType, payload)` sends an intent with a fresh `id` and waits for its reply. An `Error` reply comes back as a `*client.Error` (`errors.As`) with `Code` and `Message`. `Do` honors `ctx` deadlines and cancellation.
- `Send(ctx, intentType, payload)` and the helper methods (`Rename`, `SendInput`, …) write an intent without waiting for a reply.
- One reader goroutine routes replies to their `Do` callers and every other event to `NextEvent` / `NextEventContext`. `Do` and the writers are safe for concurrent use. Read events from one goroutine. `Close` unblocks every pending `Do`.

## SEE ALSO

- [daemon.md](daemon.md)
- [paths.md](paths.md) — socket path
- [agents/data/schemas.md](agents/data/schemas.md) — field-level mirror for agents

## BUGS

`FocusFilter` is documented in protocol comments as per-client filter state; the server accepts and ignores it. Board filtering is client-side (TUI).
