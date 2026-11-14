# R18 task plan: row icon strip

> **For agentic workers:** REQUIRED SUB-SKILL: use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to work through this plan task by task. Steps use checkboxes (`- [ ]`) for tracking.

**Goal:** Remove the row text column from the board and put a fixed-width icon strip (unseen, why, loop, subagents, spend, long-run) in its place; the `↳` preview line becomes the one place a row's words show. Behind `row_icons` (Features, default on); off is today's board, byte for byte.

**Architecture:** One additive wire field (`cost_usd`) fed by the usage hub through a new `Store.SetCost`. Everything else is TUI: a pure `row_icons.go` (glyphs, slot cells, formats), a strip-aware branch in `layoutFixed`/`layoutFit`/`renderRow`, a small unseen tracker on `Model` saved in `tui-state.json`, and a preview lead change. The off path calls today's functions unchanged.

**Tech stack:** Go 1.26, bubbletea v1.3, lipgloss, `charmbracelet/x/ansi`, `golang.org/x/text/width` (already indirect; promote to direct for the glyph test), `internal/testutil/golden` (txtar, `-update`).

**Spec:** [R18-row-icons.md](R18-row-icons.md). Read it first. Its D1–D12 recommendations are assumed; re-check the Decisions table for the user's answers before each task.

