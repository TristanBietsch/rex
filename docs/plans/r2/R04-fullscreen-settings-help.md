# R04 Settings and help as full-screen pages (ANTI delta)

Status: design revised 2026-11-11 against `integration/todo` after round 1 (U1, U2, U4, U5 merged). Revision notes at the end.

## Summary
- Input: `docs/plans/ANTI-fullscreen-settings-help-spec.md` (ANTI). Most of ANTI's goals already shipped in round 1:
  - U1: semantic palette, new `default` theme, help table from the command table, scrolling, `/` filter, legend, settings search, section jump, detail pane, modified marker, inline string edit, goldens.
  - U2: `theme` setting, user YAML themes, live reload.
  - U4: window border; U5: fixed-width columns and `layoutClass`.
  - Audio rows `soundset` and `master_volume` already exist (`internal/catalog/settings/registry.go`).
- R04 does only what is left: turn the two **overlays** into **full-screen pages** that look like the board (same header logo, dividers, hint footer, window border), add the ANTI themes that U1 does not ship, and leave room for R05 (Lua) and R06 (workspace scope).

## Why
- The overlays are capped at 56–88 columns over a dimmed board. Long help text and setting descriptions get clipped; the board behind them is noise.
- Pages should read as part of the app, not a popup on top of it.

## Goals / non-goals / anti-goals
- Goals: both pages own the whole layout area; header, divider and footer match the board; two-pane body; every setting and help row reachable at 80×24; same keys as today where they do not clash.
- Non-goals: new settings rows (owned by U4, U5, R06, R05); Lua config (R05); workspace scope (R06); mouse click targets (wheel only, as today).
- Anti-goals: a page that draws over the attach PTY; a theme change that races the UI goroutine; losing any overlay feature (search, inline edit, `?` expand, `r` reset, help filter, legend).

## Current behavior
- `tui.go` `view()`: `FocusHelp` and `FocusSettings` draw `centerOverlayBox(…, dimBoard(renderFullScreen(…)))`.
- Settings (`settings.go`, `settings_view.go`): one list grouped by section, detail pane under it, `/` search, `tab`/`shift+tab` jump section, `h`/`l` adjust, `enter` toggle or edit, `r` reset, `?` expand help. Mouse: wheel moves cursor (`board_mouse.go:updateSettingsMouse`).
- Help (`help.go`): groups from the bindings/commands tables plus a live legend, 1 or 2 columns, `j`/`k` scroll, `/` filter, rows of off features dimmed. Mouse: wheel scrolls.
- Window border (U4) wraps whatever `view()` returns (`View` → `wrapFrame`), so a page gets the border for free.
- Themes are Go structs in `internal/catalog/theme/builtin.go` (`classic`, `default`, `noir`, `paper`, `high-contrast`, `ansi`); user themes are YAML files (U2).

## Design

### Mode
- Keep `FocusHelp` and `FocusSettings` (D4). `view()` picks `renderSettingsPage` / `renderHelpPage` when `fullscreen_pages` is on, the existing overlay calls when off. Key and mouse routing stay keyed on the same foci. Board is not drawn behind a page.

### Chrome (shared, `page.go`)
```
  ∴ REX › SETTINGS       Appearance › Theme              config.yaml
  ────────────────────────────────────────────────────────────────────
  <body>
  ────────────────────────────────────────────────────────────────────
  esc back · tab pane · j/k move · enter toggle · / search · 1-8 section
```
- Header: `styleLogo` `∴ REX` (same as `renderHeader`), then `› SETTINGS` or `› HELP`; middle: breadcrumb (section › row) or the active search/filter text; right: config source (`config.yaml` now; R05/R06 extend it). Help's right slot shows `rex <version>`.
- Dividers use `colorBorder`, full content width, same side padding as the board (`boardPad`).
- Footer: `renderHints` / `wrapHints`, context-sensitive (search mode, edit mode, filter mode as today).

