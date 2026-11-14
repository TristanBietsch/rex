# R16 task plan: new-agent page with context

> **For agentic workers:** REQUIRED SUB-SKILL: use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to work through this plan task by task. Steps use checkboxes (`- [ ]`) for tracking.

**Goal:** `n` opens a full-screen new-agent page (Compose + Agent screens) where the task can be typed multi-line, pasted, and given workspace files, folders and URLs as context, which reach the agent safely through the existing argv/PTY prompt delivery. The old popup wizard stays behind `new_agent_page`.

**Architecture:** A new `NewAgentState` on `Model` with `FocusNewAgent`; `n` / `:new` branch on `featureOn("new_agent_page")`. Rendering uses R04's `page.go` frame. Context assembly and validation live in a pure package `internal/features/spawnctx` (no TUI imports), so the daemon cap and a later `rex new -context` share it. The daemon change is one size guard plus an optional metadata field.

**Tech stack:** Go 1.26, bubbletea v1.3 (`KeyMsg.Paste`, `tea.ExecProcess`), lipgloss, `charmbracelet/x/ansi`, `internal/testutil/golden` (txtar, `-update`).

**Spec:** [R16-new-agent-flow.md](R16-new-agent-flow.md). Read it first. Its D1–D12 recommendations are assumed; re-check the Decisions table for the user's answers before each task.

**Start condition:** R04-1 (`page.go`: `pageHeader`, `pageFooter`, `pageNarrow`, `pageChromeRows`, tab strip) and P2 (`feat/w8-p2-trust`: `wizard_trust.go` with `trustOfferFor`, `wizardTrustKey`, `wizardTrustRequest`, `wizTrustLines`) are merged into `integration/todo`. Re-read both on the new head; if a name differs, use the merged name and note it in the commit.

Code on the base this plan depends on:
- `wizard.go`: `openWizard`, `WizardState`, `request`, `wizardLaunchCmd`, `deriveAgentSlug`, `presetModelDefault`, `presetEffortDefault`, `loadWizardSkills`, `closeWizard`, `existingSlugs`, `pendingSpawn`.
- `tool_pickers.go`: `pickerTools(m, reg) (offer []registry.Tool, onboarding []onboardRow)`, `refreshPickers`, `unavailableNotice`.
- `workspace_board.go`: `spawnCWD(m)`. `prompt.go`: `cursorBlock`. `settings.go`: `windowLines`, `wrapHints`. `help.go`: `renderHints`.
- `internal/daemon/server/spawn.go` + `spawn_prompt.go`: `initialPromptDelivery`, `guardLeadingDash`; `validate.go`: `badIntent`.
- `internal/wire/protocol/intents.go`: `NewSession`.

## Global constraints
- Flag `new_agent_page`: bool, default **true**, Section Features, `ApplyLive`. Off: `n` opens `FocusWizard` and every existing wizard golden is byte-identical; no workspace walk, no new key does anything.
- Caps (UTF-8 bytes after sanitizing): one paste **32 KiB**; all paste text **64 KiB**; `InitialPrompt` **96 KiB** (daemon checks too); PTY-pasted prompt (`prompt_paste` tools) **16 KiB**; **32** context items; folder walk **10,000** entries or **300 ms**; preview read **64 KiB** / **200** lines; file-size warning above **1 MiB**.
- Big-paste chip threshold: more than **8** lines or more than **1 KiB**.
- Prompt format, exact: header line `Context from rex (read before starting):`; reference lines `- file: <path>`, `- dir: <path>/ (<n> files)`, `- url: <url>`; paste blocks `<pasted name="paste N" lines="L">` … `</pasted>`; `</pasted>` inside a body becomes `<\/pasted>`.
- Never: `sh -c`, a temp file for the prompt, copying workspace files, reading a path outside the resolved session folder, a network call, persisting the draft to disk.
- Every `View()` line ≤ terminal width; page exactly `h` lines; works at 80×24 and 50×20.
- No color-only meaning (spec "Accessibility").
- Tests never touch real `$HOME` (`t.Setenv("HOME", t.TempDir())`, `USERPROFILE` too).
- Conventional Commits, signed, one per task; CHANGELOG `[Unreleased]`; raise coverage floors in `tools/coverage/baseline.txt`; `make check` green (lint lock: HANDOFF §2 workaround).

