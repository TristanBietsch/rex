# R15 Scheduled description refresh

## Summary
- User ask: "make the llm remake a new desc for the agent every 5 mins in a loop, then after the first 5 x 5 mins, do it every 30 but they are more descriptive but not too long."
- Today a description is rewritten whenever the PTY output goes quiet (or every 15 s of chatter) and on every state change. Busy sessions burn Ollama calls; the text describes the last 500 ms, not the run.
- New: each live session gets a scheduled refresh at 5, 10, 15, 20 and 25 minutes after its run started (early phase), then every 30 minutes (late phase). Late-phase descriptions are one detail level longer and say what is finished plus what is happening now.
- Output-driven refreshes go away while the flag is on. State-change refreshes stay.
- Flag `summary_schedule` (default on). Off is exactly today's behavior.

## Current behavior
- Producer: only the supervisor sends session ids into the worker channel (`SummaryRequest`, `internal/daemon/pty/supervisor.go:44,588-602`), gated by `shouldEmitSummary` (`:620`): dirty and 500 ms quiet, or 15 s since the last send. The channel is wired in `cmd/rex-daemon/main.go:186` -> `server.Config.SummaryRequest` (`main.go:236`, `internal/daemon/server/spawn.go:315`). That is the only producer.
- `Worker` (`internal/features/summarizer/worker.go:20`): single goroutine `loop` (`schedule.go:106`) drains `w.ch` into `pending` (one entry per session, earliest `since`, sticky `prio`: `enqueue` `schedule.go:19`), `next` (`:52`) picks the eligible one (MinInterval 800 ms per session, nothing while `BackendAvailable` is false). One Ollama call at a time through `slot` (`acquireSlot`, `schedule.go:136`).
- `onEvent` (`worker.go:196`): `EventRemoved` -> `forget` (`schedule.go:128`); `EventUpdated` with `NewState` -> priority enqueue, clears `meta.final` when the new state is live.
- `handle` (`worker.go:216`): reads the screen via the `TranscriptReader`, builds the KindActivity prompt at `w.Detail()`, hashes the prompt; equal hash and not prio -> `skipped_unchanged` (`:262`); on success `UpdateDescription`. A failed call re-enqueues the session; 3 failures mark the backend unavailable.
- Detail: `Detail` (`detail.go`) short 60 / medium 110 / long 180 runes; `SetDetail` is fed by `boot.SummaryDetailApplier` (`internal/daemon/boot/watch_settings.go:81`).
- Prompt: `activityPrompt` (`prompt_activity.go`), fixtures `testdata/screens`, golden `testdata/prompts`, live eval `eval_test.go`.
- Reopen (T3 Resume): `Store.Reopen` (`internal/daemon/state/store.go:352`) sets state `queued` but keeps `StartedAt`; the server then patches the state (`internal/daemon/server/resume.go:130`), which the worker sees as a state change from a terminal state to a live one.

## Design

### 1. Schedule
One pure type, one place (`internal/features/summarizer/cadence.go`):

```go
type Cadence struct{ EarlyEvery time.Duration; EarlyCount int; LateEvery time.Duration }
var DefaultCadence = Cadence{EarlyEvery: 5*time.Minute, EarlyCount: 5, LateEvery: 30*time.Minute}
func (c Cadence) Due(anchor time.Time, k int) time.Time // k-th tick, 1-based
func (c Cadence) Late(k int) bool                       // k > EarlyCount
```

Ticks: 5, 10, 15, 20, 25 min (k = 1..5, early), then 55, 85, ... min (k >= 6, late; 30 min after the previous). "The first 5 x 5 mins" is read as five early ticks; the first late tick is 30 min after the fifth, not at minute 30.

### 2. Anchor and live set
- The worker keeps `sched map[id]*schedEntry{anchor, fired}` for live sessions (queued, working, needs_input), under `w.mu`, always maintained, flag on or off, so a flag flip needs no reload.
- Entry created: on `EventAdded` (anchor `StartedAt`), at `Start` for live sessions already in the store (anchor `StartedAt`; none after a real daemon restart), and on a state change into a live state when no entry exists (Resume / `Reopen`: anchor = event time, which restarts the early phase; `fired = 0`).
- Entry dropped: new state done/failed/crashed, and `EventRemoved` (`forget`).
- Reopen coexists with the final summary: the same live-state event clears `meta.final` (today) and creates the entry.

