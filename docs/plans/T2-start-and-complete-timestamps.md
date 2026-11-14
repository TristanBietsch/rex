# T2 Start and complete timestamps

**Summary**
- Add a daemon-recorded `ended_at` (the first time the session reached a terminal state) next to the existing `started_at`. Persist it and send it on the wire.
- "Complete", as shown, is when the row entered the Done column: the earlier of `retired_at` (marked done) and `ended_at` (process exit, `C`, or done_regex). That matches the board's `inCompleted` rule exactly.
- The detailed view (`v`) gets a `start→end` span column (`14:02→15:30`). `rex ls -wide` gets STARTED and ENDED columns. The simple view is unchanged, because the user removed the `done 14:32` chip in 397a8a6.

**Current behavior**
- `internal/daemon/state/session.go:22-23,45-54`: `StartedAt`, `LastEventAt`, `RetireAt`, `RetiredAt` and `WorkingSince` exist. No end time is recorded.
- `LastEventAt` is not a reliable end time. `store.go:475-497` `UpdateLastLine` bumps it on every new screen line, so a heuristic `done_regex` session whose process is still alive keeps moving it forward. `persist.go:181-187` `LoadAll` rewrites live sessions to `crashed` with no end time.
- Exit path: `pty/supervisor.go:425-445` calls `sess.SetExit` (`session.go:105`), then `WriteMeta`, then `Store.Transition` (`store.go:232`). Complete path (`supervisor.go:413-424`): `Transition(done)` then `WriteMeta`.
- The board's only time column is the "last event" age (`board.go:529`, `durationAgoAt`). The `done <time>` chip was removed in 397a8a6, but `docs/tui.md:48,163` and `docs/agents/data/schemas.md:37` still describe it (stale docs).
- `core/output.go:14` `WriteSessionsTable` has no time columns except LAST EVENT. NAME must stay last (comment at line 19).

**Design**
- **What "complete" means.** Three events can put a row in Done: the process exits, `C`/Complete, or a mark (`cc`/`:done`/`rex done`). Process exit alone misses marked sessions that are still alive. The mark alone misses natural exits. State `done` alone misses failed and crashed. So:
  - The daemon records the objective event: `EndedAt` is set the first time State becomes done, failed or crashed. It is never moved later; for example a done_regex session that later exits keeps its first time. It is cleared when the state goes back to queued/working/needs_input (T3 resume).
  - The client derives what to display: `completedAt(s) = min(non-zero RetiredAt, EndedAt)`, with fallback `EndedAt := LastEventAt` when the state is terminal and `ended_at` is absent (older daemon).
- **State.** Add `EndedAt time.Time` to `Session`. Add an `isTerminal(State)` helper and a `noteStateLocked(sess, newState, now) (patch keys)` helper in session.go, used by both `Transition` and `SetExit`:
  - `SetExit` must set `EndedAt`, because `WriteMeta` runs before `Transition` on the exit path. Otherwise meta.json would miss it.
  - `Transition` always puts `ended_at` in the patch when the new state is terminal (it carries the value; idempotent even when SetExit already set it). It puts `ended_at: nil` when clearing.
- **Persistence.** meta.json is `json.Marshal(Summary())` (`persist.go:107`), so adding the field to `SessionSummary` and `Summary()` persists it. `fromSummary` (`persist.go:372`) copies `EndedAt`.
  - `LoadAll` backfill: when the state becomes or stays terminal and `EndedAt` is zero, set `EndedAt = LastEventAt`, the best estimate for crashed sessions and old files. This happens in memory and is written on the next WriteMeta.
  - The `rex stats` and `rex digest` LoadAll callers see the same values.
  - Old meta files load fine because the field is optional.
- **Wire.** `SessionSummary.EndedAt time.Time \`json:"ended_at,omitzero"\``. The patch key `ended_at` sends RFC3339 to set and null to clear, the same convention as `retire_at`.
  - Old clients ignore it.
  - New clients on an old daemon use the LastEventAt fallback.
  - The protocol version stays 1.
- **TUI.** Add a new file `internal/surface/tui/board_times.go` to keep `board.go` churn small, with:
  - `completedAt(s)`
  - `clockStamp(now, t)`: `t.In(now.Location())`. Same day → `15:04`; same year → `Jan 2`; otherwise `2006`.
  - `spanText(now, s)`: `start→end`, or `start→` for live rows.
- **Board columns (`board.go`).**
  - `rowCols` gains `span`. `used()` counts it.
  - `layoutRows` sets `c.span = max width(spanText)` capped at `colSpan = 13`, only when `detailed`.
  - `renderRow` writes it after tokens and before agents, dim style.
  - Degrade order: insert at the front of `steps`: `drop resume` (T3), `drop span`. Then the existing chain runs unchanged (tokens → shrink model → shrink name → drop model → drop agents → drop run). The new columns are the least glanceable, so they go first and the documented order stays intact.
