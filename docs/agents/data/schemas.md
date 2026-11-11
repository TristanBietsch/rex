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
| LastEventAt | `last_event_at` | RFC3339 time | last real activity (state change, screen line, exit); not bumped by rename/fleet/name/retire/description |
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

## Overview event

| Field | JSON | Type | Notes |
|-------|------|------|--------|
| Enabled | `enabled` | bool | false while `overview_enabled` is off |
| Headline | `headline` | string | optional; model output, sanitized, at most 200 runes |
| GeneratedAt | `generated_at` | RFC3339 time | optional |

In memory only; never persisted. See `docs/protocol.md`.

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

## On-disk layout

```
<state-dir>/
  sessions/
    <session-id>/
      meta.json      # SessionSummary JSON
      transcript.log # raw PTY bytes (append-only)
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