### Body: two-pane inspector (D1)
```
  SECTIONS        │ Appearance
  ▸ Appearance  3 │ ▸ Theme                  ◂ default ▸
    Audio         │   Spinner                  braille
    Behavior      │ • Window border            on
    Features      │   ...
    ...           │
  ────────────────┴──────────────────────────────────────────────────
  id theme · default default · live
  default  classic  noir  paper  high-contrast  ansi  dracula …
  Color theme. Applies live; user themes come from ~/.config/rex/themes.
```
- Settings: left pane lists sections (count of modified rows per section as a dim badge); right pane lists rows of the selected section (or, while a search is active, every matching row grouped by section, as today); inspector at the bottom reuses `settingsDetail` unchanged.
- Help: left pane lists groups (Navigation, Filter, Actions, Mouse, Modes, Legend, Session commands, App commands, Attached); right pane shows the selected group's rows via `renderGroup`. While a `/` filter is active the right pane shows every matching row across groups (left pane highlights groups with hits). No inspector for help; the right pane takes the full height.
- Left pane width: longest section/group title + badge, capped at 24 cols.

### Keys
- Open: `S` / `:settings`, `?` / `:help` (unchanged). Close: `esc` (clears search/filter first, as today), `q`, and `?` on help.
- `tab` / `shift+tab`: switch pane focus (D5). Section jump moves to `1`–`9` (settings and help) and `[` / `]` (prev/next section).
- Left pane focused: `j`/`k` move section; `enter`/`l` focuses right pane.
- Right pane focused (settings): every current key keeps its meaning (`j`/`k`, `h`/`l`/`+`/`-`, `enter`, `r`, `?`, `/`). `h` on a non-adjustable row does nothing (does not jump to the left pane; avoids accidental value changes vs. navigation ambiguity).
- Right pane focused (help): `j`/`k` scroll, `pgup`/`pgdown`, `g`/`G`.
- Mouse: wheel only, same behavior as today, applied to the focused pane.

### Size classes
- ≥ 60 cols: two panes side by side.
- < 60 cols: single column; left pane collapses to a one-line section tab strip (`‹ Appearance ›`) under the header; same keys (`tab` is a no-op, `[`/`]` and `1`–`9` switch section).
- Height: header 3 rows (blank row, title row, divider: the same blank row the board has above its header, so the logo does not move between board and page) + footer 2 rows; settings inspector keeps U1's fixed height (4 help lines, 2 under 30 rows). Lists window around the cursor with `↑ more` / `↓ more` markers (existing `windowLines`).
- 80×24 must show every setting and help row reachable without horizontal clipping (golden).

### Themes (D2)
- Add `dracula`, `tokyo_night`, `pantone_safe` as embedded YAML files in `internal/catalog/theme/builtin/*.yaml` (`go:embed`), parsed by the U2 user-theme parser and merged into the built-in list. Each passes `TestThemeContrast`. `pantone_safe` reuses U4's colorblind-safe swatches. User files with the same name still override (existing U2 precedence). The bundled themes live in a `Bundled` map separate from the Go built-ins, so `extends:` and `rex theme new -from` accept only the original six built-ins.

### Attached session
- `::settings` / `::help` while attached: keep today's behavior: detach, then open the page. Never draw a page over the PTY. (D8)

## Feature flag
- `fullscreen_pages` (default on), Settings → Features. Off = today's overlays, unchanged (overlay renderers stay in the tree).
- Off-is-inert test: with the flag off, `view()` output for both foci equals the pre-R04 golden; no new keys (`1`–`9`, `[`, `]`, pane focus) change state.

## Plan
1. **R04-1 Flag + page frame.** `fullscreen_pages` registry row; `page.go`: `renderPageHeader`, `renderPageFooter`, `pageBodySize(m)`; `view()` branches on the flag. Settings and help bodies are the current single-list renderers placed inside the frame (no panes yet). Tests: flag off golden unchanged; goldens at 120×40, 80×24, 50×20 for both pages; `esc` returns to board with selection unchanged; resize while open.
2. **R04-2 Settings two-pane.** `SettingsState.Pane` (`paneList`/`paneDetail` naming per repo style), section list with modified badges, right list scoped to the section, search spans all sections, inspector reused. Key changes (`tab` = pane, `1`–`9`, `[`/`]`). Tests: pane focus cycle; section jump; search across sections then `esc` restores section; inline edit and `r` still work; narrow tab-strip layout.
3. **R04-3 Help two-pane.** `HelpState.Group`, `HelpState.Pane`; group list left, `renderGroup` right; filter spans groups; legend group keeps live spinner. Tests: every group reachable; filter with zero hits shows the empty-state line; off-feature rows still dimmed.
4. **R04-4 Themes.** Embedded YAML for the three themes. Tests: each loads, passes contrast, appears in the theme picker, user override wins.
5. **R04-5 Attach test + docs.** Pin `::settings` / `::help` detach-then-open with the flag on; `docs/tui.md`, `docs/settings.md`, CHANGELOG `[Unreleased]`; mark ANTI spec superseded by U1/U2/R04/R05/R06.

