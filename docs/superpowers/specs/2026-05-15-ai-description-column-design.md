# AI Description Column — Design

Date: 2026-05-15
Status: Approved (brainstorming) — pending implementation plan

## Summary

Replace the middle "desc" column in the rex TUI board — which currently shows the
last non-empty line of sanitized PTY output — with a stable, AI-generated
description of what each agent is doing right now. The summary is produced by a
local Ollama daemon, driven by the rex daemon, delivered through the existing
event broker, and rendered with a configurable text animation when it changes.

## Problem

Today, `s.LastLine` (set in `internal/pty/supervisor.go` and rendered in
`internal/tui/board.go:renderRow`) is the last non-empty line of sanitized PTY
output. Three problems compound:

1. **Garbled** — TUI-style agents (codex, gemini) redraw with cursor-positioning
   escapes. Sanitization strips escapes but still leaks fragments of overlapping
   frames (e.g. the `Received.•rking•king›Find and fix a bug in @filenamegpt-5.5 high`
   artifact observed in production).
2. **Flickery** — every PTY chunk with visible text refreshes the column.
3. **Uninformative** — a transcript fragment is not a status. `running pnpm test:billing --watch=false…`
   is a *line*, not a description.

## Decision

- **Approach**: LLM-narrated agent status (option B). The column shows what the
  agent is doing, inferred from the recent transcript by a local Ollama model.
- **Dependency posture**: hard dependency on a local Ollama daemon. If absent,
  the column falls back to `LastLine` and a dim banner instructs the user how to
  install / pull the model. No silent degradation: the user must know.
- **Trigger model**: hybrid idle-debounce + ceiling timer.
  - On 500ms idle with new bytes since last summary → summarize.
  - On 15s ceiling elapsed with new bytes (regardless of idle) → summarize.
- **Animation**: when the description changes, animate the cell using one of
  four configurable effects: `typewriter`, `decode`, `wipe`, `off`. Default
  `typewriter`. The existing `reduce_motion` setting forces `off` at render
  time regardless of the configured choice.

## Architecture

```
PTY chunk
   │  (supervisor.go) — existing path, unchanged
   │  appendBounded → window
   │  lastNonEmptyLine + sanitizeForDisplay
   ▼
Store.UpdateLastLine(id, line)             ← unchanged; still used for fail
                                              popup, JSON output, and as the
                                              renderer's fallback before the
                                              first summary lands

  ── new path, additive only ──

(supervisor.go) on each IdleTick branch:
   if dirty && (idle ≥ 500ms || since(lastSummaryAt) ≥ 15s):
       non-blocking send: summaryCh <- sessionID
       dirty = false; lastSummaryAt = now

   ▼
summarizer.Worker (single goroutine in rex-daemon)
   │  consume sessionID from channel
   │  min-interval gate per session (drop if recently submitted)
   │  read sanitized transcript window (last 2KB) + slug + tool from store
   │  skip-if-unchanged: FNV-64 hash of prompt input; if equal to last accepted
   │                     call's hash, no HTTP request
   │  POST {base}/api/generate { model, prompt, stream:false,
   │                             options:{num_predict:30, temperature:0.2} }
   │  clamp to 60 chars, strip surrounding quotes / "Output:" / leading "- "
   ▼
Store.UpdateDescription(id, desc)          ← new method; fires SessionUpdated

   ▼
event broker (existing) → TUI

(tui/update.go) on SessionUpdated:
   if prev.Description != next.Description && descAnimEnabled(m):
       register DescAnim{ From: prev, To: next, Effect, Started, Duration }
       ensure on-demand descTickMsg is queued

(tui/board.go) on each render:
   text := next.Description (fallback: LastLine if empty)
   if anim active: text := renderAnimFrame(anim, descW, now)
```

## Components

### New package: `internal/summarizer/`

Three files, ~250 LOC total:

**`config.go`**
```go
type Config struct {
    BaseURL        string        // default "http://127.0.0.1:11434"
    Model          string        // default "gemma2:2b"
    RequestTimeout time.Duration // default 4s
    MinInterval    time.Duration // default 800ms — per-session floor
    MaxBytes       int           // default 2048 — transcript window size
}
```

**`client.go`** — Ollama HTTP client.
- Single shared `*http.Client{Timeout: cfg.RequestTimeout}` with
  `Transport: &http.Transport{MaxIdleConnsPerHost: 1, IdleConnTimeout: 90s}`
  for keep-alive.
- `(*Client).Tags(ctx) ([]string, error)` — health + model presence check.
- `(*Client).Generate(ctx, model, prompt) (string, error)` — single shot,
  `stream: false`, returns the `response` field.

