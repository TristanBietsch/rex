# U5 Fixed-width columns, responsive breakpoints, mobile layout

## Summary
- Board row columns get content-independent widths: the name, model, tokens, span and resume columns stop jumping when a long name or the first subagent appears. The description is the only flexible column. `column_layout: fit` brings back today's content-sized columns.
- Width changes go through four breakpoints (`wide ≥150`, `normal 110–149`, `narrow 61–109`, `mobile ≤60`). Each drops whole columns as a group instead of shrinking them one step at a time. U4, R04, S3 and T1–T3 all read `layoutClass(m)`.
- Mobile (`mobile_layout: auto|always|off`, for phone SSH) shows two-line rows and a compact header and help line. Overlays and the attach popup never get wider than the screen.

## Current behavior (file:line)
- `layoutRows` (`board.go:395-451`) sizes name, model and tokens to the widest value (capped at 22/24/8, `:405-420`). The run and agents columns exist only while some row needs them (`:411-416`). Then a 6-step degrade chain runs (`:432-445`) until the description has 20 columns, then shrinks the description to 8 and the name (`:446-449`).
  - Result: any row change (a long name, a subagent, a token count) or any resize shifts every column.
  - `rowCols` / `used()`: `:362-377`. Constants: `:344-360`. `renderRow`: `:460-542`.
- Planned additions: T2 adds `span` (13) and T3 `resume` (8), both as new first degrade steps.
- One line per row: `boardSlots` (`board.go:234-258`) is the single source for render (`:301-333`), scroll (`keymap.go:186-198`) and mouse (`board_mouse.go:139-153`).
- **Narrow terminals:**
  - `contentWidth` only special-cases < 40 (`tui.go:117-123`).
  - The header top row is `∴ REX` plus verbose counts, about 45 columns (`header.go:14-31`).
  - The help line has 5 keys (`prompt.go:93-103`).
  - U1 panels are clamp(w-8, 56, 96), which overflows below 58 columns.
  - The attach popup floors at `cols = 40` (`attach.go:209-211`), so below 54 terminal columns it is wider than the screen and Bubble Tea clips its right side.
- The docs describe the degrade order (`docs/tui.md:43-48`). R04 already expects U5 to own breakpoints and `layoutClass(m)` (`docs/plans/r2/R04-fullscreen-settings-help.md:19`).

## Design
**1. Meaning.** "Fixed width, not resizeable" means the width of a column never depends on row content, and only changes in discrete steps at breakpoints (Q1). "Responsive" means the class is recomputed from `m.Width` on every frame; `WindowSizeMsg` already re-lays out and calls `ensureVisible`.

**2. `layoutClass(m Model) layoutClass`** (new `board_columns.go`).
- It reads `cw = contentWidth(m.Width)`. After U4, `m.Width` is the inner width.
- Classes: `classWide ≥150`, `classNormal 110–149`, `classNarrow 61–109`, `classMobile ≤60`.
- `mobile_layout` overrides it: `always` forces mobile, `off` never uses mobile.
- R04 and U4 call it.

**3. Fixed column table** (`column_layout: fixed`, the default). Widths are constant per class. The description takes the rest.

| class | simple view | detailed view (`v`) | description width at the class's lower bound (detailed) |
|---|---|---|---|
| wide ≥150 | name 22 · ⋔3 · ▴4 · age 4 | + id 4 · model 24 · tokens 8 · span 13 (T2) · resume 8 (T3) | 36 |
| normal 110–149 | name 20 · ⋔ · ▴ · age | + id · model 16 · tokens 8 (no span, no resume) | 31 |
| narrow 61–109 | name 16 · ⋔ · ▴ · age | same columns as simple; the description switches to the AI summary | 20 |
| mobile ≤60 | two-line rows (§5) | the same, with line 2 showing the description | — |

- The sometimes-empty columns (⋔, ▴, tokens, span, resume) are always reserved in the classes that show them (Q2), so they never shift.
- Invariants kept: every row is exactly `cw` cells (`TestBoardLayout_RowsFillWidthExactly`). Only if `cw` drops below the class floor (impossible except with `mobile_layout: off`) is the name shrunk, as today.

**4. `fit` mode** (`column_layout: fit`). Today's `layoutRows` is renamed `layoutFit` and keeps T2/T3's degrade steps. Mobile still applies, because it is controlled by `mobile_layout`.

