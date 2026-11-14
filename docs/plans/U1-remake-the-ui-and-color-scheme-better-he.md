# U1 Remake the UI and color scheme; better help menu; remake help and settings pages

## Summary
- Commit 28cfe51 and PR #11 already redid the help and settings layouts. U1 finishes the job: one semantic palette in a new shared package `internal/catalog/theme`, used by both the TUI and the CLI, and a new default color scheme that passes contrast tests. The old look stays available as `classic`.
- U1 fixes three styling bugs found while reading the code. `styleArrow`/`styleArrowYellow` are never colored because of Go's init order. The theme is applied from a `tea.Cmd` goroutine, which is a data race with `View`. The prompt cursor disappears under `NO_COLOR`.
- Help gets a table-driven key/command source shared with `:` dispatch, scrolling, a `/` filter and a live legend. Settings gets search, section jumps, a detail pane (id, default, options, when it applies), a "modified" marker and inline text editing. Visual changes are pinned by new golden snapshots, regenerated on purpose in a few named commits.

## Current behavior (file:line)
**What 28cfe51 and PR #11 already did (not repeated here):**
- Grouped two-column help at 96+ columns (`help.go:27-95`, `:150-171`).
- Square, high-contrast `panelStyle` panels (`panel.go:12-22`) for help and settings only.
- A windowed settings list with "n of N", a fixed 2-line help area and wrapped footer hints (`settings.go:435-660`).
- Sections reordered, and the simple board with a five-key help line (`prompt.go:93-103`).

