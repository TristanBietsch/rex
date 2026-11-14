# Schemas

Wire and on-disk shapes defined in `internal/wire/protocol` and `internal/daemon/state`. Source of truth is Go struct tags.

## Envelope (every UDS line)

| Field | Type | Notes |
|-------|------|--------|
| `v` | int | Must equal `protocol.ProtocolVersion` (1) |
| `kind` | string | `Intent` or `Event` |
| `type` | string | Intent or event name constant |
| `id` | string | Optional correlation id |
| `data` | object | Payload; shape depends on `type` |

Codec: `internal/wire/protocol/codec.go`.

## SessionSummary (wire + meta.json)

| Field | JSON | Type | Notes |
|-------|------|------|--------|
| ID | `id` | string | UUID |
| ShortID | `short_id` | string | 4+ hex chars |
| ToolID | `tool_id` | string | registry id |
| ModelID | `model_id` | string | |
| Effort | `effort` | string | optional |
| Slug | `slug` | string | |
| Title | `title` | string | optional; archived sessions use title prefix (CLI) |
| CWD | `cwd` | string | |
| State | `state` | string | `queued`, `working`, `needs_input`, `done`, `failed`, `crashed` |
| StartedAt | `started_at` | RFC3339 time | |
| LastEventAt | `last_event_at` | RFC3339 time | last real activity (creation, state change, screen line, exit); not bumped by rename/fleet/name/retire/description |
| LastLine | `last_line` | string | optional |
| Description | `description` | string | optional; from summarizer |
| ExitCode | `exit_code` | int | optional pointer |
| Tokens | `tokens` | int64 | optional; heuristic |
| RetireAt | `retire_at` | RFC3339 time | optional; when a marked-done session gets cleared |
| RetiredAt | `retired_at` | RFC3339 time | optional; when the session was marked done (absent on legacy marks) |
| EndedAt | `ended_at` | RFC3339 time | optional; first time the session became done, failed or crashed; cleared when it goes live again; backfilled from `last_event_at` when an older meta.json is loaded |
| DescriptionAt | `description_at` | RFC3339 time | optional; when `description` was last written by the summarizer; absent until the first summary |
| OutputBytes | `output_bytes` | int64 | optional | |
| Fleet | `fleet` | string | optional |
| Subagents | `subagents` | int | optional; running subagents (Claude hooks); cleared before the final meta.json write and not restored from it |
| WaitingOn | `waiting_on` | string | optional; `shell`, `subagent` or `web`: what a working session is blocked on (Claude hooks `wait+` / `wait-`); cleared when the state leaves `working`, so a final meta.json has none; not restored from meta.json. Patch `null` clears |
| WaitingSince | `waiting_since` | RFC 3339 | optional; when that wait began; set and cleared with `waiting_on` |
| ResumeID | `resume_id` | string | optional; the agent's own conversation id (canonical UUID, lowercase), used to resume it; validated on every ingest and when loading meta.json |
| WaitReason | `wait_reason` | string | optional; `review` (the agent finished a turn) or `input` (it asks for an answer or approval) while `needs_input`; empty when unknown; Claude hooks only; not restored from meta.json |

Defined: `internal/wire/protocol/events.go` (`SessionSummary`). Persisted: `internal/daemon/state/persist.go` writes same shape to `meta.json`.

## Snapshot (Hello response)

| Field | JSON | Type |
|-------|------|------|
| Sessions | `sessions` | `SessionSummary[]` |
| Filter | `filter` | string | cosmetic per-client |
| Overview | `overview` | `Overview` | optional; absent until the daemon publishes one |
| Usage | `usage` | `Usage` | optional; live quotas, absent while `usage` is off or before the first one |
| Loops | `loops` | `LoopSummary[]` | optional; absent while `loops` is off |

## Overview event

| Field | JSON | Type | Notes |
|-------|------|------|--------|
| Enabled | `enabled` | bool | false while `overview_enabled` is off |
| Headline | `headline` | string | optional; model output, sanitized, at most 200 runes |
| GeneratedAt | `generated_at` | RFC3339 time | optional |

In memory only; never persisted. See `docs/protocol.md`.

## Usage event

`Usage` in `internal/wire/protocol/usage.go`; memory only, never persisted.

