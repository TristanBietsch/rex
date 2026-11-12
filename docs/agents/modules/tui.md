# tui

**Path:** `internal/surface/tui`
**Depends on:** `internal/features/audio`, `internal/wire/client`, `internal/wire/protocol`, `internal/catalog/settings`, `internal/catalog/registry`, `internal/runtime/daemonctl`, `internal/runtime/rexlog` | external: `github.com/charmbracelet/bubbletea`, `github.com/charmbracelet/lipgloss`, `github.com/charmbracelet/x/ansi`, `github.com/hinshun/vt10x` (forked in `third_party/vt10x` via `replace`)
**Depended on by:** `internal/surface/cli`
**Entry points:** `Run`, `Model` (Bubble Tea), `RunSetupWizard`, setup helpers

## Purpose

Bubble Tea kanban board: session list by state, spawn wizard, attach overlay, settings, boot splash, stats overlay, slash command palette.

## Public surface

### Functions

- `Run(socket string) error` — main TUI program.
- `RunSetupWizard(configPath, socket, autoTrust string) error`
- `ApplyDefaultsNonInteractive(configPath, socket, toolID, modelID, effortVal, autoTrust string, noShell bool) error`
- `SaveTUIState(m Model) error`, `LoadTUIState() (persistedState, ok bool)` — `~/.local/state/rex/tui-state.json`
- `ShellProfileBlock(shell string) string`, `DetectShellRC() string`, `HasShellBlock(rcPath string) bool`

### Types (Bubble Tea)

- `Model` — `Init`, `Update`, `View`; fields include `Client`, `Socket`, `Focus`, `Sessions`, `SelectedID`, `Filter`, prompts, wizard/attach/settings state.
- `Focus` enum — `FocusBoard`, `FocusPrompt`, `FocusCommand`, `FocusWizard`, `FocusHelp`, `FocusConfirmQuit`, `FocusConfirmDelete`, `FocusSettings`, `FocusAttach`, `FocusFail`, `FocusBoot`, `FocusStats`
- Message types: `DaemonEventMsg`, `DaemonErrMsg`, `SpinnerTickMsg`, `AttachOutputMsg`, `AttachClosedMsg`
- `AttachState`, `WizardState`, `SettingsState`, `FailState`, `SetupWizardModel`, `DescAnim`

## Colors and styles

Colors come from `internal/catalog/theme` (`theme.Palette`, six built-ins). `applyTheme(name)` fills the `color*` tokens and `rebuildStyles` prebuilds every style; renderers never build a colored style or name a hex value (`TestNoHardcodedHex`, `TestNoInlineColoredStyles`). Call `applyTheme` from `Update`, never from a `tea.Cmd` goroutine. User themes (`~/.config/rex/themes/*.yaml`, `theme.LoadDir`) live in `Model.Themes`; `applyThemeFrom(m.Themes, name)` applies one, and the marker glyphs come from `stateGlyph` / `selectGlyph` (`glyphs.go`). The `themes.load` boot step installs the catalog before `theme.apply`. `config_reload.go` polls `config.yaml` and the theme files (`armConfig`, `configTickMsg`, `configReloadedMsg`); `handleConfigReloaded` swaps the store and calls `applyLive` for changed `ApplyLive` settings only.

- Tokens: `colorFgPrimary/FgDim/FgMuted`, `colorBorder/BorderFocus`, `colorBgSel`, `colorAccent`, `colorWorking/Queued/Needs/Done/Failed/Crashed`, `colorWarn/Danger/Success/Info`, `colorFleet`.
- Styles: `styleLogo`, `styleKey` (bold key caps), `styleKbd`, `styleCmd` (`:command` names), `styleBold`, `styleDim`, `styleMuted`, `stylePrimary`, `styleAlert`, `styleNeeds/Done/Failed/Working`, `styleMarker[state]`, `styleLongRun[tier]`, `styleFleet[i]`, `styleArrow` (accent), `styleArrowYellow` (warn), `styleBorderFocus`, `styleCursor`; `selectedStyle(style)` marks a selection (background, or reverse video under `ansi`). `brandStyle` is the one place a tool's own registry color is used.
- Screens are pinned by `golden_test.go` (txtar under `testdata/golden`): plain text at 80x24 and 120x40, TrueColor for the board and settings. `go test ./internal/surface/tui -run TestGolden -update` rewrites them; a refactor must leave them byte-identical.

## Internal structure