**Out of scope:** phase 2 (R18-2: `↯` failed tool calls, `◔` context used; `◔` replaced `⧗` on 2026-11-12, see the spec's Decisions). No task here builds it.

**Start condition:** the user has answered D1–D12. If R17 (`fix/ready-subagent-flap`) has merged, rebase first: it touches `events.go` next to `Subagents` and `model.go` `applyPatch`.

Code on the base this plan depends on:
- `internal/surface/tui/board.go`: `renderRow` (`:654`), `rowCols` + `used()` (`:475-488`), `layoutFit` (`:512`), `subagentCell` (`:718`), `longRunCell` (`:733`), `rowText` (`:930`), `rowDescription` (`:919`), constants `colAgents`, `colRun`, `colGap`, `rowIndent`, `descMinNarrow`.
- `board_columns.go`: `classSpecs` (`:117`), `layoutFixed` (`:145`), `layoutOpts`, `layoutOptsOf`.
- `desc_preview.go`: `previewRows`, `previewSession`, `renderDescPreview`.
- `loops.go`: `loopChip`, `withLoopChip`, `loopStyle`, `loopByID`. `waiting.go`: `waitingKindOf`. `retire_view.go`: `longRunBadge`, `styleLongRun`.
- `help.go`: `helpLegend` (`:97`). `persist.go`: `persistedState`, `SaveTUIState`, `LoadTUIState`. `model.go`: `applyPatch` (`:434`), `EventSessionUpdated` (`:328`). `attach.go`: `openAttach` (`:121`). `retire_view.go`: `retireToggleCmd`.
- `internal/daemon/state/store.go`: `SetSubagents` (`:516`) as the model. `internal/features/usage/hub.go`: `HubConfig`, `Sample`, `reportLocked`, `recordLocked` (cost sum). `internal/daemon/boot/usage_hub.go`: `NewUsage`.

## Global constraints
- Flags/settings, exact: `row_icons` bool, Features, default **true**, `ApplyLive`; `row_icon_set` `TypeString` `ListSep: ","`, Appearance, default **`unseen,reason,loop,subagents,spend,longrun`**, unknown ids ignored; `ascii_glyphs` bool, Appearance, default **false**.
- Slot widths: unseen **1**, reason **8**, loop **8**, subagents **4** (`colAgents`), spend **6** right-aligned, long-run **5** (`colRun`); gap `colGap` (2) before each slot.
- Name widths with `row_icons` on: simple **32 / 24 / 20**, detailed **24 / 18 / 20** (wide / normal / narrow).
- Glyphs: `✦` unseen, `?` asks, `✓` review, `⟳` loop, `⋔` subagents, `$` spend, `▴` long-run. ASCII: `*`, `?`, `+`, `@`, `Y`, `$`, `^`.
- Spend format: `< $10` → `$0.42`; `< $100` → `$12.3`; `< $1000` → `$123`; else `$1.2k` (cap `$999k`). Colors: dim; **≥ $5** `styleLongRun[1]`; **≥ $20** `styleLongRun[0]`.
- Cost patch: only when the session total moved by **≥ $0.01**, at most once per **2 s** per session; called outside the hub lock.
- Column widths never read row content in `fixed`; every row exactly the content width; no wrap.
- `row_icons` off: every existing golden byte-identical; no `seen` key written.
- No color-only meaning; nothing in the strip animates.
- Public repo: synthetic sessions only in goldens and docs.
- Conventional Commits, signed (retry once, never bypass), one commit per task; CHANGELOG `[Unreleased]`; raise floors in `tools/coverage/baseline.txt`; `make check` green (lint lock: HANDOFF §2 workaround). Never `make install`, never touch real `~/.config/rex`.

## Review focus
1. **Lock re-entry: cost patch → store broadcast → usage subscriber → `hub.Observe`.** Calling `OnSessionCost` while holding `hub.mu` deadlocks. Test in Task 1 (`TestHub_OnSessionCostOutsideLock` with a callback that calls `hub.Observe`).
2. **Daemon older than `cost_usd`**: spend slot reserved and blank, never `$0.00`. Task 3 (`TestSpendCell_NilBlank`).
3. **Saved `seen` map with deleted session ids**: pruned on save, file does not grow; first start without the key floods nothing. Task 5 (`TestSeen_PrunedOnSave`, `TestSeen_FirstStartAllSeen`).
4. **`mobile_layout: off` at 45 and 61 columns**: squeeze order holds, every row exactly the width. Task 4 (`TestIconLayout_SqueezeOrder`, `TestIconLayout_RowWidthExact`).
5. **Screen-only adapter in Ready** (codex) with no `WaitReason` and a non-question last line: why slot blank, no panic on empty fields. Task 3 (`TestReasonCell_ScreenOnly`).

---

### Task 1: `cost_usd` on the wire

**Files:**
- Modify: `internal/wire/protocol/events.go` (after `Subagents`)
- Modify: `internal/daemon/state/store.go` (after `SetSubagents`), `internal/daemon/state/session.go` (`Summary`), `internal/daemon/state/persist.go` (`fromSummary`)
- Modify: `internal/features/usage/hub.go` (`HubConfig`, cost tracking), `internal/features/usage/hub_query.go` (`Sample`)
- Modify: `internal/daemon/boot/usage_hub.go` (`NewUsage`)
- Modify: `internal/surface/tui/model.go` (`applyPatch`)
- Modify: `docs/protocol.md` (field table)
- Test: `internal/daemon/state/cost_test.go`, `internal/features/usage/hub_cost_test.go`, `internal/surface/tui/model_cost_test.go`

**Interfaces:**
- Produces: `protocol.SessionSummary.CostUSD *float64 \`json:"cost_usd,omitempty"\``; `func (s *Store) SetCost(id string, usd float64) error` (patch key `cost_usd`, not activity); `HubConfig.OnSessionCost func(id string, usd float64)`; TUI `applyPatch` sets `CostUSD` from `patch["cost_usd"].(float64)` (`nil` clears).

- [ ] **Step 1: Write the failing tests**
  - `TestStore_SetCost_PatchesAndPersists`: `SetCost("a", 1.25)` broadcasts `{"cost_usd": 1.25}`; same value again broadcasts nothing; `LastEventAt` unchanged; after `fromSummary(Summary())` `*CostUSD == 1.25`.
  - `TestHub_OnSessionCostThrottle`: fake clock; samples with totals 0.10, 0.105, 0.12 within 1 s → one call (0.10); at +2 s with 0.12 → second call (0.12); a sample for an unknown session → no call; hub disabled → no call.
  - `TestHub_OnSessionCostOutsideLock`: callback calls `hub.Observe(sum)`; test finishes within 1 s.
  - `TestApplyPatch_CostUSD`: `{"cost_usd": 0.42}` sets it; `{"cost_usd": nil}` clears it.
- [ ] **Step 2: Run** `go test ./internal/daemon/state ./internal/features/usage ./internal/surface/tui -run 'Cost'` → FAIL (undefined).
- [ ] **Step 3: Implement.** The total is the same sum `recordLocked` builds (`seedCost` + `costs`); extract it as `func (l *liveSession) totalCost() float64` and use it in both. `Sample` collects `(id, total)` under the lock and calls `OnSessionCost` after unlock, like `OnUsage`. Per-session `lastCostSent float64`, `lastCostAt time.Time` on `liveSession`. `NewUsage` passes `OnSessionCost: func(id string, usd float64) { _ = store.SetCost(id, usd) }`.
- [ ] **Step 4: Run** the same command → PASS; `go test -race ./internal/features/usage ./internal/daemon/...` → PASS.
- [ ] **Step 5: Commit** `feat(usage): send each session's cost as cost_usd`

### Task 2: settings, glyphs and the glyph test

**Files:**
- Modify: `internal/catalog/settings/registry.go` (three rows; `row_icons` in Features after `summary_preview`; the two Appearance rows after `column_layout`)
- Create: `internal/surface/tui/row_icons.go`
- Test: `internal/surface/tui/row_icons_glyph_test.go`, `internal/catalog/settings` registry tests (existing feature-registry test picks up the row)

**Interfaces:**
- Produces:
  - `type iconSlot int` with `slotUnseen, slotReason, slotLoop, slotSubagents, slotSpend, slotLongRun` (this order is the layout order) and `func (s iconSlot) id() string` → `unseen`, `reason`, `loop`, `subagents`, `spend`, `longrun`.
  - `var slotWidth = [...]int{1, 8, 8, colAgents, 6, colRun}`.
  - `func rowIconsOn(m Model) bool` (feature on and not mobile).
  - `func iconSlots(m Model) []iconSlot` — `row_icon_set` filtered, minus `slotLoop` when `loops` is off, minus `slotSpend` when `usage` is off; fixed order.
  - `func iconGlyph(m Model, name string) string` for names `unseen`, `asks`, `review`, `loop`, `subagents`, `longrun`; returns the ASCII form with `ascii_glyphs` on.

- [ ] **Step 1: Write the failing tests**
  - `TestRowIconGlyphs`: for every unicode and ASCII glyph from `iconGlyph`: `ansi.StringWidth == 1`; `width.LookupRune(r).Kind()` is `width.Neutral` or `width.EastAsianNarrow`; not in the set {state markers from `stateGlyph` for every state, `waitingFrames`, `queuedGlyph`, `selectDefault`, `▍`, `∴`, `λ`, `↳`, `»`, `✎`, `▰`, `▱`, every built-in registry tool `Icon`} — except `⋔`, `▴`, `⟳`, which must equal `subagentGlyph`, the long-run badge glyph and `loopDefault`.
  - `TestIconSlots_SetAndFlags`: default → six slots; `row_icon_set = "spend,unseen,bogus"` → `[slotUnseen, slotSpend]` (fixed order, bogus ignored); `loops` off drops `slotLoop`; `usage` off drops `slotSpend`.
  - `TestRowIconsSettings_Defaults`: `row_icons` true in `SectionFeatures`; `row_icon_set` default string exact; `ascii_glyphs` false.
- [ ] **Step 2: Run** `go test ./internal/surface/tui ./internal/catalog/settings -run 'RowIcon|IconSlots'` → FAIL.
- [ ] **Step 3: Implement** the three registry rows (Help text: what off does; `summary_preview` help gains "With row_icons on, this line is where a row's task and summary show.") and `row_icons.go`. Move `golang.org/x/text` to a direct require (`go mod tidy`).
- [ ] **Step 4: Run** the same → PASS.
- [ ] **Step 5: Commit** `feat(tui): row icon settings and glyph set`

### Task 3: slot cells

**Files:**
- Modify: `internal/surface/tui/row_icons.go`
- Test: `internal/surface/tui/row_icons_cells_test.go`

**Interfaces:**
- Consumes: Task 2 names; `waitingKindOf`, `loopByID`, `loopStyle`, `longRunCell`, `subagentCell`.
- Produces: `func slotCell(m Model, s protocol.SessionSummary, slot iconSlot, w int, selected bool) string` (exactly `w` cells, styled; `w` may be smaller than `slotWidth` only for `slotReason` at 1 — glyph only); helpers `reasonText(m Model, s protocol.SessionSummary) (text string, style lipgloss.Style)`, `spendText(usd float64) string`, `spendStyle(usd float64) lipgloss.Style`, `loopSlotText(m Model, s protocol.SessionSummary) string`.

- [ ] **Step 1: Write the failing tests** (plain profile, `ansi.Strip`)
  - `TestReasonCell`: needs_input + `WaitReason=input` → `? asks`; `review` → `✓ review`; reason empty + `LastLine="Proceed?"` → `? asks`; working + `WaitingOn=shell`, `WaitingSince=now-42s`, `waiting_section` on → `sh 42s`; `web` → `web …`; `subagent` → `sub …`; `waiting_section` off → blank; marked done → blank; width 1 → `?` / `✓` / blank.
  - `TestReasonCell_ScreenOnly`: codex, needs_input, no reason, `LastLine="Working…"` → 8 spaces.
  - `TestSpendText` (round to the target precision first, then pick the format): 0.42→`$0.42`, 9.999→`$10.0`, 12.34→`$12.3`, 123.4→`$123`, 1234→`$1.2k`, 2e6→`$999k`; right-aligned to 6.
  - `TestSpendCell_NilBlank`: `CostUSD == nil` → 6 spaces; `0` → `$0.00`.
  - `TestSpendStyle`: 4.99 dim, 5 → `styleLongRun[1]`, 20 → `styleLongRun[0]`.
  - `TestLoopSlotText`: running iteration 3 of 8 → `⟳ 3/8`; paused → `⟳ paused`; stopped → `⟳ stop`; unknown loop id → `⟳ 3`.
  - `TestSlotCell_ASCII`: `ascii_glyphs` on → `* `/`+ review`/`@ 3/8`/`Y 3`/`^ 45m`.
- [ ] **Step 2: Run** `go test ./internal/surface/tui -run 'ReasonCell|Spend|LoopSlot|SlotCell'` → FAIL.
- [ ] **Step 3: Implement.** `slotSubagents` and `slotLongRun` reuse `subagentCell` / `longRunCell` with the ASCII glyph swapped in when `ascii_glyphs` is on. `slotUnseen` takes its value from Task 5's `isUnseen`; until then a stub `func isUnseen(m Model, s protocol.SessionSummary) bool { return false }` lives in `row_icons.go` and Task 5 replaces it.
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `feat(tui): row icon slot cells`

### Task 4: layout and row rendering

**Files:**
- Modify: `internal/surface/tui/board.go` (`rowCols`, `used`, `layoutFit`, `renderRow`, `renderBoard`), `internal/surface/tui/board_columns.go` (`layoutOpts`, `layoutOptsOf`, `layoutFixed`, a second spec table)
- Test: `internal/surface/tui/board_icons_test.go`

**Interfaces:**
- Consumes: `iconSlots`, `slotWidth`, `slotCell`.
- Produces: `layoutOpts.icons []iconSlot` (nil = off path); `rowCols.slots [6]int` (width per `iconSlot`, 0 = absent) and `rowCols.spacer int` (replaces `desc` on the icons path; `desc` stays 0 there); `var iconClassSpecs = [...]classSpec{…}` with the name widths from Global constraints; `func renderRowIcons(m Model, s protocol.SessionSummary, c rowCols) string`. `renderRow` dispatches on `len(c.slots) > 0 && rowIconsOn(m)`; the old body is untouched.

- [ ] **Step 1: Write the failing tests**
  - `TestIconLayout_ClassWidths`: at 80, 120, 150 × simple/detailed, with `loops` on: name = 20/24/32 and 20/18/24; slot widths equal `slotWidth`; `used()+spacer == width`.
  - `TestIconLayout_RowWidthExact`: every width 40..200, both views, `column_layout` fixed and fit, `mobile_layout: off` → every rendered row `ansi.StringWidth == width`.
  - `TestIconLayout_StableOffsets`: same width, two boards with different sessions (long names, `Subagents` 0 vs 12, cost nil vs 1234) → identical byte offset of each slot in `ansi.Strip` rows.
  - `TestIconLayout_SqueezeOrder`: `mobile_layout: off`; shrink from 61 to 40: spacer hits 0 first, then spend, long-run, loop, subagents vanish in that order, then reason width becomes 1, then name shrinks to 12, unseen last.
  - `TestIconLayout_FitHidesEmptySlots`: `column_layout: fit`, no session has a cost → no spend slot and no gap.
  - `TestIconLayout_NoTextOnRow`: a session with `Title="ship the auth fix"` and `Description="rewrote the token refresh"` → neither string appears in `renderBoard` output.
  - `TestIconLayout_OffIsOldPath`: `row_icons` off → `renderBoard` equals the output with `layoutOpts.icons = nil` and contains the title.
- [ ] **Step 2: Run** `go test ./internal/surface/tui -run IconLayout` → FAIL.
- [ ] **Step 3: Implement.** `layoutFixed`/`layoutFit` branch on `o.icons != nil`; detail columns are the same `colSet` values except `slug`. Grid on the icons path: `indent marker [id] name {gap slot}… spacer {gap detail}… gap age`. Squeeze runs only when `spacer` would be negative. In `fit`, a slot exists while some row's `slotCell` text is non-blank (`unseen` and `reason` included). Rows still go through `blinkRow`.
- [ ] **Step 4: Run** → PASS. Then set `row_icons=false` in `layoutModel` (the fixture every existing golden and layout test builds on; the new default is on) and run `go test ./internal/surface/tui -run Golden` with no `-update` → PASS with zero diff. That is the byte-identical-off proof.
- [ ] **Step 5: Commit** `feat(tui): icon strip replaces the row text column`

### Task 5: unseen tracking

**Files:**
- Modify: `internal/surface/tui/row_icons.go` (replace the `isUnseen` stub), `internal/surface/tui/model.go` (`Model` fields; `EventSessionAdded`, `EventSessionUpdated`, `EventSnapshot`), `internal/surface/tui/attach.go` (`openAttach`), `internal/surface/tui/retire_view.go` (where `cc` sends the mark), `internal/surface/tui/persist.go`
- Test: `internal/surface/tui/row_icons_seen_test.go`

**Interfaces:**
- Produces: `Model.EnteredAt map[string]time.Time` (when the TUI saw the row enter Ready or Done; not saved); `Model.SeenAt map[string]time.Time` (saved); `func markSeen(m *Model, id string)`; `func isUnseen(m Model, s protocol.SessionSummary) bool`; `persistedState.Seen map[string]time.Time \`json:"seen,omitempty"\``.

- [ ] **Step 1: Write the failing tests**
  - `TestSeen_ReadyAfterAttachIsUnseen`: attach at t0 (`markSeen`), patch `state: needs_input` at t1 > t0 → unseen; `markSeen` at t2 → seen.
  - `TestSeen_CCClears`: unseen Done row, `cc` → seen (and marked-done rows never show `✦`).
  - `TestSeen_WorkingNeverUnseen`: working/waiting rows → false whatever the times.
  - `TestSeen_FirstStartAllSeen`: snapshot with no saved `seen` → every row seen; a later transition → unseen.
  - `TestSeen_PrunedOnSave`: `SeenAt` holds ids `a`, `gone`; sessions hold `a` → saved file has only `a`.
  - `TestSeen_OffWritesNothing`: `row_icons` off → saved JSON has no `seen` key and `EnteredAt` stays empty.
- [ ] **Step 2: Run** `go test ./internal/surface/tui -run Seen` → FAIL.
- [ ] **Step 3: Implement.** A section change is computed with the board's own grouping (`partitionSessions` / `groupOf`) before and after `applyPatch`; entering `groupNeeds` or `groupDone` stamps `EnteredAt[id] = m.now()`. On the first `EventSnapshot` with no loaded `Seen`, set `SeenAt[id] = m.now()` for every session.
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `feat(tui): mark rows that finished since you last looked`

### Task 6: the `↳` line carries the words

**Files:**
- Modify: `internal/surface/tui/desc_preview.go`
- Test: `internal/surface/tui/desc_preview_icons_test.go`

**Interfaces:**
- Produces: `func previewLeadText(s protocol.SessionSummary) (text string, isSummary bool)` — question (needs input, not marked done, `LastLine` ends in `?`) → `Description` → `"task: " + Title`; `""` when none.

- [ ] **Step 1: Write the failing tests**
  - `TestPreview_IconsLeadOrder`: question beats summary beats task; age suffix only for the summary.
  - `TestPreview_IconsBothViews`: detailed view, summary fits on nothing (no text column) → line shows.
  - `TestPreview_IconsReservedForTitleOnly`: no session has a description, one has a title → `previewRows == 1`.
  - `TestPreview_OffUnchanged`: `row_icons` off → existing `desc_preview_test.go` expectations hold (run the file).
- [ ] **Step 2: Run** `go test ./internal/surface/tui -run Preview` → FAIL.
- [ ] **Step 3: Implement**: `previewRows` gates on `anyLead` when `rowIconsOn`; `previewSession` skips the "row shows all of it" rule on that path; `renderDescPreview` uses `previewLeadText`.
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `feat(tui): preview line shows the selected row's question, summary or task`

### Task 7: mobile, legend, goldens, docs

**Files:**
- Modify: `internal/surface/tui/board_mobile.go` (line 1 adds unseen and the 1-cell why glyph), `internal/surface/tui/help.go` (`helpLegend`), `internal/surface/tui/golden_test.go` (new screens; per-screen sizes incl. 150×40), `internal/surface/tui/board_layout_test.go` (golden board sessions: `WaitReason`, `WaitingOn`, `CostUSD`, a loop, pinned `SeenAt`/`EnteredAt`)
- Create: `testdata/golden/board_icons_simple.txtar`, `board_icons_detailed.txtar`, `board_icons_ascii.txtar`, `board_icons_loops.txtar` (via `-update`)
- Modify: `docs/tui.md` (ROW COLUMNS, Column widths, Mobile layout, Legend), `docs/settings.md` (three rows), `CHANGELOG.md` `[Unreleased]`, `docs/agents/modules/` TUI module doc if it lists row columns, `tools/coverage/baseline.txt`

**Interfaces:**
- Consumes: everything above.

- [ ] **Step 1: Write the failing tests**
  - `TestMobile_IconsLine1`: 50 columns, unseen Ready row with `WaitReason=input` → line 1 contains `✦` and `?`; line 2 still holds the task.
  - `TestLegend_RowIcons`: legend lists `✦`, `? asks`, `✓ review`, `sh 42s`; `⟳ 3/8` only with `loops` on; `$0.42` only with `usage` on; ASCII forms with `ascii_glyphs` on; none of them with `row_icons` off.
  - Golden screens registered (fail until `-update`).
- [ ] **Step 2: Run** `go test ./internal/surface/tui -run 'Mobile_Icons|Legend_RowIcons|Golden'` → FAIL.
- [ ] **Step 3: Implement**, then `go test ./internal/surface/tui -run Golden -update`; read every new txtar section by eye against spec §11 (slot order, blanks, spend on Claude rows only, no row text).
- [ ] **Step 4: Run** `make check` → green. Existing goldens: zero diff.
- [ ] **Step 5: Commit** `feat(tui): row icons on mobile, legend, goldens and docs`