## Review focus
1. **A paste that contains a newline right before the user hits enter**: bracketed paste inserts, never launches; only a real `enter` key launches. Test in Task 1 (`TestNewAgent_PasteNeverLaunches`).
2. **Symlink inside the workspace pointing outside** (`ln -s ~/.ssh keys`): refused in the picker, in `@` completion, and again at launch if the link changed after it was added. Tests in Task 5 (`TestValidatePath_SymlinkOut`) and Task 6 (`TestNewAgent_LaunchRevalidates`).
3. **Huge or special files** (a 4 GiB log, a FIFO, `/dev/zero` via symlink): the preview reads at most 64 KiB and never blocks; FIFOs and devices are refused without being opened. Task 5 (`TestPreview_BoundedRead`, `TestValidatePath_FIFO`).
4. **Resize while the import picker or a preview is open** (120×40 → 50×20 → 80×24): no stale rows, cursor visible, exactly `h` lines. Task 7 (`TestNewAgent_ResizeEveryScreen`).
5. **Flag turned off with the page open and a draft**: page closes, draft kept, old wizard gets the task text only with a notice. Task 6 (`TestNewAgent_FlagOffKeepsDraft`).

---

### Task 1 (R16-1): flag, page shell, composer

**Files**
- Modify `internal/catalog/settings/registry.go`: Features row `new_agent_page` (help text from the spec, "Feature flag").
- Create `internal/surface/tui/newagent.go` (state, open/close, key routing), `newagent_view.go` (render), `composer.go` (multi-line editor), tests `newagent_test.go`, `composer_test.go`.
- Modify `keys.go` `runNew` and the `n` handler in `update.go`: branch on the flag (one-line change each; other sessions edit `update.go`, keep the diff to the branch).
- Modify `tui.go` `view()`: `FocusNewAgent` → `renderNewAgent(m)`. Add `FocusNewAgent` next to `FocusWizard` in `model.go`, plus `NewAgent *NewAgentState` and `newAgentDraft *NewAgentState` (kept across closes).
- Modify `golden_test.go`: screens `newagent_empty` at 120×40, 80×24, 50×20.

**Interfaces (produced)**
```go
type naSection int
const (naAgent naSection = iota; naTask; naContext; naTrust)

type NewAgentState struct {
    Section  naSection
    Tools    []registry.Tool
    Missing  []onboardRow
    ToolIdx, ModelIdx, EffortIdx int
    Skills   []skills.Skill; SkillsOn bool; SkillSel map[string]bool
    Task     Composer
    Items    []spawnctx.Item // Task 3
    ItemIdx  int
    Trust    wizTrustMode    // P2
    Screen   naScreen        // naCompose, naAgentScreen, naImport, naPreview (later tasks)
    Err, Notice string
    Launching bool
}
func openNewAgent(m Model) (Model, tea.Cmd)          // restores m.newAgentDraft when set
func closeNewAgent(m Model, keepDraft bool) Model
func updateNewAgentKey(m Model, k tea.KeyMsg) (Model, tea.Cmd)
func renderNewAgent(m Model) string

type Composer struct{ Lines [][]rune; Row, Col int }
func (c *Composer) Insert(s string)          // splits on "\n"
func (c *Composer) Newline()
func (c *Composer) Backspace()
func (c *Composer) DeleteWord()
func (c *Composer) Move(dRow, dCol int)
func (c *Composer) Home(); func (c *Composer) End()
func (c *Composer) Clear()
func (c Composer) String() string            // lines joined by "\n"
func (c Composer) Empty() bool
func (c Composer) View(width, rows int, focused bool, cursor string) []string // wraps, windows around Row
```

- [ ] **Step 1: failing tests.** `composer_test.go`: `TestComposer_InsertMultiline` (`Insert("a\nb")` → `String()=="a\nb"`, `Row==1, Col==1`), `TestComposer_BackspaceJoinsLines`, `TestComposer_DeleteWord`, `TestComposer_WideRunes` (`"日本"` backspace drops one rune), `TestComposer_ViewWrapsAndWindows` (40 lines in 4 rows keeps `Row` visible). `newagent_test.go`: `TestNewAgent_FlagRoutesN` (on → `FocusNewAgent`; off → `FocusWizard`), `TestNewAgent_PasteNeverLaunches` (`KeyMsg{Type: KeyRunes, Runes: []rune("fix\nit\n"), Paste: true}` → no `tea.Cmd` that sends `NewSession`, task `"fix\nit\n"`), `TestNewAgent_EnterLaunchesCtrlJNewline`, `TestNewAgent_TabCyclesSections` (Agent→Task→Context→Trust→Agent; `shift+tab` reverse), `TestNewAgent_EscKeepsDraft` (reopen shows the same task), `TestNewAgent_OffIsInert` (flag off: `ctrl+o`, `ctrl+g`, `ctrl+s` on the board change nothing, existing wizard goldens unchanged).
- [ ] **Step 2: run** `go test ./internal/surface/tui -run 'Composer|NewAgent' -count=1` → FAIL (undefined).
- [ ] **Step 3: implement.** Opening reuses `pickerTools` and the wizard's preset helpers (agent default per spec "Defaults"; last launch remembered in `Model.lastSpawn quickChoice`). Launch builds the request with `WizardState`-equivalent fields; context is empty until Task 3. Body layout: rail (`naRailW = 16`) │ form; under `pageNarrow(w)` the tab strip. Mockup A of the spec is the target.
- [ ] **Step 4: goldens.** `go test ./internal/surface/tui -run TestGolden -update`, read every new `.txtar` by eye against mockup A; confirm `git diff --stat` touches no existing wizard golden.
- [ ] **Step 5: run** `go test ./internal/surface/tui -count=1` → PASS.
- [ ] **Step 6: commit** `feat(tui): new-agent page behind new_agent_page with a multi-line task`.