## Review focus
- Terminal resized while a page is open: panes and inspector reflow, no stale rows.
- Theme change from the page applies on the UI goroutine (U1 fixed the race; keep it fixed). The page itself re-renders in the new theme immediately.
- Zero-hit search/filter shows an empty-state line, not a blank pane.
- Window border (U4) wraps the page exactly as it wraps the board.
- Flag off: byte-identical to pre-R04 overlays.

## Decisions

| # | Question | Answer |
|---|---|---|
| D1 | Page layout | Two-pane inspector: categories left, rows right, inspector box bottom. (2026-11-11, user) |
| D2 | Extra themes | dracula, tokyo_night, pantone_safe as embedded YAML. (2026-11-11, user) |
| D3 | Audio rows | Already shipped (`soundset`, `master_volume`); nothing to do. Step dropped. |
| D4 | New `FocusPage` vs keep foci | Keep `FocusHelp` / `FocusSettings`; swap only the renderer. Less churn in routing, `focusStateMissing`, mouse and tests. (agent call) |
| D5 | `tab` clash | `tab` switches panes; section jump moves to `1`–`9` and `[`/`]`. (agent call) |
| D6 | Mouse | Wheel only, as today; click targets out of scope. (agent call) |
| D8 | `::settings` / `::help` while attached | Detach, then open (today's behavior); no refusal notice. (2026-11-11, user) |
| D9 | Page header height | 3 rows with the board's blank top row so the logo lines up with the board. (2026-11-11, user) |
| D10 | `:usage` | Stays a floating overlay; full-screen usage is out of R04. (2026-11-11, user) |
| D7 | Overlay code | Kept behind the flag; R04-1's "overlays removed" dropped (contradicted the flag). (agent call) |

## Decision: page navigation (2026-11-12)

2026-11-12 (user): "the new fullscreens are nice, but the vim movement is broken. remake it so that it is more fluid and natural", "esp for the new agent screen". One vim-style model, shared by settings, help and the R16 new-agent page through one navigator (`internal/surface/tui/nav.go`). Supersedes the Keys section above where they differ (h no-op, `[`/`]` wrap, the wheel on the focused pane).

Defects found (each has a failing-first test in `page_nav_test.go`):

| # | Page | Defect | Test |
|---|---|---|---|
| N1 | settings | `j`/`k` stop at a section's last/first row | `TestPageNav_SettingsJKCrossSections` |
| N2 | settings | `h` is a no-op and `l` changes the value: no key goes from the rows to the sections | `TestPageNav_SettingsHLMovePanes` |
| N3 | settings | the sections pane, `[`/`]` and `1`–`9` always land on a section's first row: the place is lost | `TestPageNav_SettingsRemembersRowPerSection` |
| N4 | settings | no `gg`/`G`, `ctrl+d`/`ctrl+u`, `ctrl+f`/`ctrl+b` | `TestPageNav_SettingsTopBottomAndPages` |
| N5 | settings, help | `[`/`]` wrap while `j`/`k` clamp | `TestPageNav_SettingsBracketsClamp`, `TestPageNav_HelpHLAndBrackets` |
| N6 | settings, R16 lists | the window recenters on the cursor (`windowLines`) instead of a scroll margin | `TestPageNav_SettingsScrollOff`, `TestNav_ScrollKeepsContextAndMovesOneRow` |
| N7 | settings, help | the wheel acts on the focused pane, not the one under the pointer | `TestPageNav_SettingsWheelUnderPointer` |
| N8 | help | the `↑ more` marker overwrites a row: the first `j` hides two rows | `TestPageNav_HelpJScrollsOneRowNoneHidden`, `TestNav_WindowMarkersHideNoRow` |
| N9 | help | `ctrl+d` scrolls a whole page; a single `g` jumps to the top | `TestPageNav_HelpHalfPageAndGg` |
| N10 | help | `j` stops at a group's end | `TestPageNav_HelpJCrossesGroups` |
| N11 | help | `h` does nothing; the wheel scrolls 3 rows, `j` 1 | `TestPageNav_HelpHLAndBrackets`, `TestPageNav_HelpWheelIsOneRow` |
| N12 | new agent | `esc` in the task editor closes the page; no way to leave the editor and move with `j`/`k` | `TestPageNav_NAEscLeavesTheEditorFirst`, `TestPageNav_NAIAndEnterEdit` |
| N13 | new agent | `j`/`k` do nothing on Agent and Trust, and stop at the last context item | `TestPageNav_NAJKCrossSections` |
| N14 | new agent | the step rail cannot take the focus | `TestPageNav_NARailFocus` |
| N15 | new agent | context items: no `d`, no `e` outside the preview | `TestPageNav_NAContextKeys` |
| N16 | new agent | no launch row; trust only by `ctrl+t`/`ctrl+a` | `TestPageNav_NAEnterOnLaunchRow`, `TestPageNav_NATrustCycles` |
| N17 | new agent | the wheel does nothing on the page; narrow `h`/`l` do nothing | `TestPageNav_NAWheel`, `TestPageNav_NANarrowHL` |
| N18 | new agent, Agent screen | `h`/`l` change the effort, not the column; `j` stops at the last model; effort has no focus | `TestPageNav_NAAgentHLColumns`, `TestPageNav_NAAgentJIntoEffort` |
| N19 | new agent, Agent screen | 200 models: no `gg`/`G`/`ctrl+d`, centered window | `TestPageNav_NAAgentManyModels` |

The model (all three pages):

| Key | Does |
|---|---|
| `j` `k`, `↓` `↑` | one row in the focused pane; the rows pane goes on into the next/previous section (the section list follows); clamps, never wraps |
| `h` `l` | sections pane / rows pane (narrow: previous / next section) |
| `←` `→` | settings: step the row's value where it has one (the existing contract), else `←` = `h`; R16 Agent screen: the effort row's value, else as `h`/`l` |
| `gg` / `G`, `home` / `end` | first / last row |
| `ctrl+d` / `ctrl+u` | half a window |
| `ctrl+f` / `ctrl+b`, `pgdn` / `pgup` | a whole window |
| `tab` / `shift+tab` | switch panes (kept, D5); R16 Compose: next / previous step (wraps, as R16) |
| `[` / `]`, `1`–`9` | previous / next section (clamps), section N; each section keeps the row it was left on |
| `/` | search: keys type; `enter` keeps it and `j`/`k` move through the hits; `esc` clears |
| wheel | one row, as `j`/`k`, on the pane under the pointer |

- Scrolling: a scrolloff of 2 rows, one row per step; the `↑ more`/`↓ more` markers take rows of their own.
- Counts (`5j`): not supported; the digits are section jumps on every page.
- Board keys: unchanged. The overlays (`fullscreen_pages` off): unchanged.
- R16 Compose keeps two modes: **editing** (keys type; `enter` launches per R16 D2, `ctrl+j`/`alt+enter` newline; `esc` leaves the editor; `tab` leaves it for the next step) and **navigating** (`j`/`k` walk agent, task, each context item, trust, launch; `h` focuses the rail; `i`/`a`/`A`/`enter` on the task edit; `esc`/`q` close, keeping the draft). D2 does not conflict: `enter` launches only while editing. The one change to R16's table: `esc` in the editor no longer closes the page (it is the innermost thing now). A Launch step joins the rail (`5`), so `enter` launches from it.
- R16 Agent screen: columns are agents | models, effort, skills; `h`/`l` between columns, `j`/`k` within and across models → effort → skills; effort is its own focus with `←`/`→` (`+`/`-` from any pane); `tab` cycles all four.

## Revision notes (2026-11-11)
- Old R04-4 (audio rows) removed: rows exist.
- Old "Overlays removed" in R04-1 contradicted "flag off = overlays unchanged"; overlays now stay.
- `FocusPage` replaced by D4.
- `tab` key conflict with U1 section jump resolved by D5.
- Added narrow-layout tab strip, attach guard as its own step, help empty state.
- 2026-11-11 (user): attach refusal replaced by detach-then-open (D8); header gains the board's blank row (D9); `:usage` out of scope (D10); bundled-theme `extends:`/`-from` limit written down.
