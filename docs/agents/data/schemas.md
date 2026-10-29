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
| Slug | `slug` | string | ≤64 runes, no whitespace or control characters, unique |
| Title | `title` | string | optional; one line, ≤512 runes (daemon flattens and truncates); archived sessions use title prefix (CLI) |
| CWD | `cwd` | string | |
| State | `state` | string | `queued`, `working`, `needs_input`, `done`, `failed`, `crashed` |
| StartedAt | `started_at` | RFC3339 time | |
| LastEventAt | `last_event_at` | RFC3339 time | last real activity (state change, screen line, exit); not bumped by rename/fleet/name/retire/description |
| LastLine | `last_line` | string | optional |
| Description | `description` | string | optional; from summarizer |
| ExitCode | `exit_code` | int | optional pointer |
| Tokens | `tokens` | int64 | optional; heuristic |
| RetireAt | `retire_at` | RFC3339 time | optional; when a marked-done session gets cleared |
| RetiredAt | `retired_at` | RFC3339 time | optional; when the session was marked done (absent on legacy marks); drives the board `done <time>` badge |
| OutputBytes | `output_bytes` | int64 | optional | |
| Fleet | `fleet` | string | optional; one line, ≤64 runes |

Defined: `internal/wire/protocol/events.go` (`SessionSummary`). Persisted: `internal/daemon/state/persist.go` writes same shape to `meta.json`.

## Snapshot (Hello response)

| Field | JSON | Type |
|-------|------|------|
| Sessions | `sessions` | `SessionSummary[]` |
| Filter | `filter` | string | always `"all"`; filtering is client-side |

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

## NewSession intent

| Field | JSON | Required |
|-------|------|----------|
| tool_id | string | yes |
| model_id | string | yes |
| effort | string | no |
| slug | string | yes |
| title | string | no |
| cwd | string | yes (existing directory) |
| initial_prompt | string | no |
| fleet | string | no |

## On-disk layout

```
<state-dir>/
  sessions/
    <session-id>/
      meta.json        # SessionSummary JSON
      transcript.log   # raw PTY bytes (append-only; rotated at 64 MiB)
      transcript.log.1 # previous transcript after a rotation
      hooks.log        # state words written by agent hooks
```

Default `<state-dir>`: `~/.local/share/rex` (`rex-daemon -state-dir`).

## tools.yaml

Root: `tools:` array of `registry.Tool` (see `internal/catalog/registry/types.go`). Merged with embedded `internal/catalog/registry/builtin.yaml`. Not committed at repo root.

## config.yaml

Flat top-level keys matching `settings.Registry` IDs. Unknown keys ignored on load. Path: `settings.DefaultPath()` → `~/.config/rex/config.yaml`. Human reference: [../../settings.md](../../settings.md).

## tui-state.json

Written by `internal/surface/tui/persist.go` at `~/.local/state/rex/tui-state.json` — selection and filter strings (read once on next launch then deleted). Exact JSON keys: see `persist.go` (not exported as named struct).

## Registry Tool (YAML)

See `modules/registry.md` for `Tool`, `Model`, `Detect`, `Effort` fields.
