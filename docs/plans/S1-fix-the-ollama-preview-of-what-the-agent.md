# S1 Fix the Ollama preview of what the agent is doing

## Summary
On this machine the summary pipeline works, but it is slow and fails often. The daemon log shows 557 calls over 2s (p50 2.4s, p90 3.2s, max 11.8s), 67 timeouts and 17 "consecutive call failures" outages. The configured `gemma2:2b` is not pulled, so the daemon falls back to `llama3.2:3b`.
On top of that, the code has defects that make descriptions stale or wrong: requests are dropped instead of deferred, nothing is re-queued after an outage or a state change, the queue holds duplicate ids with no fairness, a timed-out call blocks every session for 15s, and agy chrome leaks into the model input.
The fix: a coalescing scheduler in the worker, per-attempt timeouts, a re-summary on state change plus one final summary at done, agy chrome filtering in termtext, and `description_at` on the wire so clients can see how old a summary is.

## Current behavior (file:line)
- The trigger is `internal/daemon/pty/supervisor.go:490-509`. It sends the session id without blocking when the screen changed and is either idle ≥500ms (`summaryIdleThreshold`, :515) or 15s have passed (`summaryCeiling`, :520). After a successful send it clears `dirty`, so the request is assumed handled.
- Model input is `cmd/rex-daemon/main.go:139-147`: `store.Screen(id)` (vt10x screen → `termtext.ScreenLines` → `Tail` 2048B, `supervisor.go:641-656`), with the transcript tail as fallback. Note: the summarizer is wired in `cmd/rex-daemon/main.go:117-149, 184-196`, not in `internal/daemon/boot`. Boot only holds `ProbeOllamaHealth` (`boot/ollama.go:22`).
- The worker is `internal/features/summarizer/worker.go`:
  - :88 is a single FIFO `chan string` of 64 with no dedupe.
  - :155 `handle`; :164 skips done/failed/crashed.
  - :174 is the MinInterval gate. It **drops** a request that arrives within 800ms of the last one, and the supervisor has already cleared `dirty`.
  - :156 drops every request while the backend is unavailable, and nothing re-queues it later.
  - :210 is one 15.5s budget shared by both attempts, so a retry after a timeout always fails.
  - :218 marks the backend unavailable after 3 failures. Only the 30s probe brings it back.
  - `perSess` is never pruned.
- `client.go:85`: `num_predict: 30`, no `stop`. `client.go:32`: 15s HTTP timeout.
- `termtext.go`:
  - :79 `inputMarkers` are only `❯` and `›`. agy's `>` input row (with the user's typed text) is never cut.
  - :135 `turnDoneRe` matches `…` but not ASCII `...`, so agy's `⣷  Generating...` passes `IsChrome` (:140) and becomes the last line. That makes it both the board's `last_line` and the most prominent line in the prompt. A1 confirmed both leaks.
- `state/store.go:536-555`: `UpdateDescription` sets `DescriptionAt`, but `Session.Summary()` (`state/session.go:76-101`) and `protocol.SessionSummary` (`wire/protocol/events.go:32-79`) don't carry it. The TUI can't tell a 10-minute-old summary from a fresh one.
- `surface/tui/board.go:495-504`: the default (simple) view shows `rowText` (the task title). The AI summary only appears in the detailed view (`v`, `rowDescription` :716) or when a session has no title.

## Root cause
Ranked, with evidence:
1. **Slow or flaky backend (confirmed from `~/.local/state/rex/daemon.log`).**
   - Every daemon start logs `model_substituted from=gemma2:2b to=llama3.2:3b`. Pulled models: `qwen3:30b-a3b`, `llama3.1:latest`, `llama3.2:3b`.
   - 52 errors are the 15s HTTP `Client.Timeout`. 15 are the retry hitting the shared deadline.
   - 17 outages last until the next 30s probe; 6 more say `model not pulled: gemma2:2b`.
   - Likely contributor: the 18.5GB qwen3 or llama3.1 being loaded evicts the 3B model, and then a 15s timeout stalls the single worker for all sessions.
2. **Requests are lost (confirmed from code).**
   - The MinInterval gate drops a request instead of deferring it (worker.go:174).
   - Requests that arrive while unavailable are dropped (:156).
   - Neither is retried, because the supervisor already cleared `dirty`. Whatever changed last on screen, such as the agent going idle with a question, is never summarized until new output arrives. The description freezes on old text.
3. **agy chrome leaks into the input (confirmed from code; A1 found the same).** `Generating...` is the last line, and the `>` input row with typed text survives the filter. agy summaries come out as "generating response" or echo the user's draft.
4. **State is ignored.**
   - The prompt (`prompt.go:13`) has no state, so the model can't tell working from waiting.
   - A working → needs_input change doesn't force a re-summary.
   - Done rows keep their last "Running tests…" text forever (worker.go:164).