| Field | JSON | Type | Notes |
|-------|------|------|-------|
| Enabled | `enabled` | bool | false: `usage` was switched off, drop held quotas |
| Quotas | `quotas` | `UsageQuota[]` | latest reading per tool and window |

`UsageQuota`: `tool`, `model` (empty = plan-wide), `window` (`5h`, `week`, a spend period), `used_pct`, `resets_at`, `used_usd`, `limit_usd` (all optional), `at`, `stale`.

## Loop event

`LoopSummary` in `internal/wire/protocol/loop.go`. Persisted by the daemon as `<state-dir>/loops/<id>.json` (0600, `features/loop.State`: the summary fields plus the validated definition) with the log in `<id>.log`.

| Field | JSON | Type | Notes |
|-------|------|------|-------|
| ID | `id` | string | `loop-` and 8 hex digits |
| Name | `name` | string | the loop file's name |
| Kind | `kind` | string | `repeat` |
| State | `state` | string | `running`, `paused`, `done`, `capped`, `stopped`, `failed` |
| PauseReason | `pause_reason` | string | optional; `user`, `needs_input`, `user_input`, `daemon_restart`, `session_ended`, `state_changed`, `send_failed`, `feature_off` |
| StopReason | `stop_reason` | string | optional; `check_passed`, `marker`, `max_iterations`, `max_minutes`, `stopped`, `check_error` |
| SessionID, CWD | `session_id`, `cwd` | string | |
| Iteration, MaxIterations | `iteration`, `max_iterations` | int | turn in progress, cap |
| LastCheckExit | `last_check_exit` | int | optional; absent on timeout or start failure |
| StartedAt, UpdatedAt | `started_at`, `updated_at` | RFC3339 time | |
| Notice | `notice` | string | optional; daemon-written text |

`LoopSummary` also has `max_minutes` and `elapsed_s` (additive; the loops overlay shows running time against the cap).

`SessionSummary` also has `workspace_id` (optional, 8 lowercase hex digits; set by the daemon from the session's CWD at spawn while `workspaces` is on; persisted in `meta.json`, dropped on load when malformed) and, in `Snapshot`, `workspace_tagging` (bool). `SessionSummary` also has `loop_id` and `loop_iter` (both optional; set only for a loop's session). Intents `StartLoop` (`name`, `cwd`), `PauseLoop`, `ResumeLoop`, `StopLoop` (`loop_id`) reply with this event.

## UsageSample intent (status-line tap, no reply)

`session_id`, `claude_session_id`, `five_hour` / `seven_day` (`used_pct`, `resets_at` epoch seconds), `spend_limit` (`used_pct`, `resets_at`, `used_usd`, `limit_usd`, `period`), `cost_usd`, `model_id`, `model_name`. All but `session_id` optional.

## UsageQuery intent and UsageResult

Intent: `range` (`today` | `7d` | `30d`), `tool` (optional). Reply event `UsageResult` (`UsageReport`): `range`, `since`, `tool`, `total`, `tools[]` (`tool`, row fields, `models[]` with `model` and row fields), `quotas[]`. Row: `sessions`, `tokens`, `tokens_estimated`, `cost_usd` (optional).

## SessionUpdated patch

| Field | JSON | Type |
|-------|------|------|
| SessionID | `session_id` | string |
| Patch | `patch` | map[string]any | sparse merge |

## SessionOutput

| Field | JSON | Type |
|-------|------|------|
| SessionID | `session_id` | string |
| Bytes | `bytes` | base64 on wire |
| Repaint | `repaint` | bool | optional; true only on the output answering `Subscribe.repaint` (bytes redraw the current screen) |

## Subscribe intent

| Field | JSON | Type |
|-------|------|------|
| SessionID | `session_id` | string | `""` = board-wide only |
| Replay | `replay` | bool | optional; transcript tail (≤ 256 KiB) first |
| ModeSync | `mode_sync` | bool | optional; without replay or repaint, the child's terminal modes first |
| Repaint | `repaint` | bool | optional; after any replay, one `SessionOutput` with `repaint: true` redrawing the current screen (running sessions only) |

## NewSession intent

| Field | JSON | Required |
|-------|------|----------|
| tool_id | string | yes |
| model_id | string | yes |
| effort | string | no |
| slug | string | yes |
| title | string | no |
| cwd | string | yes |
| initial_prompt | string | no |
| fleet | string | no |

## Resume intent