**Gaps that remain:**
1. **Init-order bug.** `prompt.go:11-14` builds `styleArrow = …Foreground(colorWorking)` as a package-level var. Go initializes package vars before `init()`, and `applyTheme` (`styles.go:111-113`) only runs in `init()`, so `colorWorking` is still `""`. `lipgloss.Color("")` means no color (termenv `profile.go:85`). As a result the λ glyph, the board/wizard/settings `▸` marker and the `:` prompt have never been colored, and they never follow the theme.
2. **Data race.** `stepThemeApply` (`splash_steps.go:110-122`) calls `applyTheme` inside `emit` (`:21-33`). That is a `tea.Cmd`, which runs on its own goroutine, and it rewrites globals that the splash `View` is reading at the same moment.
3. **Hard-coded colors.**
   - The cursor uses `#0F1115` and is drawn with background only (`prompt.go:57-66`). Under `NO_COLOR` (lipgloss uses `EnvColorProfile`, which falls back to Ascii) the cursor is invisible.
   - `fleetPalette` (`board.go:152-171`).
   - About 35 inline `lipgloss.NewStyle()` calls per frame: `board.go:52, 470-512, 554-565, 607-626`, `retire_view.go:122-131`, `attach.go:472-560`, `attach_scroll.go:251-264`, `settings.go:587, 621`, `tui.go:239`, `fail.go:165`.
   - The CLI copies the classic hex values and several colors outside the palette (#4CAF50, #4CAF82, #F0A04B, #E05C5C, #5FC96E, #FACC15, #F87171): `cli/core/help.go:11-18`, `inspect/digest.go:24-42`, `inspect/stats.go:219-224`, `setup/setup.go:99-104`, `setup/update.go:23-27`, `setup/doctor.go:67-70`, `setup/uninstall.go:21-22`. The CLI ignores `color_scheme` completely, so `paper` users get light-on-light `rex --help`.
4. **Palette is not semantic.** The struct (`styles.go:12-19`) has no accent, warn, success or danger tokens. `Working` blue doubles as the accent, `colorBgModal` is unused, and `Crashed` is the same value as `FgDim`.
5. **Two chromes.** Help and settings use a square white border (`panel.go`). Wizard, stats, fail and attach use a rounded dim border (`tui.go:228-251`). There is also no background dimming, unlike the mockup (`dev/mockup.html` `.board-dim`).
6. **Help gaps.**
   - It is static data that drifts from `command.go:27-110` and `update.go`. T1, T3, T4 and T5 each have to remember to add help rows.
   - It cannot scroll. Height overflow drops groups or hard-clips (`help.go:173-205`), and `update.go:456-464` only handles `esc`/`?`.
   - The "Long-running badges" group describes colors in words ("amber") instead of showing them.
   - There is no marker legend (◆ ◌ ● ✕ ○ ▍ ⋔).
7. **Settings gaps.**
   - No search and no section jump across about 30 rows.
   - No marker for values changed from the default, and no default or options shown.
   - Help is cut to 2 lines, and `auto_trust_dirs` help is long.
   - Free-form strings (`auto_trust_dirs`, a custom `prompt_glyph`) cannot be edited, because `enter` does nothing for `TypeString` without options (`settings.go:173-199`).
   - Dead rows: `keybind_preset` has a single option (`registry.go:59`), and `lua_config_path` says "Plan E" (`registry.go:178`).
   - There is no hint about which changes need a daemon restart (`summary_enabled`, `summary_model`).
8. **No golden snapshots.** `snapshot_test.go` only checks substrings and prints with `-v`, so a restyle has no visual regression guard.

## Design
**1. Package `internal/catalog/theme` (new, shared by TUI and CLI).**
```go
type Palette struct {
  Name string; Dark bool
  Bg, Fg, FgDim, FgMuted          string // Bg = assumed terminal bg (never painted; caret/contrast only)
  BgSel, Border, BorderFocus      string
  Accent                          string // λ, ▸, focused input, logo, :commands in help
  Working, Queued, Needs, Done, Failed, Crashed string
  Warn, Danger, Success, Info     string // feedback; default to Needs/Failed/Done/Working
  Fleet                           []string
  SelReverse                      bool   // ansi theme: selection = reverse video
}
var Builtin map[string]Palette; func Names() []string
func Contrast(fg, bg string) float64        // WCAG 2.x
```
- Values are strings: `#RRGGBB`, an ANSI index `"0"`–`"255"`, or `""` for the terminal default. This works with `lipgloss.Color` as is, so no type churn.
- `internal/surface/tui/styles.go` keeps its existing var names (`colorFgDim`, `styleDim`, …) so sibling code still compiles, and fills them from a `theme.Palette`.
- New vars: `colorAccent`, `colorWarn`, `colorDanger`, `colorSuccess`, `styleKey` (bold Fg key caps), `styleLogo` (bold Accent), `styleCmd` (bold Accent), `styleMarker[state]`, `styleLongRun[tier]`, `styleFleet[i]`, `styleAlert`.
- Every inline `NewStyle()` above becomes one of these prebuilt styles. That also cuts per-frame allocations.
- `styleArrow` (Accent) and `styleArrowYellow` (Warn) move into `rebuildStyles()`.
- `cursorBlock` becomes `Reverse(true)`, so it survives Ascii/NO_COLOR.

**2. Token mapping (every current style).**

| Current | Token |
|---|---|
| colorFgPrimary, stylePrimary, styleSlug, styleSectionTitle, styleChipActive | Fg (bold where it already was) |
| colorFgDim, styleDim, styleHeaderMeta, styleChipDim, long-run 10m | FgDim |
| colorFgMuted, styleMuted, styleChipSep, styleBootSkip, styleBootBracket | FgMuted |
| colorBorder, styleBorderFg, renderHR, attach frame | Border |
| colorBorderStrong (panelStyle) | BorderFocus |
| colorBgElev, styleSelected, selected marker bg | BgSel (or Reverse when SelReverse) |
| colorBgModal | removed (unused) |
| styleHeaderApp | split: `styleLogo` (Accent) for "∴ REX"; `styleKey` (bold Fg) for key caps in the help line, group titles and help |
| styleArrow (λ, ▸ markers, wizard cursor) | Accent |
| styleArrowYellow (`:` prompt, confirm) | Warn |
| wizInputBox / setupInput border (colorWorking) | Accent |
| colorWorking, queued ◌, subagent ⋔ | Working / Queued |
| colorNeeds, styleAlert, attach_scroll banner | Needs |
| colorDone, done blink, styleBootOK/Ready | Done / Success |
| colorFailed, styleStateFailed, ✕, styleBootFail/Cause, long-run 2h and 6h (reverse) | Failed / Danger |
| long-run 30m, styleBootWarn | Warn |
| styleBootRun | Info |
| colorCrashed ○ | Crashed |
| fleetPalette | Fleet[] |
| cursor `#0F1115` | Reverse (no color) |
| Tool brand glyph colors (registry `color`) | unchanged (brand data) |
| CLI Help*/digest*/stats*/setup*/update*/doctor*/uninstall* | `core.Styles()` built once from the theme resolved through `settings.LoadDefault()`: Bold/Section=Fg, Dim=FgDim, Muted=FgMuted, Accent=Accent, HR=Border, OK=Success, Warn=Warn, Err=Danger |

**3. New color scheme.** The new palette replaces `default` (stored configs already hold `color_scheme: default`, so every user gets it). The old values ship as `classic`.

| token | default (new) | classic | noir | paper | high-contrast | ansi |
|---|---|---|---|---|---|---|
| Bg (assumed) | #0F1115 | #0F1115 | #05080F | #FBFAF7 | #000000 | "" |
| Fg | #E6E8EE | #E6E6E6 | #D8DDE6 | #1C1D21 | #FFFFFF | "" |
| FgDim | #8B91A1 | #7A7F8C | #7A8291 | #5D6270 | #C8C8C8 | "8" |
| FgMuted | #5A6070 | #4A4F5A | #434A59 | #9A9EA8 | #9A9A9A | "8" |
| Border | #2B303C | #262A36 | #1C212C | #D9D7D0 | #808080 | "8" |
| BorderFocus | #AEB4C2 | #FFFFFF | #9AA3B2 | #3A3D45 | #FFFFFF | "7" |
| BgSel | #1D212C | #171922 | #121722 | #EEECE6 | #303030 | reverse |
| Accent | #B392F0 | #5B8DEF | #9C8CF0 | #6E4BD8 | #00E5FF | "5" |
| Working | #6EA8FE | #5B8DEF | #7AA6F0 | #2B5CC0 | #4DA3FF | "4" |
| Needs/Warn | #F5B94A | #E5B341 | #D4B574 | #966200 | #FFD000 | "3" |
| Done/Success | #5BD08C | #4ADE80 | #5BC487 | #1F7A45 | #3CF07A | "2" |
| Failed/Danger | #F26B6B | #EF4444 | #D85A5A | #C23B3B | #FF5C5C | "1" |
| Crashed | #8B91A1 | #7A7F8C | #7A8291 | #6E7380 | #C8C8C8 | "8" |

- **Measured WCAG contrast vs Bg.**
  - default: Fg 15.4, FgDim 6.0, FgMuted 3.0, Accent 7.5, Working 7.8, Needs 10.7, Done 9.8, Danger 6.4, BorderFocus 9.1, Fg on BgSel 13.1.
  - paper: FgDim 5.8, Accent 5.5, Working 5.9, Needs 5.0, Done 5.1, Danger 5.1. The current paper Needs #B58800 is only 3.2, so it fails today.
  - classic still passes the rules below (FgDim 4.7, Danger 5.0).
- **Why violet accent:** blue, amber, green and red already mean states. A separate accent stops "the cursor is here" from reading as "working".
- `ansi` uses the terminal's own 16 colors, so users with Solarized or Gruvbox terminal themes get a matching rex, and it is the natural fallback on 16-color terminals.

**4. Unified chrome.**
- All modal overlays (help, settings, stats, fail, wizard) use one `panel(title, body, width)`: square border in BorderFocus, with the title set into the top border (`┌─ HELP ──…─ rex v1.x ┐`), which saves 2 rows.
- `overlayBox` (the attach popup, which is performance-sensitive) only swaps its glyphs to square corners and keeps its single-measure code.
- Non-attach overlays draw the board behind them in FgMuted: `ansi.Strip` plus `styleMuted`, done once per frame in `compositeOverlay` behind a `dim bool`. Attach is excluded, which keeps its frame-time budget.

**5. One table for keys and commands (`internal/surface/tui/keys.go`, new).**
```go
type binding struct{ Keys, Desc, Group string; Ctx ctxMask }      // board/attach/mouse
type command struct{ Names []string; Args, Desc, Group string; Run func(Model, []string) (Model, tea.Cmd) }
var bindings []binding; var commands []command
```
- `command.go`'s `switch` becomes a lookup in `commands`. The `Run` bodies move out unchanged.
- Help, the five-key help line and `:` error messages ("unknown command; try :help") all read these tables.
- `bindings` is descriptive only. Key handling stays in `update.go` and F1's `chords.go`, and a parity test checks that every `binding` key is handled.

**6. Help remake.**
- It renders from the tables in the same two/one-column layout. It no longer drops groups: the body scrolls (`j/k`, `pgup/pgdn`, `g/G`, wheel) with `windowLines`-style ↑/↓ markers.
- `/` opens an inline filter that keeps rows whose key or description matches. `esc` clears the filter first, then closes.
- The new "Legend" group draws the real markers (◆ needs you, ⠋ working, ◌ queued, ● done, ✕ failed, ○ crashed, ▍ fleet, ⋔3 subagents) and the four long-run badges in their real styles. This replaces the "dim / amber / red" words.
- Footer: `styleKey` key caps plus dim descriptions, the same format as the board help line.

**7. Settings remake.**
- Same list, with:
  - `/` live search over label, id, section and help;
  - `tab`/`shift+tab` to jump between sections;
  - a `•` before values that differ from their default;
  - on/off for bools, with "on" in Accent.
- The detail pane below the list replaces the fixed 2-line help:
  - the id (`spinner`, so CLI users learn `rex config set`) and the default;
  - for enums, an option strip with the current option highlighted;
  - when the change applies;
  - up to 4 help lines; `?` expands the full text.
- Registry gains `Hidden bool` and `Apply ApplyMode` (`live | next_attach | next_spawn | daemon_restart`).
  - `keybind_preset` becomes Hidden. It stays in the registry and in `rex config list`.
  - `lua_config_path` help loses "Plan E".
- `enter` on a free-form `TypeString` opens an inline editor. `lineEditor` is extracted from `rename.go`, which then reuses it.
  - Validation errors show in the detail pane.
  - `prompt_glyph` must be one printable grapheme of width 1–2.
  - `auto_trust_dirs` runs through `settings.SplitTrustDirs` and the same refusal rules as `rex setup`.
- Panel width is clamp(w-8, 56, 96).

**8. Snapshot strategy.**
- New `golden_test.go` with a `-update` flag. Goldens live in `internal/surface/tui/testdata/golden/*.golden`.
- Fixed `Model.Clock`, `SpinnerTick=0`, no `BlinkUntil`.
- Screens: board (simple and detailed), help, help filtered, settings, settings searching, wizard, stats, fail, at 80x24 and 120x40.
- Plain goldens: output passed through `ansi.Strip`, `lipgloss.SetColorProfile(termenv.Ascii)`.
- Two styled goldens (board, settings) under TrueColor, for color regressions.
- The baseline is committed first. Each later commit that changes visuals regenerates in the same commit and names the changed goldens in its body. Logic and refactor commits must leave goldens byte-identical, which is how we prove the token refactor is neutral.

## Files to change
- New:
  - `internal/catalog/theme/{theme.go,builtin.go,contrast.go}` plus tests;
  - `internal/surface/tui/{keys.go,golden_test.go,lineedit.go}`;
  - `internal/surface/tui/testdata/golden/*`.
- Modified (TUI):
  - `internal/surface/tui/styles.go` (consumes `theme.Palette`; all styles built in `rebuildStyles`);
  - `prompt.go` (remove var-init styles; cursor; help line from table);
  - `splash_steps.go` and `update.go` (`stepThemeApply` returns a `themeAppliedMsg` and `applyTheme` runs in `Update`);
  - `board.go`, `retire_view.go`, `attach.go`, `attach_scroll.go`, `fail.go`, `wizard.go`, `setup_wizard.go`, `stats_overlay.go`, `confirm.go` (prebuilt styles);
  - `panel.go` (titled panel);
  - `tui.go` (`compositeOverlay` dim; `overlayBox` corners);
  - `help.go` (rewrite);
  - `command.go` (table dispatch);
  - `settings.go` (rewrite of render and keys);
  - `rename.go` (uses `lineEditor`);
  - `update.go:456-464` (help keys).
- Modified (settings): `internal/catalog/settings/{types.go,registry.go}` (`Hidden`, `Apply`, `color_scheme` options, `lua_config_path` help).
- Modified (CLI): `internal/surface/cli/core/help.go` (+ `core/styles.go` new), `inspect/{digest,stats}.go`, `setup/{setup,update,doctor,uninstall,config}.go` (`config list` skips nothing; Hidden is a TUI concern only).
- `tools/coverage/baseline.txt`: add `internal/catalog/theme`.
- `dev/mockup.html` `:root`: new tokens.

## Test plan (test first)
- `TestStyleArrowFollowsTheme`: TrueColor profile; `styleArrow.Render("▸")` contains the Accent SGR; after `applyTheme("paper")` it contains paper's Accent. Fails today, because the arrow is uncolored.
- `TestThemeAppliedOnUpdateGoroutine`: `stepThemeApply(m)()` returns a msg and does not mutate globals (compare `colorFgPrimary` before and after); `Update(msg)` applies it. Run under `-race` with the splash `View` in a goroutine.
- `TestCursorVisibleUnderNoColor`: Ascii profile; `cursorBlock` contains `\x1b[7m`.
- `theme` package:
  - `TestBuiltinContrast`: every hex theme meets Fg ≥ 7.0; FgDim, Accent, Working, Needs, Done, Failed ≥ 4.5; BorderFocus ≥ 3.0; FgMuted ≥ 2.0 and below FgDim; Fg on BgSel ≥ 7.
  - `TestStateHuesDistinct`: CIE76 ΔE ≥ 20 between state colors, and after `termenv.ANSI256.Convert` the state colors map to distinct indices, as do Fg, FgDim and FgMuted.
  - `TestAnsiThemeIndices`.
  - `TestEveryBuiltinComplete` (no empty required token except in `ansi`).
- `TestNoHardcodedHex`: a go/ast walk of `internal/surface/**.go` (non-test) fails on any `lipgloss.Color("#…")` literal outside `internal/catalog/theme`.
- `TestEveryThemeRendersBoard`: for each built-in, `View()` at 120x40 under TrueColor does not panic, and the markers for working, needs, done and failed carry different SGR codes.
- Golden tests as in Design §8.
- Help:
  - `TestCommandTableParity`: every `commands` name dispatches, and `:nonsense` gives an error naming `:help`.
  - `TestHelpListsEveryCommand`: replaces the hand list in `help_test.go:47-55`.
  - `TestHelpScrolls`: 50x20; `j` ×N reaches the last group; nothing is dropped.
  - `TestHelpFilter`: `/` then "prune" leaves one row.
  - `TestHelpLegendUsesRealStyles`.
  - Keep `TestRenderHelpFits`.
- Settings:
  - `TestSettingsSearch` ("spin" leaves `spinner` only; `esc` restores).
  - `TestSettingsSectionJump`.
  - `TestModifiedMarker`.
  - `TestDetailShowsIDDefaultOptions`.
  - `TestHiddenSettingNotListed` (still exists in `settings.Find`).
  - `TestEditFreeString` (`auto_trust_dirs`: enter, type, enter, saved; `/` rejected with the message).
  - `TestPromptGlyphRejectsControlChars`.
- `rename_retire_test.go` still passes on top of `lineEditor`.
- CLI: `TestCLIStylesFollowTheme` (paper resolves to paper Fg in `core.Styles()`).

## Docs/CHANGELOG
- `docs/tui.md`: Help overlay (scroll, `/`, legend) and Settings (search, tab, detail pane, editing).
- `docs/settings.md` and `docs/howto/customize-appearance.md`: new theme list; `classic`; contrast note.
- `docs/agents/modules/{tui,settings}.md`: `keys.go`, theme package.
- `docs/STRUCTURE.md`: `catalog/theme` row.
- `dev/mockup.html` palette.
- CHANGELOG:
  - Changed: "New default color scheme; old look is `classic`. CLI output follows `color_scheme`."
  - Added: `high-contrast` and `ansi` themes; help scroll, filter and legend; settings search and inline editing.
  - Fixed: selection arrow and prompt glyph colors; cursor under NO_COLOR.

## Conflict map vs parallel tasks
- **U1 lands after everything else.**
- **T1, T3, T4, T5:** their `help.go` rows and `command.go` cases fold into the `commands`/`bindings` tables in step 7. The parity test catches any row that gets lost.
- **F1:** `chords.go` stays the handler. `bindings` lists `cc`, `dd`, `uc`, `z…` descriptively only.
- **T1 and T5:** the filter bar and search bar keep their existing style vars. They come out restyled for free.
- **T2, T3:** new cells use `colorFgDim`, which maps to FgDim.
- **S3:** the preview line becomes FgDim, with an age shown in FgMuted.
- **S4, S5:** the banner and fleet summary must use `styleDim`/`colorAccent`; there must be no hex in `TestNoHardcodedHex`.
- **U3:** spawn pickers (`settings.go:255-345`) are kept verbatim, and U1 rebases on U3's tool filtering.
- **P1 (setup step):** land P1 first. U1 then restyles `setup_wizard.go` mechanically.
- **P2, P3:** the CLI style migration touches `core/help.go`, `doctor.go`, `update.go`, `setup.go` and `uninstall.go`. Var renames only, so rebases are trivial.
- **A1:** none.
- **U2:** depends on `catalog/theme`, `rebuildStyles` and the settings detail pane, which shows where each value comes from.

## Security notes
- Inline settings editing writes user-typed strings. `prompt_glyph` is checked for printable characters and width (no ESC/C0/C1/bidi), because it is echoed on every frame. `auto_trust_dirs` reuses the existing validation, so no wider trust than `rex setup` allows.
- Help and settings render only rex-owned strings. Settings values still go through `settingValueText`, which flattens newlines; that is kept.
- The background dim strips ANSI from the board, so it removes styling and never adds sequences.
- No socket, PTY or init.lua changes.

## Open questions for user
1. Should the new palette replace `default` (old look kept as `classic`), and should the accent be violet #B392F0? **Recommended: yes and yes.**
2. Square, titled borders on every overlay, the attach popup included, plus a dimmed board behind non-attach overlays? **Recommended: yes.** Attach keeps a calmer Border color and is not dimmed, for frame time.
3. Convert `:` command dispatch into a table that help reads from? This is a bigger refactor, but it ends help drift for good. **Recommended: yes.** It lands last, after T1, T3, T4 and T5.
4. Hide `keybind_preset` until presets exist? **Recommended: hide it in the TUI; keep it in the CLI and the file.**

## Ordered steps (each one commit, builds, `make check` green)
1. `test(tui): golden snapshot harness with baseline` (golden_test.go plus testdata; no production change).
2. `fix(tui): color the selection arrow, apply theme on the UI goroutine, reverse-video cursor`. Covers styleArrow init order, `stepThemeApply` returning a msg, and `cursorBlock`. Only the styled goldens are regenerated.
3. `refactor(theme): semantic palette package; prebuilt TUI styles`. Adds `catalog/theme` with `classic`, `noir` and `paper` at today's values and `default`=classic, and replaces every inline style and hex in the TUI. Goldens are byte-identical, which proves the refactor changes nothing. Adds `TestNoHardcodedHex` (TUI) and the coverage floor.
4. `refactor(cli): CLI output styles follow color_scheme`. Adds `core.Styles()` and migrates the 7 CLI files, extending `TestNoHardcodedHex` to `surface/cli`. The off-palette greens and reds unify, which is noted in the commit body.
5. `feat(theme): new default palette, high-contrast and ansi themes`. Adds the contrast and distinctness tests, registry options and the CHANGELOG entry. Goldens regenerated.
6. `feat(tui): unified titled panels and dimmed board behind overlays` (goldens regenerated).
7. `refactor(tui): table-driven commands and key bindings` (keys.go and command.go; parity tests; goldens identical).
8. `feat(tui): help with scroll, filter and live legend` (goldens regenerated).
9. `feat(settings): Hidden and Apply metadata on settings` (catalog only; tests).
10. `feat(tui): settings search, section jump, detail pane, modified marker` (goldens regenerated).
11. `feat(tui): edit free-form string settings inline` (lineedit.go; rename reuses it).
12. `docs: tui, settings, appearance guide, mockup palette`.

## Size estimate
**L** (12 commits; about 1.5–2k lines changed, much of it mechanical style replacement and goldens).