### 3. Triggers
- State change: unchanged, immediate priority enqueue. It does not touch `sched`; ticks stay fixed relative to the anchor.
- Output-driven sends on `w.ch`: discarded by the worker while the flag is on, honored while off (all producers go through that one channel, so one gate covers them). The supervisor keeps sending; the cost is one non-blocking channel send per quiet burst. Gating inside the worker makes the live flag flip exact.
- Tick: the loop computes the earliest due entry, sleeps on one timer (the existing `timer`, no new goroutine), and on due enqueues the session (`prio=false`, `scheduled=true`, `late = Late(k)`). `enqueue` merges: one pending entry per session, `late` sticky. A tick that falls inside the session's in-flight call window is counted but not enqueued (merge with the call in flight). Missed ticks (long stall, backend down) are caught up in one step: `fired` advances past every due time, a single refresh is enqueued with the latest `k`.
- Unchanged screen: scheduled and ordinary requests keep the existing prompt-hash skip (`skipped_unchanged`); the tick still counts and the schedule moves on. The hash includes the prompt, so the first late tick after an early description is a call (the prompt differs) and later late ticks skip while the screen is unchanged.

### 4. Late phase
- Detail: `Detail.Up()`: short -> medium, medium -> long, long -> long (cap 180 runes, `detail.go`). Only scheduled late-phase requests use it; early ticks and state-change refreshes use the user's `summary_detail`.
- Prompt: new `KindProgress` (`prompt_progress.go`, next to `prompt_activity.go`; `BuildPrompt` gets one case). Same system rules as the activity prompt (no "Claude is...", no "It/I/We/This/Currently" opener, no quotes or markdown), length rule per level, and "say what is finished so far, then what is happening now, in one or two short clauses joined by ' — ' or '; '". Clauses rather than two sentences: the reply is one line (`Stop: ["\n"]`) and `Clean` strips a trailing period, so two literal sentences would not survive reliably.
- Examples per level, held to the same rule test as the activity examples (within cap, survive `Clean`, no subject opener).
- Fixtures: two new synthetic screens in `testdata/screens` (they also enter the activity golden), golden files `testdata/prompts/progress_*.txtar`, the live eval covers `KindProgress`.

### 5. Backend down
- `next` already returns nothing while the backend is unavailable; ticks keep merging into the one pending entry per session (`late` sticky), so nothing piles up. `MarkAvailable` wakes the loop and the pending entry runs (existing behavior; S1 guarantee 1).

### 6. Clock
- `Worker.now func() time.Time` (default `time.Now`) and `Config.Cadence` (zero value = `DefaultCadence`). Tests set a cadence of tens of milliseconds with real timers; the cadence math is tested purely. No timer per session: one `time.Timer` in `loop`.

### 7. Invariants kept (S1)
1. Defer, never drop: a tick that cannot run yet stays in `pending`.
2. Coalescing: ticks, state changes and output sends collapse to one pending entry (earliest `since`, sticky `prio`).
3. One shared Ollama slot: ticks join the same queue; no parallel call.
4. Final summary on done; `meta.final` reset on reopen.
5. No screen or prompt text in logs: new log lines carry session id, `k`, phase only.

## Feature flag
- `summary_schedule` (bool, default **on**), Settings -> Features. Help text states the cadence.
- Off: no ticks computed, the loop timer is never armed for the schedule, `w.ch` sends are honored, the late-phase detail bump and `KindProgress` are never used. Exactly today's behavior. Off-is-inert test: no tick enqueued over several cadence periods.
- Live: `Worker.SetSchedule(bool)`, fed by an applier on the settings watcher (or the SettingsHub when present). On -> off: ticks stop at once. Off -> on: `fired` is recomputed from the anchor (ticks already past count as fired), the next tick is the next one on the anchor's grid.
- The daemon wires it only when `summary_enabled` is on (the summarizer does not run otherwise).

## Plan
Each step is one commit with its tests; `make check` green at the end.

