# U7 Board selector: glyph, room, weight, blink (`selector_*` settings)

## Summary
- The selector is the arrow left of the selected board row (the agent picker). Today it is a fixed `▸` in a 3-column gutter, and only a theme file can change it.
- Five new Appearance settings make it editable from the settings page, `rex config set` and `config.lua`:
  - `selector_glyph`: 5 Unicode presets (one is a right-pointing finger, `☞`), `theme`, or any glyph you type.
  - `selector_gap`: blank columns between the selector and the state marker. The default goes from 1 to 2 ("a little more room").
  - `selector_bold`: draws the selector bold. On by default ("a tiny bit bigger").
  - `selector_blink`: `off` / `blink` / `pulse`.
  - `selector_blink_ms`: the blink period.
- One glyph everywhere: the 9 hardcoded `▸` in the wizards, settings page, pages and new-agent flow now call `selectGlyph()`. Gap and blink apply to the board only.

## Current behavior (file:line)
- Glyph: `selectDefault = "▸"` and `selectGlyph()` (`internal/surface/tui/glyphs.go:13-14,59-65`). The only override is the theme's `Glyphs.Select` (`internal/catalog/theme/spec.go:34`).
- Gutter: `rowIndent = 3` (`board.go:464`), built as `" " + markerPrefix + " "` in `rowLead` (`board.go:597-619`). It is read by the column budget (`board.go:493`, `board_columns.go:310`), the empty-board line (`board.go:407`) and the mobile second line (`board_mobile.go:22`).
- Non-selected rows put a fleet bar `▍` or a blank space in the same cell (`board.go:604-609`).
- Hardcoded `▸ ` that bypasses `selectGlyph()`:
  - `page.go:178,180`
  - `newagent_view.go:191,193,411,548`
  - `settings_view.go:304`
  - `newagent_import.go:577`
- Blink machinery already exists:
  - the 100 ms `SpinnerTick` chain (`events.go:62`), gated by `wantsSpinnerTick` (`events.go:84-95`);
  - the text cursor blinks on `SpinnerTick%10 < 5` and holds steady under `reduce_motion` (`prompt.go:67-78`).
- Closest precedent for the glyph setting is `prompt_glyph`:
  - presets plus free entry (`registry.go:58-63`);
  - validated by `normalizePromptGlyph`, which allows one grapheme 1–2 cells wide (`internal/catalog/settings/prompt_glyph.go:18-42`).
- `WorkspaceAllowIDs` lists the display settings a workspace may set (`trust_ids.go:24-33`). New IDs are denied until they are listed there.

## Design

**1. `selector_glyph`** (TypeString, Free, ApplyLive)
- Options: `theme`, `▸`, `❯`, `➜`, `➤`, `☞`. Default `theme`, which means the theme's `Glyphs.Select`, else `▸`, so no one's look changes until they choose.
- Four of the presets avoid East Asian *ambiguous* width, matching the rule in `glyphs.go`. `▶ ● ◆ »` are ambiguous and are left out.
- The finger is `☞` (U+261E, user decision D1). It is ambiguous width: 1 cell in default macOS terminal setups, but 2 cells in a terminal set to treat ambiguous characters as wide, where the selected row's columns would shift by one. This trade-off is accepted, and the setting's Help text names it. `👉` stays available as free entry for anyone who wants a 2-cell finger.
- `normalizeSelectorGlyph` accepts `theme`, else calls `normalizePromptGlyph`.
- `ascii_glyphs` on, or an ASCII color profile, falls back to `>` unless the glyph is already ASCII.

**2. Gutter / room** (`selector_gap`, TypeInt, Min 1 Max 3, default 2)
- `rowIndent` const becomes `rowIndent()`, which reads a package var `curSelector` (same idiom as `curGlyphs`, written on the UI goroutine at settings apply):
  `indent = 1 (lead) + slotW + gap`, where `slotW = ansi.StringWidth(glyph)` (1 or 2).
- `rowLead` pads the fleet bar and the blank to `slotW`, so every row keeps the same indent and the header columns stay aligned.
- Default gutter: 3 → 4 columns (5 with `👉`). The desc column gives up the column through the existing `shrink` steps.

**3. Weight** (`selector_bold`, TypeBool, default true)
- `styleArrow.Bold(...)` on the board marker and every `selectGlyph()` call site.
- A terminal cannot scale one glyph, so "bigger" comes from bold plus the 2-cell glyph option. Double-height lines (DECDHL) were considered and rejected: they double the whole line, tmux drops them, and they break row math.