**`worker.go`** — channel consumer + dedup + prompt builder.
- Single goroutine. `MaxConcurrent = 1` is implicit.
- Per-session map: `lastHash uint64`, `lastSubmittedAt time.Time`.
- For each incoming `sessionID`:
  1. `time.Since(lastSubmittedAt) < MinInterval` → drop.
  2. Read session + transcript tail (`MaxBytes`) from store.
  3. Skip if session state is `done`, `failed`, or `crashed`.
  4. Build prompt; FNV-64 over prompt body.
  5. If hash equals `lastHash` → log `skipped_unchanged`, drop.
  6. POST to Ollama with one retry on 5xx / network error after 500ms.
  7. Clamp + strip; if non-empty, call `Store.UpdateDescription`.
  8. Track consecutive failures across all sessions; ≥3 → flip
     `BackendUnavailable` flag and start the 30s health-recheck loop.

Public API:
```go
type Worker struct { /* unexported */ }
func New(cfg Config, store *state.Store) *Worker
func (w *Worker) Start(ctx context.Context) error  // launches goroutine
func (w *Worker) Channel() chan<- string           // supervisor writes here
func (w *Worker) BackendAvailable() bool           // for TUI banner
```

### Prompt design

```
You are watching a CLI coding agent named {{tool}} working on a task slugged "{{slug}}".
Below is the recent terminal output from the agent (ANSI stripped).
In ONE line of at most 60 characters, describe what the agent is doing RIGHT NOW.
Use simple verbs. No quotes, no preface, no trailing period.

Examples:
- rewriting webhook handlers — 12 of 14
- running pnpm test:billing
- waiting on user: pick a theme

Transcript:
{{last 2KB sanitized}}
```

Defense in code (independent of model compliance):
- Clamp to 60 runes (truncate with ellipsis).
- Trim surrounding `"` / `'`.
- Strip leading `Output:`, `Description:`, `- `, `> `.
- Strip trailing `.` if not part of a number.

### PTY supervisor changes (`internal/pty/supervisor.go`)

Additive only — does not change existing semantics.

In `pty.Config`:
```go
SummaryRequest chan<- string  // sessions needing summary; nil disables
```

In the read goroutine local state:
```go
var (
    dirty          bool
    lastSummaryAt  time.Time
)
```

- On every visible chunk: `dirty = true` (alongside existing `lastChunk = time.Now()`).
- In the existing `IdleTick` branch (next to adapter classification):
```go
if s.cfg.SummaryRequest != nil && dirty {
    idle := time.Since(lastChunk)
    if idle >= 500*time.Millisecond || time.Since(lastSummaryAt) >= 15*time.Second {
        select {
        case s.cfg.SummaryRequest <- sess.ID:
            dirty = false
            lastSummaryAt = time.Now()
        default: // worker backed up, retry next tick
        }
    }
}
```

No new ticker. Non-blocking send means PTY reads never stall on a slow worker.

### Protocol & state

`internal/protocol/events.go` — `SessionSummary`:
```go
Description string `json:"description,omitempty"`
```

`internal/state/session.go` — `Session`:
```go
Description   string
DescriptionAt time.Time
```

`internal/state/store.go` — new method:
```go
// UpdateDescription records the AI-generated activity summary for a session.
func (s *Store) UpdateDescription(id, desc string) error {
    // mirrors UpdateLastLine: lock, mutate, emit SessionUpdated
}
```

`internal/state/persist.go` — include `Description` and `DescriptionAt` in the
snapshot, so descriptions survive daemon restarts (matches existing
`LastLine` persistence pattern).

### TUI rendering (`internal/tui/board.go`)

In `renderRow`, replace the desc cell construction:
```go
text := s.Description
if text == "" {
    text = s.LastLine // bootstrap fallback
}
if anim, ok := m.DescAnim[s.ID]; ok {
    text = renderAnimFrame(anim, descW, time.Now())
}
desc := lipgloss.NewStyle().Foreground(descColor).Width(descW).Render(truncate(text, descW))
```

### Animation (`internal/tui/anim.go`, new)

```go
type DescAnim struct {
    From, To  string
    Effect    string // "typewriter" | "decode" | "wipe"
    StartedAt time.Time
    Duration  time.Duration
}

func renderAnimFrame(a DescAnim, width int, now time.Time) string
```

Implementations operate on `width`-padded rune slices so output is always
exactly `width` runes wide (no row-length drift mid-animation).

- **typewriter** (300ms): first `floor(p·len(to))` runes are `to`; the rest are
  spaces to preserve width.