### Task 2 (R16-2): Agent screen

**Files**: `newagent_agent.go`, `newagent_agent_test.go`; goldens `newagent_agent`, `newagent_models_filter`.

**Interfaces (produced)**
```go
type naAgentPane int // naPaneAgents, naPaneModels, naPaneSkills
func (s *NewAgentState) filteredModels() []registry.Model // case-insensitive substring on ID and Name
func updateNewAgentAgentKey(m Model, k tea.KeyMsg) (Model, tea.Cmd)
```
- [ ] **Step 1: failing tests.** `TestAgentScreen_ListsMissingDimmedUnselectable` (installed `claude, cursor`; `j` past cursor never lands on a missing row; view contains `not installed`), `TestAgentScreen_ModelFilterKeepsCursorByID` (cursor tool with 200 detected models from `daemonTools`; filter `son` → only matches; cursor stays on the same id when still matching; `esc` clears filter first), `TestAgentScreen_EffortBracketed` (view shows `[high]`), `TestAgentScreen_SkillsOnlyWithSkillArgs`, `TestAgentScreen_RefreshPickersKeepsSelection` (ToolsChanged while open).
- [ ] **Step 2: run** → FAIL.
- [ ] **Step 3: implement**; hook `refreshPickers` to also refresh `m.NewAgent` (same cursor-by-id rules as the wizard). `enter` on the `not installed` header opens Settings → Onboarding (D7).
- [ ] **Step 4: goldens** `-update`, read by eye (mockup C).
- [ ] **Step 5: run** package tests → PASS. **Step 6: commit** `feat(tui): agent, model, effort and skills on one screen with a model filter`.

### Task 3 (R16-3): `spawnctx` package + daemon cap

**Files**: create `internal/features/spawnctx/{item.go,assemble.go,sanitize.go,assemble_test.go,sanitize_test.go,fuzz_test.go}`; modify `internal/wire/protocol/intents.go` (`ContextSummary`), `internal/daemon/server/validate.go` (cap), tests `internal/daemon/server/prompt_cap_test.go`; docs `docs/protocol.md`.

**Interfaces (produced)**
```go
package spawnctx
type Kind int
const (Paste Kind = iota; File; Dir; URL)
type Item struct{ Kind Kind; Name, Text string; Bytes, Files int }
type Caps struct{ Paste, PasteTotal, Prompt, Items int }
var DefaultCaps = Caps{Paste: 32 << 10, PasteTotal: 64 << 10, Prompt: 96 << 10, Items: 32}
const PTYPasteCap = 16 << 10
var ErrTooBig = errors.New("too big")         // wrapped with the item name and sizes
func Sanitize(s string) string
func IsBinary(s string) bool                  // > 1% NUL or U+FFFD
func Assemble(task string, items []Item, c Caps) (string, error)
func Summary(items []Item) []protocol.ContextRef

// protocol
type ContextRef struct{ Kind, Name string; Bytes int }
// NewSession gains: ContextSummary []ContextRef `json:"context_summary,omitempty"`
const MaxInitialPromptBytes = 96 << 10
```
- [ ] **Step 1: failing tests.** `TestAssemble_Format` (task + file + dir + url + paste → exact string from Global constraints), `TestAssemble_NoItemsIsTask`, `TestAssemble_EscapesClosingTag`, `TestAssemble_Caps` (table: paste at 32 KiB ok, +1 → `ErrTooBig`; total 64 KiB ok/+1; prompt 96 KiB; 33 items), `TestSanitize` (table: `\r\n`→`\n`, `\x1b[31m` dropped, `‮` dropped, `\t` kept, invalid UTF-8 → U+FFFD), `FuzzSanitize` (output has no C0 except `\n\t`, no bidi controls, is valid UTF-8, idempotent), `TestIsBinary`. Daemon: `TestNewSession_PromptOverCapRefused` (97 KiB → `bad_intent`, error text `initial_prompt is 99328 bytes; the limit is 98304`, no session created), `TestNewSession_ContextSummaryIgnoredByOldShape` (JSON without the field decodes).
- [ ] **Step 2: run** `go test ./internal/features/spawnctx ./internal/daemon/server -count=1` → FAIL.
- [ ] **Step 3: implement.** The daemon cap goes in the existing NewSession validation (`validate.go`), before any session dir is made. Store `ContextSummary` in session meta only if the meta type has a natural slot; otherwise drop it after validation (note in `docs/protocol.md`).
- [ ] **Step 4: run** → PASS; `go test -fuzz FuzzSanitize -fuzztime 30s ./internal/features/spawnctx`.
- [ ] **Step 5: commit** `feat(spawnctx): assemble context into the first prompt; daemon caps initial_prompt at 96 KiB`.

