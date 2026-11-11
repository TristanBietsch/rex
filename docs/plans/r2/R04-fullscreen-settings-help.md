# R04 Settings and help as full-screen pages (ANTI delta)

## Summary
- Input: `docs/plans/ANTI-fullscreen-settings-help-spec.md` (ANTI). Most of ANTI's goals are already planned in round 1:
  - U1: semantic palette, new `default` theme, help table from the command table, scrolling, `/` filter, legend, settings search, section jump, detail pane, modified marker, inline string edit, goldens.
  - U2: `theme` setting, user YAML themes, live reload.
  - U4: window border; U5: fixed-width columns.
- R04 does only what is left: turn the two **overlays** into **full-screen pages** with ANTI's header/footer chrome, add the ANTI themes that U1 does not ship, and leave room for R05 (Lua) and R06 (workspace scope).

## Current behavior (after round 1)
- Help and settings are centered overlays (`centerOverlayBox`, `tui.go`) over a dimmed board, bounded to 56–88 columns.
- U1 gives them content parity with ANTI §4–5 but keeps the overlay frame.
- Themes after U1/U2: `default` (violet `#B392F0`), `classic`, `high-contrast`, `ansi`, plus user YAML files.

## Design
- New `FocusPage` mode with `pageKind ∈ {settings, help}`. The page owns the whole screen: header row (`∴ REX › SETTINGS`, breadcrumb, right-aligned config source), divider, body, footer hints. Board is not drawn.
- Body layout per Decision D1 (ANTI §6 options 1–3).
- Keys: `S` / `:settings` and `?` / `:help` open the page; `esc` / `q` back to board; `tab` cycles panes; `/` filter (U1 filter reused); `1–9` jump to section.
- Narrow terminals (< 60 cols): single column, panes stacked, same keys. U5 owns breakpoints; R04 reads `layoutClass(m)` from U5.
- Themes: add `dracula`, `tokyo_night`, `pantone_safe` as **built-in YAML theme files** (U2 format) embedded with `go:embed`, so they ship as data, not code. Each passes U1's contrast test. `pantone_safe` reuses U4's colorblind-safe swatch list (Decision D2).
- ANTI's new settings rows that other plans own are not added here: border (U4), fixed columns (U5), workspace scope + id (R06), soundset/volume (Decision D3).
- "Config source" slot in the header shows `config.yaml` now; R05 and R06 extend it to `config.lua` / `.rex/config.lua`.

## Plan
1. **R04-1 Page frame.** `page.go`: `renderPage(m, kind, w, h)`, header/footer helpers, `FocusPage` routing in `update.go`. Overlays removed; `centerOverlayBox` stays for wizard/stats/fail. Tests: golden at 120×40 and 60×20 for both pages; `esc` returns to board with selection unchanged.
2. **R04-2 Body layout (D1).** Option 1: `settingsPanes{cats, list, inspector}` with focus index; help reuses the same two-pane frame (groups left, rows right). Tests: `tab` focus cycle; filter narrows both panes; inspector shows id/default/options for the selected row.
3. **R04-3 Themes.** Embedded YAML for the three ANTI themes. Tests: each theme loads, passes `TestThemeContrast` (U1), appears in the theme picker.
4. **R04-4 Audio rows (if D3 = a).** `soundset` enum backed by existing `features/audio` packs only; `master_volume` 0–1 step 0.1. Tests: registry contiguity test (round-1 rule), volume clamps.
5. **R04-5 Docs.** `docs/tui.md`, `docs/settings.md`, CHANGELOG; remove "Proposed" from ANTI spec and mark it superseded by U1/U2/R04/R05/R06.

## Review focus
- Terminal resized while a page is open: no stale overlay rows, inspector reflows.
- `:settings` while attached (`::settings`): page opens on detach or is refused with a notice, never draws over the PTY.
- Theme change from the page applies on the UI goroutine (U1 fixed the race; keep it fixed).
- Filter with zero hits shows an empty-state line, not a blank page.
- 80×24 default terminal: every setting reachable without horizontal clipping.

## Decisions

| # | Question | Options (recommended first) |
|---|---|---|
| D1 | Page layout (ANTI §6) | (a) Two-pane inspector: categories left, rows right, inspector box bottom (lazygit-like; matches U1's detail pane) · (b) Tabbed studio: tab bar + card grid, toggle pills, sliders · (c) Visual list + live `config.lua` buffer (needs R05 first) |
| D2 | Extra themes | (a) add dracula, tokyo_night, pantone_safe as embedded YAML · (b) only pantone_safe (colorblind-safe), rest as user-theme examples in docs · (c) none, U1's four are enough |
| D3 | ANTI audio rows (soundset, master volume) | (a) volume now, soundset only if more than one pack exists · (b) both now, add packs · (c) skip |
