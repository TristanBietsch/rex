# tui

**Path:** `internal/surface/tui`
**Depends on:** `internal/features/audio`, `internal/wire/client`, `internal/wire/protocol`, `internal/catalog/settings`, `internal/catalog/registry`, `internal/runtime/daemonctl`, `internal/runtime/rexlog` | external: `github.com/charmbracelet/bubbletea`, `github.com/charmbracelet/lipgloss`, `github.com/charmbracelet/x/ansi`, `github.com/hinshun/vt10x`
**Depended on by:** `internal/surface/cli`
**Entry points:** `Run`, `Model` (Bubble Tea), `RunSetupWizard`, setup helpers

## Purpose

Bubble Tea kanban board: session list by state, spawn wizard, attach overlay, settings, boot splash, stats overlay, slash command palette.

## Public surface

### Functions

- `Run(socket string) error` — main TUI program.
- `RunSetupWizard(configPath, socket string) error`
- `ApplyDefaultsNonInteractive(configPath, socket, toolID, modelID, effortVal string, noShell bool) error`
- `SaveTUIState(m Model) error`, `LoadTUIState() (selection, filter string, ok bool)` — `~/.local/state/rex/tui-state.json`
- `ShellProfileBlock(shell string) string`, `DetectShellRC() string`, `HasShellBlock(rcPath string) bool`

### Types (Bubble Tea)

- `Model` — `Init`, `Update`, `View`; fields include `Client`, `Socket`, `Focus`, `Sessions`, `SelectedID`, `Filter`, prompts, wizard/attach/settings state.
- `Focus` enum — `FocusBoard`, `FocusPrompt`, `FocusCommand`, `FocusWizard`, `FocusHelp`, `FocusConfirmQuit`, `FocusConfirmDelete`, `FocusSettings`, `FocusAttach`, `FocusFail`, `FocusBoot`, `FocusStats`
- Message types: `DaemonEventMsg`, `DaemonErrMsg`, `SpinnerTickMsg`, `AttachOutputMsg`, `AttachClosedMsg`
- `AttachState`, `WizardState`, `SettingsState`, `FailState`, `SetupWizardModel`, `DescAnim`

## Internal structure

| File | Role |
|------|------|
| `tui.go` | Program entry, tea.NewProgram |
| `model.go` | Model struct and Init |
| `update.go` | Update loop, daemon intents, spawn |
| `board.go` | Kanban columns render |
| `header.go` | Aggregate counts |
| `prompt.go` | Bottom prompt |
| `command.go` | `/` command palette |
| `wizard.go` | New-session wizard |
| `attach.go` | Full-screen attach (vt10x) |
| `settings.go` | Settings overlay |
| `setup_wizard.go` | First-run wizard |
| `splash.go`, `splash_steps.go` | Boot splash + daemon start |
| `keymap.go` | Key bindings |
| `help.go` | `?` overlay |
| `confirm.go` | Quit/delete confirms |
| `persist.go` | tui-state.json |
| `styles.go`, `anim.go` | Lipgloss and description animation |
| `stats_overlay.go` | Token histogram overlay |
| `events.go` | tea.Msg wrappers |
| `fail.go` | Backend failure UI |

Tests: `*_test.go` for board snapshots, splash, wizard slugs, settings, stats overlay, update spawn logic.

## Invariants

Boot splash must reach daemon (`daemonctl.Start` or existing socket) before board `Hello`. `Model.Client` receives events on dedicated goroutine; updates on main tea thread.

## Side effects

UDS to daemon; audio `Play`; writes `tui-state.json` on exit; setup wizard writes `config.yaml` and optional shell RC block; spawns `rex attach` child for in-TUI attach path (see `attach.go`).

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