- **decode** (400ms): each position `i` has settle threshold `i/len(to)`.
  Before threshold: noise glyph from `!@#$%&*+=?<>~/\` keyed by
  `(now.UnixMilli()/40 + i) % len(noise)` so it flickers across frames.
  After: target rune.
- **wipe** (250ms): `█` cursor at column `floor(p·width)`. Cells left of cursor
  are `to`; cells right of cursor are `from`.
- **off**: not registered; renderer skips the anim branch.

Width invariance is asserted in tests.

### Animation tick

A separate, on-demand 33ms tick (`descTickMsg`) — independent of the spinner
tick (which is 100ms and bound to working-state sessions).

In `internal/tui/update.go`:
- On `SessionUpdated` with a changed description: register the anim entry; if
  the map transitions empty → non-empty, queue one `descTickMsg`.
- On `descTickMsg`: drop entries where `now ≥ StartedAt + Duration`; if any
  remain, queue another tick. Otherwise stop.

Zero cost while no row is animating.

Mid-flight replacement: if a new description arrives while an animation is in
flight for that session, replace the entry with `From = old.To`, fresh
`StartedAt`, new `To`. Log `desc_anim: replaced_in_flight`.

### Settings

`internal/settings/...` — three new keys with defaults:

| Key | Type | Default | Notes |
|---|---|---|---|
| `summary_enabled` | bool | `true` | When false, worker never starts; column uses `LastLine`. |
| `summary_model` | string | `"gemma2:2b"` | Any Ollama-pulled model. |
| `desc_animation` | string | `"typewriter"` | One of `typewriter`, `decode`, `wipe`, `off`. |

`internal/tui/settings.go` — new "AI summary" group with three rows. The
`desc_animation` row cycles through values on Enter (same pattern as the
`prompt_glyph` cycling commit).

`reduce_motion = true` forces `desc_animation` to behave as `off` at render
time without mutating the setting.

### First-run / health check

In `cmd/rex-daemon/main.go`, after store init and before worker start:

1. If `summary_enabled = false` → skip entirely.
2. `GET {base}/api/tags` (2s timeout).
3. Unreachable → log warn, set `BackendUnavailable = true`. Worker launches but
   no-ops on incoming signals.
4. Reachable → check `summary_model` appears in tags. If not → same flag, log
   warn telling user to `ollama pull <model>`.
5. While `BackendUnavailable`, a goroutine re-checks every 30s. On recovery:
   clear flag, log `backend_restored` with `downtime_ms`.

TUI banner — a dim one-line row between the header (`header.go`) and the board
content when the flag is set:
```
ollama unreachable — install: https://ollama.com  ·  pull: ollama pull gemma2:2b
```

The banner is dim, single-line — no red, since core rex still works. Desc
column falls back to `LastLine` while the flag is set (renderer already
handles `Description == ""`).

**Delivery to the TUI**: a new event type `SummarizerHealthChanged { Available
bool, Reason string }` published on the existing event broker whenever the
flag flips. The TUI subscribes alongside `SessionUpdated` and stores the
boolean on `Model`. No polling.

### Error handling (runtime)

Per Ollama call:
- 4s `http.Client.Timeout`.
- One retry after 500ms on 5xx or network error.
- Both attempts fail → log warn, keep previous description (sticky).
- 3 consecutive failures across any sessions → flip `BackendUnavailable`, kick
  re-check loop.
- Slow calls (>2s) → log info with `elapsed_ms`.

Per-session: a failed summary never clears the previous description.

**Signal-vs-call mismatch**: the supervisor clears `dirty` and updates
`lastSummaryAt` on a successful channel send. The worker may then drop the
signal (min-interval gate, skip-if-unchanged, terminal state). That is
intentional — supervisor's job is to throttle signal generation, worker's job
is to throttle Ollama calls; they don't need to agree. A dropped signal costs
at worst one update opportunity, which the next batch of bytes recovers.

**Runtime toggling of `summary_enabled`**: the worker goroutine is always
running; when the setting is false it no-ops on incoming signals. Health
checks are gated on the setting too — when false, no probes, no banner.

## Lightweight optimizations (already baked in)

- Trigger logic piggybacks on the existing `IdleTick` — **no new tickers**.
- Worker is **single-goroutine**; "inflight" is implicit. Dedup is just
  `lastSubmittedAt + MinInterval` per session.
- **Skip-if-unchanged**: FNV-64 hash of the prompt body short-circuits when the
  transcript window is byte-identical to the last accepted call.
- **2KB** transcript window (down from a 4KB starting point).
- Only `working` and `needs_input` sessions are summarized.
- `num_predict: 30`.
- **HTTP keep-alive** to Ollama (single connection, 90s idle).
- Animation tick is **on-demand** — fires only while a row is mid-animation.

Steady-state cost with 8 active sessions:
- Mostly idle: ≈ 0 Ollama calls/sec (skip-if-unchanged dominates).
- Mixed activity: 1–3 calls/sec serialized → typically <100ms GPU per second.
- rex process memory: trivial (a 2KB buffer per active session).
- Ollama process memory: model floor (gemma2:2b ≈ 1.6GB resident).

## Logging (slog)

Per project convention, daemon and TUI log to `~/.local/state/rex/`.

**Daemon (`daemon.log`):**

| Event | Level | Fields |
|---|---|---|
| `summarizer: started` | info | model, base_url, min_interval |
| `summarizer: backend_unavailable` | warn | reason |
| `summarizer: backend_restored` | info | downtime_ms |
| `summarizer: request` | debug | session_id, bytes_in |
| `summarizer: response` | debug | session_id, duration_ms, chars_out |
| `summarizer: skipped_unchanged` | debug | session_id |
| `summarizer: error` | warn | session_id, err, attempt |
| `summarizer: slow_call` | info | session_id, duration_ms |

**TUI (`tui.log`):**

| Event | Level | Fields |
|---|---|---|
| `desc_anim: start` | debug | session_id, effect, duration_ms |
| `desc_anim: replaced_in_flight` | info | session_id |
| `desc_anim: completed` | debug | session_id, elapsed_ms |

## Testing

**Unit**
- `summarizer/client_test.go` — mock Ollama returning success, 500, timeout;
  verify keep-alive reuse.
- `summarizer/worker_test.go` — trigger combos (dirty+idle / dirty+ceiling /
  clean), skip-if-unchanged hash hit, min-interval gate, terminal-state skip.
- `summarizer/prompt_test.go` — clamp to 60 chars; strip surrounding quotes,
  `Output:`, leading `- `.
- `tui/anim_test.go` — `renderAnimFrame` at `p ∈ {0, 0.25, 0.5, 0.75, 1.0}` for
  each effect; output is always exactly `width` runes wide; decode noise
  alphabet bounded to the expected set.

**Integration**
- `cmd/rex-daemon/summarizer_integration_test.go` — fake Ollama HTTP server +
  canned PTY output; assert a description lands on the session within ~2s.
- Extend `internal/tui/snapshot_test.go` with rows mid-animation in each effect.

**Manual checklist** (run before merging):
- Real Ollama running + codex session: column is calm, descriptions accurate.
- Cycle through each effect via `:settings`; visual smoke test.
- Stop Ollama mid-session: banner appears, desc falls back to `LastLine`, no
  crash. Restart Ollama: banner clears, descriptions resume.
- Toggle `reduce_motion`: animations stop immediately, no visual artifact.
- Toggle `summary_enabled` off: column reverts to `LastLine`, no health checks,
  no banner.

## Files touched

**New** (~620 LOC code + tests):
- `internal/summarizer/config.go`
- `internal/summarizer/client.go`
- `internal/summarizer/worker.go`
- `internal/summarizer/client_test.go`
- `internal/summarizer/worker_test.go`
- `internal/summarizer/prompt_test.go`
- `internal/tui/anim.go`
- `internal/tui/anim_test.go`

**Modified**:
- `internal/pty/supervisor.go` — `SummaryRequest` channel in Config, dirty
  bool, trigger in existing IdleTick branch.
- `internal/state/session.go` — `Description`, `DescriptionAt` fields.
- `internal/state/store.go` — `UpdateDescription` method.
- `internal/state/persist.go` — persist new fields.
- `internal/protocol/events.go` — `Description` on `SessionSummary`.
- `internal/tui/board.go` — read `Description`, apply animation frame.
- `internal/tui/model.go` — `DescAnim` map, `descTickMsg` plumbing,
  `BackendUnavailable` flag.
- `internal/tui/update.go` — animation registration on `SessionUpdated`,
  `descTickMsg` handler, banner state from daemon health events.
- `internal/tui/header.go` — render the dim "ollama unreachable" banner row
  when the flag is set.
- `internal/tui/settings.go` — three new rows.
- `internal/settings/*` — three new keys with defaults.
- `cmd/rex-daemon/main.go` — wire `summarizer.Worker`, pass channel to
  supervisor, kick health-check goroutine.

## Out of scope

- Replacing `LastLine` entirely. It stays for the fail popup, the
  `rex ls --json` output, and as the renderer's bootstrap fallback.
- Cloud LLM providers. The dependency is local Ollama only.
- Custom prompt templates per tool. One template covers all agents; the prompt
  inlines `tool` and `slug` for context.
- Multi-line descriptions. The cell is single-line by design.
- Streaming the summary back to the column. The full result lands at once and
  the animation runs on the TUI side.

## Non-goals / future work

- A reasoning-effort knob for the summarizer model (e.g., second-pass
  refinement on long sessions).
- Per-tool prompt overrides.
- Summarizer model auto-pull on first run.
- Translating descriptions to non-English locales.