| Field | JSON | Required |
|-------|------|----------|
| session_id | string | yes |

Running session: clears the done mark. Exited session: respawns in place (see `docs/protocol.md`, Resume).

## Undo and Redo intents

| Field | JSON | Required |
|-------|------|----------|
| session_id | string | yes |
| turns | int (Undo only; 0 = 1, max 1000) | no |
| force | bool | no |
| dry_run | bool | no |

Reply event `UndoResult`: `session_id`, `restored` []string, `deleted` []string (relative slash paths, never null), `safety` string (tree id; empty on `dry_run`), `warnings` []string (omitted when empty). See `docs/protocol.md`, Undo and Redo.

## Turn snapshot ledger

`<state-dir>/snapshots/<key>/turns.jsonl`, one JSON object per line, append-only except retention rewriting it. `<key>` is the first 16 hex of sha256 of the folder's absolute path (lowercased on Windows).

| Field | JSON | Notes |
|-------|------|-------|
| SessionID | `session_id` | |
| Turn | `turn` | 1-based turn number; for `safety` the turn count applied before the undo; for `undo` the count after it |
| Phase | `phase` | capture: `start`, `end`, `safety`, `pre-redo`; operation: `undo`, `redo`, `removed` |
| Tree | `tree` | git tree id of the capture, or the tree an undo or redo restored; empty on `removed` and on a `start` whose capture failed (that turn is not undoable) |
| At | `at` | RFC 3339 UTC |
| FilesChanged | `files_changed` | paths that differ from the session's previous capture |

A `start` line numbers its turn one past the current count, which replaces any turns that were undone (redo history is dropped). Refs `refs/rex/<session>/<n>` in `<key>/git` keep each captured tree alive.

## On-disk layout

```
<state-dir>/
  sessions/
    <session-id>/
      meta.json      # SessionSummary JSON
      transcript.log # raw PTY bytes (append-only)
  snapshots/         # only with turn_snapshots on
    <key>/
      git/           # shadow git dir
      turns.jsonl    # ledger, see above
      cwd            # the folder this store belongs to
  usage.jsonl        # usage ledger (0600, append-only; see below)
```

Default `<state-dir>`: `~/.local/share/rex` (`rex-daemon -state-dir`).

## usage.jsonl (usage ledger)

One JSON object per line, `usage.Record` in `internal/features/usage/ledger.go`. A session has several lines; the one with the greatest `at` (the later line on a tie) is its current total, so periodic lines never double count.

| Field | JSON | Type | Notes |
|-------|------|------|-------|
| version | `v` | int | `1` |
| session id | `session_id` | string | rex session id (a resumed session keeps it) |
| tool | `tool` | string | tool id |
| model | `model` | string | model id; the model Claude reported when known, else the spawn model |
| tokens | `tokens` | int | rex's own estimate (`output_bytes / 4`) |
| cost | `cost_usd` | number | omitted when no source reported a cost |
| estimated | `estimated` | bool | `tokens` is an estimate |
| started | `started_at` | time | omitted when unknown |
| at | `at` | time | when the line was written; an ended session is dated by when it ended |
| ended | `ended` | bool | the session's final line |

Written when a session ends (or is removed by prune, retire or delete) and, for a live session, at most every 10 minutes and only when its totals changed. Trimmed to 90 days when the daemon starts. A line that is not valid JSON or lacks `session_id` or `at` is skipped with one warning. Never created while the `usage` setting is off.

## tools.yaml

Root: `tools:` array of `registry.Tool` (see `internal/catalog/registry/types.go`). Merged with embedded `internal/catalog/registry/builtin.yaml`. Not committed at repo root.

## config.yaml

Flat top-level keys matching `settings.Registry` IDs. Unknown keys ignored on load. Path: `settings.DefaultPath()` → `~/.config/rex/config.yaml`. Human reference: [../../settings.md](../../settings.md).

## tui-state.json

Written by `internal/surface/tui/persist.go` at `~/.local/state/rex/tui-state.json` — `selection`, `filter`, `scroll_offset`, `saved_at` and `hidden_sections` (a list of section ids; `[]` means none hidden, a missing key leaves `board_hidden_sections` alone). Read once on next launch then deleted. See `persistedState` in `persist.go`.

## Registry Tool (YAML)

See `modules/registry.md` for `Tool`, `Model`, `Detect`, `Effort` fields.