5. **No fairness under load (confirmed from code; depends on load).** With N working sessions at about 2.4s per call, the 64-slot FIFO fills with duplicates and chatty sessions starve quiet ones. In the last run only one session was active, so this is a secondary issue.
6. **Product, not a bug:** the default view never shows the summary. S3 adds a selected-row preview line rather than reversing PR #11.

What the builder runs first (diagnostics, before any code; nothing gets committed):
- D1: `grep -E 'summarizer|summary worker busy' ~/.local/state/rex/daemon.log | tail -300`. Confirm the numbers above on the latest run.
- D2: `ollama ps` while 2–3 sessions work. Then time a warm call: `time curl -s localhost:11434/api/generate -d '{"model":"llama3.2:3b","prompt":"<2KB of text>","stream":false,"options":{"num_predict":64}}'`. Repeat with `qwen3:30b-a3b` loaded to check eviction.
- D3: `REX_LOG_LEVEL=debug rex daemon restart`, run 3 sessions (claude, agy, codex) for 5 minutes, then count `summarizer: request`, `skipped_unchanged`, `skipped_empty_screen` and `pty: summary worker busy`. Note the time from a state flip to the next `response`.
- D4: replay harness (step 1 below) on a real local claude and agy `transcript.log` to see the exact model input. Never commit the output; fixtures stay synthetic (A1 hands over agy ones).
- D5: ask the user whether "preview" means the detailed view's text. That decides S3's preview line.

## Design
1. **Coalescing scheduler** in `worker.go`. The external contract stays the same (`Channel() chan<- string`, so `server.Config.SummaryRequest` and the supervisor are untouched).
   - `Start` runs one loop that drains `ch` without blocking into `pending map[string]pendingReq{since time.Time; prio bool}`.
   - It picks the eligible entry with the earliest `since`, with `prio` first. Eligible means `now - lastSubmittedAt >= MinInterval`.
   - If nothing is eligible, it waits on a `time.Timer` set to the earliest eligibility, on `ch`, or on `wake` (a `chan struct{}` with capacity 1).
   - Duplicate ids collapse into one entry, and requests are deferred, never dropped.
   - While unavailable, pending entries are kept, and `MarkAvailable` sends on `wake`.
   - Drop `perSess` and `pending` entries on `state.EventRemoved`.
   - Add an `ollamaSlot chan struct{}` (capacity 1). Every Ollama call acquires it, which S2's `Summarize` reuses.
2. **Per-attempt timeout.** Each attempt gets `RequestTimeout` (still 15s).
   - Retry once only for transient errors (connection refused or reset, HTTP 5xx), and only if the parent ctx still has time.
   - Never retry a deadline.
   - Debug logs gain `wait_ms` (time from request to call), `queue_len` and `model`. Never log screen text.
3. **State-driven refresh.** `Start` subscribes to the store (`Store.Subscribe`; the callback must not block, so it takes the mutex, writes the map and does a non-blocking send on `wake`).
   - A `state` patch to `working` or `needs_input` enqueues the session with `prio`, which bypasses the hash dedupe once (the state changes the prompt anyway once S2 adds State).
   - A patch to `done`, `failed` or `crashed` enqueues one **final** summary. `handle` allows a terminal state once per session (`meta.final`) and then refuses it, replacing the blanket skip at :164. The supervisor calls `refreshScreenText` before `Transition`, so the screen is the final one.
4. **termtext (S1 owns this file; A1 supplies fixtures).**
   - (a) `turnDoneRe` also accepts `\.\.\.`, as `^\S+ +\p{L}+(?:…|\.\.\.| for …)`, so `⣷ Generating...` becomes chrome.
   - (b) Add `bottomInputRe = ^>( |$)`. In `ScreenLines`, cut at the bottom-most row matching it only when every row below it is blank or chrome, or there are at most 3 such rows. That is agy's input box above its footer. A `>` row in the middle of the screen (markdown quote, history) is kept.
   - (c) Add `"generating..."` to the chrome hints only if A1's fixtures show it without a spinner glyph.
   - This also fixes the agy `last_line`.
5. **Wire `description_at`.**
   - Add `SessionSummary.DescriptionAt time.Time \`json:"description_at,omitzero"\``.
   - `Session.Summary()` copies it, `fromSummary` restores it, and the `UpdateDescription` patch adds `"description_at"`.
   - TUI `applyPatch` (model.go) parses it with `patchTime`.
   - The change is additive; older TUIs ignore it.