1. **R15-1 Cadence and detail.** `Cadence`, `Due`, `Late`; `Detail.Up`. Tests: ticks at 5/10/15/20/25 then every 30 anchored to `StartedAt`; `Up` mapping and cap.
2. **R15-2 Progress prompt.** `KindProgress`, `prompt_progress.go`, fixtures, goldens, examples test, eval extension.
3. **R15-3 Scheduler.** `sched` map, anchors, loop timer, tick enqueue, `pendingReq.late/scheduled`, output gate, `SetSchedule`, late-phase request building. Tests: state change immediate and no shift, output ignored/honored, unchanged skip counts, detail+1 and progress prompt only late, removed session, backend down one pending, Reopen restarts, flag flip, goleak via `TestMain`.
4. **R15-4 Flag and daemon.** Registry row, `Config` wiring in `rex-daemon`, applier, registry test, off-is-inert test.
5. **R15-5 Docs.** `docs/settings.md`, `docs/daemon.md`, CHANGELOG, Build notes, coverage floor.

## Review focus
- The in-flight merge rule and the catch-up rule: no double call, no burst after a stall.
- Gating output sends in the worker: a full 64-slot channel while the flag is on is drained and discarded, never blocks the loop.
- Reopen: `StartedAt` is not the anchor after a resume; the state-change event is. A session already live when the flag turns on keeps its original anchor.
- Hash skip interplay: the first late tick after the early phase is always a call.
- Merge conflicts: S1 owns `worker.go` and `schedule.go`; SettingsHub replaces `WatchSettings` appliers in `main.go`; the registry test counts Features rows.

## Decisions (given 2026-11-11, not re-asked)

| # | Question | Answer |
|---|---|---|
| D1 | Cadence | Scheduled refresh at 5, 10, 15, 20, 25 min after the run start (early phase), then every 30 min while the session is live. Terminal sessions: none; the final summary stays. |
| D2 | Anchor | `StartedAt`, or the reopen time on Resume / `Store.Reopen` (restarts the early phase). A daemon restart makes live sessions crashed, so nothing carries over. |
| D3 | Triggers | Keep the immediate priority refresh on state change; remove PTY-output-driven refreshes while the flag is on; a state-change refresh does not shift the schedule; a tick merges with a pending or in-flight refresh. |
| D4 | Unchanged screen | Skip the LLM call; the tick still counts and the schedule moves on. |
| D5 | Late phase | Detail one level up (short->medium->long->long, cap 180 runes); the prompt asks what is finished plus what is happening now, same style rules. Early and state-change refreshes use `summary_detail`. |
| D6 | Flag | `summary_schedule`, bool, default on, Settings -> Features; off = today's behavior; applies live; constants in one place, no other settings. |
| D7 | Clock | Injectable clock/cadence, one scheduler loop with one timer, `forget` drops the schedule. |
| D8 | Summarizer down | At most one pending refresh per session; runs when the backend returns. |

## Build notes
As built (branch `feat/r15-desc-schedule`):

- Files: `cadence.go` (`Cadence`, `DefaultCadence`, `Due`, `Late`, `Elapsed`), `desc_schedule.go` (live set, `SetSchedule`, `fireDue`, `nextTick`), `prompt_progress.go` (`KindProgress`), `Detail.Up` in `detail.go`; `worker.go`/`schedule.go` carry the hooks (`pendingReq.late`, `sessionMeta.callStart/callEnd`, `fromOutput`). `boot.SummaryScheduleApplier` subscribes through the SettingsHub; `rex-daemon` seeds `Config.Schedule` from `hub.Current()`.
- `Config.Schedule` defaults to false in the library (`Defaults()`), so pre-R15 tests keep their meaning; the daemon sets it from the flag (default on).
- Resume reaches the worker as a state patch without `NewState` (`resume.go` calls `Store.BroadcastPatch`), so the live-set tracking reads `Patch["state"]` too; the priority enqueue stays on `NewState` only (unchanged S1 behavior).
- Sessions already live when the worker starts, and sessions when the flag turns on, count past ticks as fired and do not replay them.
- `New` maps `MinInterval == 0` to 800 ms, so tests that want no gap pass 1 ms.
- Tests use real timers with a scaled `Config.Cadence` (tens of ms) in `desc_schedule_test.go`; the cadence math in `cadence_test.go` is pure. `now` is an injectable clock for anchors and ticks, timers stay real.
- New synthetic screens `claude_longrun`, `codex_migration` also entered the activity golden set.
- Coverage floor for the summarizer stays at 93 (measured 93.4%).
- `TestSnapshots_OldGitDisablesCaptures` fails in `internal/daemon/boot` on this machine; unrelated to R15 (not touched).