**4. Blink** (`selector_blink` enum `off|blink|pulse`, default `off`; `selector_blink_ms` TypeInt Min 200 Max 2000 default 600)
- Phase comes from `SpinnerTick`, so the period is rounded to 100 ms.
- `blink`: the off phase renders `slotW` spaces, so the layout does not shift.
- `pulse`: alternates the accent color with `styleDim`. It is calmer and is never invisible.
- The phase counts from `Model.lastInput` (the screensaver's idle clock), so any key or click, j/k included, leaves the selector solid, like a text cursor. No per-move hook is needed at the 22 selection writes.
- It holds solid under `reduce_motion`, while the screensaver shows, while focus is off the board, and after 60 s with no input. That last rule lets the tick chain rest on an idle board, so there is no constant 10 Hz wakeup.
- `wantsSpinnerTick` gains `|| m.selectorBlinking(now)`.

**5. Settings page**
- New rows sit in Appearance under `prompt_glyph`. The glyph row previews the glyph, with arrows to cycle presets and enter for free entry (the `prompt_glyph` flow, `settings_view.go:136`).
- All 5 IDs are added to `WorkspaceAllowIDs` (display-only).

## Files to change
- `internal/catalog/settings/registry.go`: 5 entries.
- `internal/catalog/settings/selector_glyph.go`: new normalizer.
- `internal/catalog/settings/trust_ids.go`: allow list.
- `internal/surface/tui/glyphs.go`: `curSelector`, `selectGlyph()` precedence, `selectSlotW()`.
- `internal/surface/tui/board.go`, `board_columns.go`, `board_mobile.go`: `rowIndent()`, padding, blink render.
- `internal/surface/tui/events.go`: `selectorBlinking`, tick gate.
- `page.go`, `newagent_view.go`, `settings_view.go`, `newagent_import.go`: route through `selectGlyph()`.
- The settings apply hook that writes `curGlyphs`: also write `curSelector`.
- Docs: `docs/settings.md`, `docs/themes.md` (precedence), `docs/tui.md`, `CHANGELOG.md`, `TODO.md`.

## Test plan (test first)
- Normalizer accepts `theme`, `☞` and free-entry `👉`. It rejects `""`, `ab`, space and a control character.
- Every preset is 1–2 cells wide. Every preset except the allow-listed `☞` must not be ambiguous width (table test using `uniseg`/`runewidth` EAW).
- `selectGlyph` precedence: the setting beats the theme, `theme` falls back to `theme.Glyphs.Select` then `▸`, and `ascii_glyphs` gives `>`.
- `rowLead` width equals `rowIndent()` for each combination of {`▸`, `👉`} × gap {1,2,3} × {selected, fleet, plain}.
- Header and row columns align: board goldens for default, `👉` gap 3, and mobile.
- Blink, with `SpinnerTick` pinned: solid on tick 0 after a move; `blink` blanks at half period and keeps width; `pulse` swaps style; solid under `reduce_motion`, off-board focus and 60 s idle.
- `wantsSpinnerTick`: false on an idle board with blink off; true with blink on and recent input; false after idle.
- Workspace allow list test covers the new IDs.
- The 9 former hardcoded call sites render the configured glyph (wizard and settings goldens).

## Conflict map
- The working tree on `integration/todo` has uncommitted edits to `newagent_launch_test.go` and `snapshot_test.go`. Branch off after those land, or rebase goldens later.
- Default gap 1 → 2 changes every board golden. Regenerate them in one isolated commit (step 3) so review stays readable.

## Decisions (user, 2026-11-13)
- D1 Finger preset is `☞`, accepting the ambiguous-width risk. `👉` is free entry only.
- D2 Default `selector_gap` is 2. Board goldens are regenerated once.
- D3 The glyph and bold apply to all pickers. Gap and blink apply to the board only.

## Ordered steps (each one commit, `make check` green)
1. Settings: registry entries, `normalizeSelectorGlyph`, allow list, tests. (~45 min)
2. Glyph plumbing: `curSelector`, `selectGlyph` precedence, ASCII fallback, replace 9 hardcoded `▸`, goldens for wizards and settings. (~45 min)
3. Gutter: `rowIndent()`, slot padding, columns, mobile, board goldens regenerated. (~1.5 h)
4. Bold weight. (~15 min)
5. Blink: phase, move reset, idle stop, tick gate, tests. (~1.5 h)
6. Settings-page preview, docs, CHANGELOG, TODO tick. (~45 min)

## Size estimate
About 5–6 h. Roughly 250 lines of code plus 300 lines of tests, with golden churn on top.

### Critical Files for Implementation
- `internal/surface/tui/board.go`
- `internal/surface/tui/glyphs.go`
- `internal/surface/tui/events.go`
- `internal/catalog/settings/registry.go`

## Implementation notes
- `curSelector` is loaded from the Store by `syncSelector` at the top of `View`, on the UI goroutine. Pickers without a `Model` read it from there.
- A picker cursor cell is always 2 columns (`selectCursor`): the glyph plus a space, or a 2-cell glyph alone. The board slot is the glyph's own width.
- `ascii_glyphs` also turns a non-ASCII selector into `>`. The help text says so.