**5. Mobile layout.**
- Row shape:
  ```
   ▸ ◆  release-notes            ⋔2 ▴42m  11m
        Which feature should lead: search…
  ```
  - Line 1: indent, marker, name (flexible), ⋔/▴ when present (dropped below 30 columns), age.
  - Line 2: 6-column indent, then the text (simple view: `rowText`; detailed view: `rowDescription`) through S3's `truncateWords`, dim.
- `boardSlots` adds a `slotRowCont` after each `slotRow`.
  - `sessionAtScreenRow` accepts both kinds.
  - `selectedBoardLine` returns line 1.
  - `ensureVisible` keeps `sel + rowLines(m) - 1` on screen.
- **Header:**
  - The top row always uses the glyph counts (`◆4 ⠋2 ●7`) and drops `· detailed (v)`.
  - The chip row, S4's overview line and the backend banner are cut by `padLine`; the banner gets a short form.
- **Help line:** 3 keys (`enter open · i new · ? help`).
- **Prompt:** unchanged.
- **S3 preview line:** `previewRows(m) = 0` (line 2 already shows the text).

**6. Overlays fit** (all classes):
- Panels use `min(panelWidth, w-2)`. U1 must keep this clamp.
- The attach popup:
  - floors at `min(40, w-6)`;
  - in mobile, becomes a sheet: no outer margin and no padding, so `cols = w-2` and rows lose the margin rows.

  This goes through one `attachChrome(m)` struct, which extends S5's chrome-rows value with margins and padding. `attachPopupInnerSize`, `renderAttach`/`overlayBox` and `attachInnerOrigin` all read it, so mouse origin cannot drift.