## Files to change
- `internal/features/summarizer/worker.go`: `New`, `Start`, `handle`, `callWithRetry`; new `schedule` / `next` helpers; `MarkAvailable` (wake).
- `internal/features/summarizer/worker_test.go`: rewrite `TestWorkerMinIntervalGate` to expect deferral.
- `internal/daemon/termtext/termtext.go`: `turnDoneRe`, `ScreenLines`, `IsChrome`. Tests in `termtext_test.go` and the fuzz seeds.
- `internal/wire/protocol/events.go` (`SessionSummary`).
- `internal/daemon/state/session.go` (`(*Session).Summary`).
- `internal/daemon/state/store.go` (`UpdateDescription`).
- `internal/daemon/state/persist.go` (`fromSummary`).
- `internal/surface/tui/model.go` (`applyPatch`).
- New `internal/daemon/pty/replay_test.go`: flag-gated harness `-replay=PATH`, skipped when unset. It reuses `newScreen` from `repaint_test.go` and prints `termtext.ScreenLines`.

## Test plan (test-first)
- Scheduler, using `httptest` fake Ollama servers and an injected clock or short intervals:
  - `TestWorker_DefersInsteadOfDropping`: 2 requests within MinInterval → 2 calls, the second after ≥MinInterval.
  - `TestWorker_CoalescesDuplicates`: 10 sends for the same id while a call is blocked → 1 more call.
  - `TestWorker_OldestFirstAcrossSessions`.
  - `TestWorker_PendingSurvivesUnavailable`: MarkUnavailable, send, MarkAvailable → call.
  - `TestWorker_StateChangeEnqueuesPriority`.
  - `TestWorker_FinalSummaryOnceAtDone`.
  - `TestWorker_ForgetsRemovedSession`.
  - `TestWorker_NoRetryAfterDeadline`: the fake sleeps longer than the timeout → exactly 1 request.
  - `TestWorker_RetriesTransient500`.
- Every worker test runs under `-race` with goleak (already in main_test).
- termtext:
  - `TestIsChrome` gains `⣷ Generating...`.
  - `TestScreenLines_AgyCutsInputRow`: A1's synthetic fixture, input `> fix the flaky test` above `? for shortcuts` → not in the output.
  - `TestScreenLines_KeepsMidScreenQuote`.
  - Fuzz seeds for both.
- State and wire:
  - `TestUpdateDescription_PatchHasDescriptionAt`.
  - Persist round-trip.
  - TUI `applyPatch` test.
- `make check` after every step.

## Docs/CHANGELOG
- `docs/agents/modules/summarizer.md`: scheduler, final summary, invariants.
- `docs/protocol.md` §SessionSummary and `docs/agents/data/schemas.md`: `description_at`.
- `docs/settings.md` AI summary: the fallback list is used and logged; suggest `ollama pull` for the configured model.
- `docs/tui.md`: agy last line.
- CHANGELOG `[Unreleased]` Fixed:
  - descriptions no longer freeze after a burst, an outage or a state change;
  - agy's spinner and input text no longer appear in last_line or summaries;
  - a finished session gets a final summary.
- Added: `description_at`.

## Conflict map
- `termtext.go`: S1 only. A1 adds fixtures to `termtext_test.go` (append-only) or hands them to S1.
- `protocol/events.go` `SessionSummary`, `state/session.go` `Summary()`, `persist.go` `fromSummary`: T2/T3/T4 add fields in the same struct literals. Each side adds lines, so merges are trivial; land S1's step 6 early or rebase.
- `tui/model.go` `applyPatch`: T2–T4 also add patch keys (adjacent lines).
- `cmd/rex-daemon/main.go` summarizer block: S3 adds a watcher line, S4 adds the fleet wiring.
- `board.go`: untouched in S1. `rowText` and `rowDescription` keep their names and semantics (T5's search haystack is safe).
- S4/S5 depend on S1's `ollamaSlot` and S2's API.

## Security notes
- No screen or prompt text is logged at any level. The replay harness reads local files only under an explicit flag, and committed fixtures are synthetic.
- `description_at` is a timestamp, not free text.
- The store subscriber follows the store contract (no blocking, no I/O), so it can't deadlock the store.
- Model output still goes through `cleanResponse` → `SanitizeDisplayText`, unchanged.

## Open questions for user
1. "Preview" means the detailed view's AI text, or should the default view show it? Recommended: keep the default view showing the task, and add S3's selected-row preview line in both views.
2. Pull `gemma2:2b`, or make `llama3.2:3b` the default `summary_model`? Recommended: keep the default, and have the setup wizard (P1) offer `ollama pull`. The fallback already picks llama3.2:3b.
3. Final "Done:/Failed:" summary for finished sessions? Recommended: yes.

## Ordered steps (each one commit, `make check` green)
1. `test(pty): flag-gated transcript replay harness` (no behavior change).
2. `fix(termtext): drop agy spinner rows and its bottom input row`, with A1's fixtures.
3. `refactor(summarizer): coalesce requests; defer instead of drop`, plus the Ollama slot and pruning.
4. `fix(summarizer): per-attempt timeout, retry only transient errors`.
5. `feat(summarizer): re-summarize on state change, final summary at done`.
6. `feat(protocol): carry description_at on sessions` (state, persist, TUI applyPatch).
7. `docs: summarizer scheduling, description_at, agy text` plus CHANGELOG.

## Size estimate
L