- **applyPatch** (`model.go:288`): handle `ended_at` through `patchTime`.
- **CLI.** `rex ls -wide` adds `STARTED` and `ENDED` (local `2006-01-02 15:04`, `-` if unset) before NAME. The default table is unchanged. `-json` already carries `ended_at`.

**Files to change**
- `internal/wire/protocol/events.go`: `SessionSummary` (new field).
- `internal/daemon/state/session.go`:
  - `Session` (field), `Summary()`, `SetExit` (modified).
  - New `isTerminal` and `noteStateLocked`.
- `internal/daemon/state/store.go`: `Transition` (modified).
- `internal/daemon/state/persist.go`: `fromSummary`, `LoadAll` (modified).
- `internal/surface/tui/board.go`: const block (`colSpan`), `rowCols`, `rowCols.used`, `layoutRows` (field plus `steps`), `renderRow` (one cell).
- New `internal/surface/tui/board_times.go`.
- `internal/surface/tui/model.go`: `applyPatch`.
- `internal/surface/cli/session/ls.go`: `RunLs` (`-wide` flag).
- `internal/surface/cli/core/output.go`: `WriteSessionsTable` gains a wide variant, `WriteSessionsTableWide`.

**Test plan (test-first)**
- Unit, state:
  - `TestTransition_EndedAtSetOnceClearedOnLive`: done → done keeps the first value; failed after done keeps it; working clears it; check the patch keys.
  - `TestSetExit_SetsEndedAtBeforeMetaWrite`: meta.json after the exit path contains `ended_at`.
  - `TestLoadAll_BackfillsEndedAt`: a legacy terminal file, plus a working file that becomes crashed.
  - `fromSummary` round trip.
- Unit, protocol: codec round trip with `ended_at` omitted when zero.
- Integration (`testutil.StartDaemonWithTools`):
  - `echo/short` reaches done, the patch carries `ended_at`, and `meta.json` has it.
  - `echo/long` plus `Complete` gives `ended_at` set.
- Unit, TUI:
  - `TestCompletedAt` table: marked then exit, exit then mark, legacy mark, older daemon fallback, live.
  - `TestClockStamp`: fixed UTC `now`.
  - `TestApplyPatch_EndedAt`.
  - `layoutSessions` (`board_layout_test.go:18`) gains StartedAt and EndedAt; `TestBoardLayout_RowsFillWidthExactly` covers it across `layoutWidths`.
  - New `TestBoardLayout_SpanDropsBeforeTokens`.
- Snapshot: extend `TestBoardSnapshot` with a detailed-view case showing the span.
- CLI: `output_test.go` `WriteSessionsTableWide` test, checking NAME is still last.

**Docs, CHANGELOG, schema**
- `docs/protocol.md` SessionSummary fields: `ended_at` and its patch null semantics.
- `docs/agents/data/schemas.md`: add EndedAt. Fix the stale "drives the board done badge" text on RetiredAt.
- `docs/tui.md`:
  - ROW COLUMNS: span column and the narrow order (remove "done badge is hidden").
  - MARK DONE: remove the done-badge sentence at :163.
- `docs/cli.md` `rex ls`: `-wide`.
- CHANGELOG `[Unreleased]`: Added (timestamps). Fixed docs (done badge).

**Conflict map**
- F1 does not touch `rowCols`, `layoutRows` or `renderRow` (board.go:335-542). No conflict there.
- T3 adds its `resume` column in the same three spots in the same commit series. Do T2 first and T3 rebases (same author ideally).
- S1–S3 (desc column) may change `descMin` or `rowDescription`. Steps-slice edits are adjacent, so expect a trivial merge.
- U1/U2 restyle colors. New cells use the existing `colorFgDim`.
- P1–P3 (CLI hardening) may touch `ls.go` and `output.go`. Coordinate the `-wide` flag name.
- T5 search: the haystack does not need timestamps.

**Security notes:** none new. Times are rendered through `cell()`/`truncate`, and no new input path exists.

**Open questions**
1. Show any timestamp in the simple view? Default: no (you removed the done chip in 397a8a6). Option: Done rows show the completion clock time instead of the age.
2. Span format `14:02→15:30` (one column) vs two columns `start` and `end`? Default: one span column (one degrade step, reads naturally).
3. Should `rex stats` and `rex digest` switch their end time from `last_event_at` to `ended_at`? Default: yes, as a follow-up commit (it fixes done_regex drift), not in this task.

**Ordered steps (each one commit, `make check` green)**
1. `feat(state): record ended_at when a session reaches a terminal state`: protocol field, session/store/persist changes, state and integration tests, protocol.md and schemas.md.
2. `feat(tui): start→end span column in the detailed view`: board_times.go, board.go layout and render, applyPatch, tests, tui.md (including the stale done-badge fix).
3. `feat(cli): rex ls -wide shows started/ended`: output.go, ls.go, cli.md, CHANGELOG.

**Size:** M