### Task 4 (R16-4): paste chips + preview

**Files**: `newagent_context.go`, `newagent_preview.go`, tests; goldens `newagent_filled`, `newagent_preview`.

**Interfaces (produced)**
```go
func addPaste(s *NewAgentState, text string) error // chip when > 8 lines or > 1 KiB; else Task.Insert
func removeItem(s *NewAgentState, i int)
func renameItem(s *NewAgentState, i int, name string)
func editInEditor(text string) tea.Cmd             // tea.ExecProcess on $EDITOR (fallback vi); temp dir 0700, file 0600, removed after
type editorDoneMsg struct{ Item int; Text string; Err error }
```
- [ ] **Step 1: failing tests.** `TestPaste_SmallInline`, `TestPaste_BigBecomesChip` (9 lines → item `paste 1`, task gets `[paste 1 · 9 lines]`), `TestPaste_SanitizedAndBinaryRefused` (error text starts `✗ `), `TestPaste_OverCapError` (spec copy: `paste 2 is 210 KB; the limit for pasted text is 64 KB in total.`), `TestContextList_RemoveRenamePreview`, `TestEditor_TempFilePerms` (inject `editorCmd` that records the path; `0600`, parent `0700`, gone after `editorDoneMsg`), `TestPreview_ScrollAndBack`.
- [ ] **Step 2: run** → FAIL. **Step 3: implement.** Launch summary row (`prompt X KB of 64 KB · slug …`) uses `spawnctx.Assemble` on every render only when items changed (cache by a revision counter).
- [ ] **Step 4: goldens** `-update`, read by eye (mockups B, E). **Step 5: run** → PASS. **Step 6: commit** `feat(tui): pasted context as chips with preview, rename, remove and $EDITOR`.

### Task 5 (R16-5): import picker + `@` completion

**Files**: create `internal/features/spawnctx/{paths.go,walk.go,paths_test.go,walk_test.go,paths_unix_test.go}`; `internal/surface/tui/newagent_import.go`, tests; golden `newagent_import`, `newagent_loading`.

**Interfaces (produced)**
```go
package spawnctx
type PathInfo struct{ Rel string; Dir bool; Size int64; Files int; Warn string; Deny string }
var ErrOutside, ErrSpecial, ErrBadName = errors.New(...), ...
func ValidatePath(root, p string) (PathInfo, error) // Lstat, EvalSymlinks, inside root, regular/dir only, no control chars, denylist → Deny reason
func Denied(rel string) (reason string, ok bool)
type Index struct{ Paths []string; Truncated bool }
func Walk(ctx context.Context, root string, maxEntries int, budget time.Duration) (Index, error) // git ls-files -co --exclude-standard when root is in a repo (D9), else WalkDir; never enters symlinked dirs; skips .git
func Match(idx Index, query string, limit int) []string // fuzzy subsequence, shorter path and basename hits first
func Preview(root, rel string, maxBytes, maxLines int) (lines []string, more int, err error)

// tui
type indexDoneMsg struct{ Index spawnctx.Index; Err error }
func startIndex(root string) tea.Cmd // context cancelled on page close
```
- [ ] **Step 1: failing tests.** `TestValidatePath_Inside`, `_DotDot`, `_AbsoluteOutside`, `_SymlinkOut` (error names the target), `_SymlinkInside` (ok), `_FIFO` (Unix: `syscall.Mkfifo`; refused, never opened: test passes within 1 s), `_ControlCharName` (skip on Windows), `_Denylist` (table of every spec pattern → `Deny` set), `TestWalk_CapsEntries` (10,001 files → 10,000, `Truncated`), `TestWalk_Budget` (injected budget 0 → `Truncated`), `TestWalk_GitIgnore` (repo with ignored `dist/` → absent), `TestWalk_SkipsSymlinkedDirs`, `TestMatch_Order`, `TestPreview_BoundedRead` (5 MiB file → ≤ 64 KiB read, `more` > 0), `TestPreview_Binary`. TUI: `TestImport_MarkAndAdd`, `TestImport_DenyNeedsConfirm` (`y` adds), `TestAt_CompletionInsertsAndAdds`, `TestAt_EscKeepsText`, `TestImport_LoadingThenReady`.
- [ ] **Step 2: run** → FAIL. **Step 3: implement**; the walk runs in a `tea.Cmd` (never on the UI goroutine), started when the page opens with the flag on.
- [ ] **Step 4: goldens** `-update`, read by eye (mockups D, F). **Step 5: run** → PASS (also `GOOS=windows go vet ./internal/features/spawnctx`). **Step 6: commit** `feat(tui): import workspace files and folders as context (picker and @)`.