**7. Settings** (Appearance, after `header_style`, before U4's rows):
- `column_layout` (enum `fixed|fit`, default `fixed`).
- `mobile_layout` (enum `auto|always|off`, default `auto`).
- No per-column width knobs (Q5).

## Files to change
- New: `internal/surface/tui/board_columns.go` (`layoutClass`, column specs, `layoutFixed`, `layoutFit`, `rowLines`), `board_columns_test.go`, `board_mobile.go` (`renderRowStacked`), `board_mobile_test.go`.
- Modified:
  - `board.go`: `rowCols`, `layoutSlots`, `boardSlots`, `renderBoard`, `renderRow` (cells unchanged).
  - `keymap.go`: `ensureVisible`, `selectedBoardLine`.
  - `board_mouse.go`: `sessionAtScreenRow`.
  - `header.go`: mobile top row.
  - `prompt.go`: help line.
  - `desc_preview.go` (S3): `previewRows`.
  - `panel.go`, `help.go`, `settings.go`: width clamp.
  - `attach.go`, `attach_mouse.go`, `tui.go`: `attachChrome`, sheet mode.
  - `internal/catalog/settings/registry.go`: 2 rows.
- `docs/tui.md` (ROW COLUMNS rewrite), `docs/settings.md`, CHANGELOG.

## Test plan (test-first)
- **Classes and widths:**
  - `TestLayoutClassBreakpoints`: 60/61, 109/110, 149/150, plus `mobile_layout` always/off.
  - `TestFixedColumnsIgnoreContent`: boards with short and very long names, with and without tokens or subagents, give identical `rowCols` (minus `desc`) at each width.
  - `TestFixedColumnsReservedSometimesEmpty`.
  - `TestFitModeUnchanged`: `fit` output is byte-identical to the pre-change baseline at all `layoutWidths`.
  - Extend `layoutWidths` to {30, 40, 45, 60, 61, 80, 109, 110, 149, 150, 220} for both modes and both views: every line is exactly the width and the time column is flush right.
- **Mobile:**
  - `TestMobileTwoLineRows`.
  - `TestMobileClickSecondLineSelects`.
  - `TestMobileEnsureVisibleKeepsBothLines`.
  - `TestMobileHeaderHelpFit40x20`: no View line wider than 40.
  - `TestMobilePreviewLineOff`.
  - `TestResizeAcrossBreakpointKeepsSelection`.
- **Overflow:**
  - `TestOverlaysNeverWiderThanScreen` for w in 30..60 (help, settings, wizard, stats, attach). This fails today for attach.
  - `TestAttachInnerOrigin_MatchesRenderedPopup` also in sheet mode.
- **T2/T3/S3 interplay:** the span and resume columns are present only in `wide` detailed. `truncateWords` is used on line 2.
- **Goldens** (U1 harness if present, otherwise `snapshot_test.go`): board simple and detailed at 40×20, 80×24, 120×40, 160×40 in `fixed`, and one in `fit`.
- **Manual check:** `rex render -w 40 -h 20` and a narrow tmux pane through `dev/sandbox.sh`.

## Docs/CHANGELOG
- `docs/tui.md` ROW COLUMNS: fixed widths per breakpoint (table), which columns each class shows, `fit` mode, mobile rows, sheet attach. Remove the degrade-order paragraph (or move it under `fit`).
- `docs/settings.md`: `column_layout`, `mobile_layout`.
- CHANGELOG:
  - Changed: "Board columns have fixed widths by default and change only at breakpoints (`column_layout: fit` restores content-sized columns)."
  - Added: "Mobile layout for terminals ≤ 60 columns."
  - Fixed: "Attach popup and overlays no longer overflow narrow terminals."

## Conflict map and placement
- **T2-2 (span), T3-6 (resume), S3-5/6 (`truncateWords`, preview line), T1 (chips, hidden sections), S4-6 (overview line):** U5 rewrites the layout code they extend. Land U5 **after** all of them, so their columns slot into the table as rows instead of degrade steps. Their degrade steps survive inside `layoutFit`.
- **S5:** `attachChrome` generalizes S5-1's chrome-rows value. U5's attach step must follow S5-1. Same lane B wave 5, or U5-5 rebases.
- **U1 (wave 6):** U1's golden baseline (U1-1) then captures the final layout. U1's panel clamp keeps `min(…, w-2)`. U1 restyles `board_columns.go` and `board_mobile.go` mechanically.
- **U4:** the frame shrinks `m.Width` first, so classes see the inner width.
- **R04:** consumes `layoutClass`. **R09** (dynamic sections) is unaffected: columns are per row.
- **Placement:** wave 5, **lane A**, after P2-1..6. U5-5 (attach chrome) runs after S5-1 merges in lane B the same wave.

## Security notes
No new input path. Every cell still goes through `cell()` / `truncate` / `truncateWords` (sanitize, then cut), and line 2 uses the same sanitizers (`FlattenDisplayText`), so agent text cannot add lines or escape codes. The settings are enums validated by the registry.

## Open questions for user
1. **What should "fixed width, not resizeable" mean?**
   - (a) Widths never depend on row content, only on breakpoints; the description is the only flexible column. **[recommended]**
   - (b) Content-sized as today, but frozen after the first layout until the window resizes.
   - (c) Columns the user resizes by dragging (off by default), with that drag feature built too.
2. **In fixed mode, the sometimes-empty columns (⋔ subagents, ▴ long-run, tokens, span, resume):**
   - (a) Always reserved in the breakpoints that show them, so nothing shifts. **[recommended]**
   - (b) Shown only while some row uses them (today's rule).
   - (c) Never reserved; folded into the age cell as a suffix.
3. **When does the mobile layout turn on?**
   - (a) Automatically at ≤ 60 columns (`mobile_layout: auto|always|off`). **[recommended]**
   - (b) Automatically at ≤ 60 columns or ≤ 16 rows.
   - (c) Manually only (`mobile_layout: on`).
4. **Mobile row shape?**
   - (a) Two lines: name + age, then the description. **[recommended]**
   - (b) One line; the description only in S3's preview line.
   - (c) Three-line cards (name/state, description, model · age).
5. **Width knobs?**
   - (a) None, only `column_layout` + `mobile_layout`. **[recommended]**
   - (b) Add `name_column_width` (12–40).
   - (c) A per-column width map (YAML only).
6. **Attach popup on mobile?**
   - (a) A full-screen sheet (no margin or padding) at ≤ 60 columns. **[recommended]**
   - (b) Keep the popup, only clamp it to the screen.
   - (c) Suspend the TUI and run CLI-style passthrough `rex attach`.

## Ordered steps (each one commit; `make check` green)
1. `refactor(tui): column layout behind layoutClass and a column spec table`
   - `layoutFit` = today's code, with the `layoutClass` plumbing.
   - Goldens byte-identical.
2. `feat(tui): fixed-width columns with breakpoints (column_layout)`
   - Fixed table, setting, tests.
   - tui.md ROW COLUMNS, settings.md, CHANGELOG Changed.
3. `feat(tui): mobile board layout (mobile_layout)`
   - `slotRowCont`, stacked rows, hit-test and scroll, header and help-line compaction, preview off.
   - Tests, goldens, docs.
4. `fix(tui): overlays fit narrow terminals`: panel clamp; tests.
5. `feat(tui): attach sheet on mobile and popup clamped to the screen` (after S5-1)
   - `attachChrome`, origin tests.
   - Docs, CHANGELOG Fixed.

## Size estimate
**M+.** 5 commits, about 800–1000 lines including tests and goldens.