| File | Role |
|------|------|
| `tui.go` | Program entry, tea.NewProgram |
| `model.go` | Model struct and Init |
| `update.go` | Update loop, daemon intents, spawn |
| `board.go` | Kanban columns render; section registry (`boardGroups`, `resolveSection`) |
| `row_icons.go` | Row icon strip behind `row_icons`: slots (`iconSlots`, `slotWidth`), glyphs (`iconGlyph`, ASCII forms), `slotCell`, unseen tracking (`markSeen`, `isUnseen`, `SeenAt`/`EnteredAt`); grid in `board_columns.go`, mobile line 1 in `board_mobile.go` (`mobileIcons`), legend in `help.go` (`rowIconLegend`) |
| `rowfilter.go` | `rowFilter`: the one predicate every board view applies |
| `filter.go` | `:filter` grammar, hidden sections (`setSectionHidden`, `z` chord, `board_hidden_sections`), filter-bar status |
| `chords.go` | Board chord table (`dd`, `cc`; first key to handler) |
| `frame.go` | Window frame: `relayout`/`syncLayout` (terminal size `TermW/TermH` vs layout size `Width/Height`), `frameInset`, `wrapFrame` + row cache, `translateMouse`, border color (`borderEdge`, `borderColorOf`; random = `PickEdge(BorderSeed)`) |
| `header.go` | Aggregate counts |
| `prompt.go` | Bottom prompt |
| `command.go` | `:` command line: parse and look up in `commands` |
| `keys.go` | `commands` table (names, args, help text, `Run`) and `bindings` table (help rows, board help line); add a `:` command here |
| `wizard.go` | New-session wizard |
| `attach.go` | Full-screen attach (vt10x) |
| `attach_mouse.go` | Popup mouse: wheel/click passthrough, wheel batching |
| `attach_scroll.go` | Popup scrollback ring + scroll mode (primary-screen agents) |
| `attach_render.go` | Popup cell painter (cached SGR per color/attribute set) and per-row render cache |
| `attach_cmd.go` | `::` command bar and `qq` detach inside the popup (`::banner`, `::border`) |
| `border_cmd.go` | `:border` / `::border`: per-run frame choice (`borderRunOn`, `borderRunColor`), notices; never saved |
| `attach_banner.go` | Attach banner row: `attachBannerRows` (the one place that decides whether it shows), `bannerParts`, static layout, `marqueeWindow`, tick chain, `::banner` |
| `settings.go` | Settings state and keys (page and overlay): search, section jump (`settingsSectionJump`), page panes and section keys (`settingsPageKey`, `settingsSections`, `settingsPageList`), inline editor (`settingEdit`; saves through `Store.Set`) |
| `page.go` | Full-screen page frame behind `fullscreen_pages` (`pagesOn`): header, footer, `composePage`, section pane and narrow strip, `renderSettingsPage`, `layoutHelpPage`, `renderHelpPage` |
| `settings_view.go` | Settings render: list, modified marker, fixed-height detail pane (`settingsDetail`) |
| `lineedit.go` | `lineEditor`: one-line text editing shared by rename and the settings editor |
| `panel.go` | `panelLines`: the titled square panel every overlay uses; `dimBoard` mutes the board behind it |
| `setup_wizard.go` | First-run wizard |
| `splash.go`, `splash_steps.go` | Boot splash + daemon start |
| `keymap.go` | Key bindings |
| `help.go` | `?` page and overlay: groups (`helpGroups`), scrollable body (`layoutHelp`; `helpLayoutFor` picks it or the page's `layoutHelpPage`), `/` filter, live legend (`helpLegend`), `(off)` marks from `binding.Flag` / `command.Flag` |
| `confirm.go` | Quit/delete confirms |
| `persist.go` | tui-state.json |
| `styles.go`, `anim.go` | Palette tokens and prebuilt styles (see below), description animation |
| `stats_overlay.go` | Token histogram overlay |
| `events.go` | tea.Msg wrappers |
| `fail.go` | Backend failure UI |
| `screensaver.go` | Idle timer, screensaver show/wake and render (`screensaver_*` settings) |
| `keepawake.go` | macOS display keep-awake (`caffeinate -d -w <pid>`) while the screensaver shows |

Tests: `*_test.go` for board snapshots, splash, wizard slugs, settings, stats overlay, update spawn logic.

## Invariants

Boot splash must reach daemon (`daemonctl.Start` or existing socket) before board `Hello`. `Model.Client` receives events on dedicated goroutine; updates on main tea thread.

## Side effects

UDS to daemon; audio `Play`; writes `tui-state.json` on exit; setup wizard writes `config.yaml` and optional shell RC block; spawns `rex attach` child for in-TUI attach path (see `attach.go`); on macOS runs `/usr/bin/caffeinate` while the screensaver shows (`keepawake.go`; tests inject a fake launcher).

## Error handling

`DaemonErrMsg` surfaces connection loss. Boot failure → `FocusFail` with log lines.

## Tests

Snapshot tests for board/header/splash; spawn slug derivation; setup shell block idempotency; stats overlay buckets.

## Gotchas

`cli.RunRender` constructs `Model` without running full `Run`. Keymap details live in `keymap.go` — not duplicated here; human doc `docs/tui.md` may list keys. Fleet colors and archived title prefix logic in `board.go`. Quick-spawn (`i`) reads `default_spawn_*` settings.

## See also

- `modules/cli.md` — `RunTUI`, `RunRender`, `RunSetup`.
- `modules/client.md`
- Human reference: [../../tui.md](../../tui.md), [../../slash.md](../../slash.md).