### Task 6 (R16-6): trust section, launch states, draft

**Files**: `newagent_launch.go`, tests; reuse P2's `wizard_trust.go` (generalize `wizTrustLines(m)` to take the trust mode and width: `trustLines(mode wizTrustMode, cwd string, w int) []string`, wizard calls it unchanged); golden `newagent_errors`.

**Interfaces (produced)**
```go
type newAgentLaunchedMsg struct{ Slug string; Err error }
func newAgentLaunchCmd(c *client.Client, req protocol.NewSession) tea.Cmd
func (s *NewAgentState) request(cwd string, existing []string) (protocol.NewSession, error) // Assemble + revalidate every path + PTYPasteCap for prompt_paste tools
```
- [ ] **Step 1: failing tests.** `TestNewAgent_TrustKeysMatchWizard` (same offers/refusals as P2's tests, through the page), `TestNewAgent_LaunchKeepsPageUntilAnswer` (`Launching` true, view shows `starting…`), `TestNewAgent_LaunchErrorKeepsDraft` (`CmdErrMsg` → page open, task and items intact, `✗ launch failed: …` shown), `TestNewAgent_LaunchSuccessClearsDraft` (selects slug), `TestNewAgent_LaunchRevalidates` (symlink swapped to outside after add → refused), `TestNewAgent_PTYPasteCap` (ollama + 17 KiB → error naming the 16 KB limit), `TestNewAgent_FlagOffKeepsDraft`, `TestNewAgent_RequestCarriesSummary`.
- [ ] **Step 2: run** → FAIL. **Step 3: implement.** **Step 4: goldens** `-update`, read by eye (mockup F). **Step 5: run** → PASS. **Step 6: commit** `feat(tui): trust, launch progress and kept drafts on the new-agent page`.

### Task 7 (R16-7): narrow layout, accessibility, docs

**Files**: `newagent_view.go`; goldens at 50×20 for every screen, `newagent_frame` (window border, 80×24), `newagent_ansi` (`theme ansi`); `docs/tui.md` (new "NEW-AGENT PAGE" section; old wizard section marked "with `new_agent_page` off"), `docs/settings.md` (flag row), `docs/plans/r2/00-roadmap.md` (flag table row), `CHANGELOG.md`.
- [ ] **Step 1: failing tests.** `TestNewAgent_ResizeEveryScreen` (each screen at 120×40 → 50×20 → 80×24: `len(lines)==h`, every line width ≤ w, cursor row present), `TestNewAgent_NoColorOnlyState` (strip ANSI from every golden screen: selected agent has `▸`, effort has `[`, errors start `✗`, warnings `!`, rail has a word for trust), `TestNewAgent_HelpGroup` (`?` opens help filtered to the new-agent rows; every key in the spec table is listed).
- [ ] **Step 2: run** → FAIL. **Step 3: implement** tab strip and help rows (`keys.go` command table, `Ctx: ctxNewAgent`).
- [ ] **Step 4: goldens** `-update`, read by eye (mockup G). **Step 5:** `make check` → exit 0. **Step 6: commit** `feat(tui): new-agent page at narrow widths; docs and changelog`.

### Later (not in this plan)
- R16-8 import a recent session (D10). R16-9 `rex new -context PATH` (repeatable) and `-paste-stdin`, reusing `spawnctx`. Per-tool `@path` templates (D8).
