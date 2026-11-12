# R04 task plan: settings and help as full-screen pages

> **For agentic workers:** REQUIRED SUB-SKILL: use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to work through this plan task by task. Steps use checkboxes (`- [ ]`) for tracking.

**Goal:** `:settings` and `:help` open as full-screen pages (board header, dividers, hint footer, window border) with a section list on the left. The old overlays stay in the code behind the `fullscreen_pages` flag. Three bundled themes are added.

**Start condition (2026-11-11, user):** build subagent-driven (fresh builder + reviewer per task), and only after `feat/settings-onoff-style` (on/off values right-aligned in settings rows, "on" white, "off" gray via theme tokens) and `feat/hints-footer` (DONE-column chips moved into the bottom hint bar; new bottom-hints boolean setting) are merged into `integration/todo`. Before Task 1, re-read `settings.go`, `settings_view.go`, `styles.go` and the footer/hints code on the new head and carry both changes into the page renderers: the page rows keep the right-aligned colored on/off, and `pageFooter` honors the bottom-hints setting.

**Architecture:** Keep `FocusSettings` / `FocusHelp`. `view()` and the key and mouse handlers branch on `fullscreen_pages`. A new `page.go` holds the shared frame: header, dividers, footer, pane join, section list and narrow tab strip. Settings and help each add a page renderer next to their overlay renderer. Bundled themes are YAML files embedded in `internal/catalog/theme`, parsed by the U2 parser and kept in a map separate from the Go built-ins, so a user file with the same name still overrides them.

**Tech stack:** Go 1.26, bubbletea, lipgloss, `charmbracelet/x/ansi`, `internal/testutil/golden` (txtar, `-update`), `embed`.

**Spec:** [R04-fullscreen-settings-help.md](R04-fullscreen-settings-help.md). Read it first. Decisions D1–D7 are binding.

Base: `integration/todo` at `bc28b0b` or later. Code on that base that this plan depends on:
- `internal/surface/tui/tui.go:127` `view()`: `FocusHelp` and `FocusSettings` draw `centerOverlayBox(…, dimBoard(renderFullScreen(…)))`. `FocusUsage` is also an overlay, but R04 does not touch it.
- `settings.go`: `SettingsState` (now with `Onboarding []onboardRow`); `settingsList` appends Onboarding rows after `settings.Visible()`; `settingsAfter`, `settingsSectionJump`, `settingsKeepCursor`, `windowLines`, `wrapHints`, `settingsHints`.
- `settings_view.go`: `renderSettings(m, termW)` builds the rows, detail pane and footer inline; `settingsDetail(m, s, inner, helpN, expanded)`.
- `help.go`: `HelpState{Scroll, Filter, Filtering}`, `helpLeft`, `helpLegend`, `helpRight`, `markOff`, `filterGroups`, `renderGroup`, `renderColumn`, `layoutHelp`, `renderHints`, `helpSize`.
- `board_mouse.go:126` `updateSettingsMouse`; `help.go` `updateHelpMouse`.
- `attach_cmd.go:139` `executeAttachCommand`: an unknown verb detaches and runs the board command.
- `golden_test.go`: `goldenScreen{name, styled, model}`; `goldenSizes = 80×24, 120×40`; screens `help`, `help_filtered`, `settings`, `settings_search`, `settings_onboarding`.
- `internal/catalog/theme`: `Builtin` map; `Get`; `Parse` (refuses `Builtin` names); `Catalog.Get` (user, then `Get`); `contrast_test.go` `hexThemes()`, `TestBuiltinContrast`, `TestStateHuesDistinct`.
- Theme option lists: `theme_cmd.go:16` `themeOptions`, `setup_wizard.go:188`, `cli/setup/theme.go:62`. Each one is registry `Options` + `UserNames()`.

## Global constraints
- Flag `fullscreen_pages`: bool, default **true**, Section Features, `ApplyLive`. When it is off, both foci render byte-identical to the pre-R04 goldens. The keys `q`, `1`–`9`, `[`, `]` and pane focus change nothing, and `tab` keeps U1's section jump.
- The flag is read on every frame and every key (`m.featureOn`), never cached. Turning it off from inside the settings page switches to the overlay on the next frame, with the same cursor.
- Never draw a page over the attach PTY. `::settings` / `::help` while attached detach, then open (D8).
- No horizontal clipping at 80×24. Every `View()` line is at most the terminal width at any size, and the page is exactly `h` lines.
- Left pane width = longest title + badge, capped at **24** columns. Under **60** columns, one column with a tab strip.
- Height: header 3 rows (blank row + title row + divider, matching the board's blank top row so the logo lines up, D9) and footer 2 rows (divider + hints). The settings inspector is `2 + settingsHelpLines` (4), or `2 + settingsHelpLinesShort` (2) under `settingsTallHeight` (30) rows.
- Theme names, exact: `dracula`, `tokyo_night`, `pantone_safe`.
- Tests never touch the real `$HOME` (`t.Setenv("HOME", t.TempDir())`; `USERPROFILE` too where paths are resolved).
- Commits: Conventional Commits, signed, one per task. CHANGELOG `[Unreleased]` for user-visible changes. Raise coverage floors in `tools/coverage/baseline.txt` when they rise. `make check` before the PR.

## Review focus
1. **Resize while a page is open** (120×40 → 50×20 → 80×24): the layout switches between two panes and the tab strip, the cursor row stays visible, there is no stale row, and the output is exactly `h` lines. Owned by Task 1 (frame) and Task 3 (settings panes).
2. **Turning `fullscreen_pages` off from inside the settings page**: the next frame is the overlay with the same `CursorID`, and `Pane` is ignored. Turning it back on restores the page. Owned by Task 2.
3. **Search, then `esc`**: esc while typing the query restores the section from before the search. esc on a query already kept with `enter` keeps the cursor on the matched row. A zero-hit query shows the empty-state line, not a blank pane. Owned by Tasks 2 and 3 (settings) and Task 4 (help).
4. **Theme changed from the page**: it applies on the UI goroutine (`applyLive`, no `tea.Cmd`), and the same frame renders in the new palette. A user `dracula.yaml` beats the bundled one, and the theme picker lists `dracula` once. Owned by Tasks 3 and 5.
5. **Window border on (U4)**: the page sits inside the frame exactly as the board does (layout size = terminal − 2·inset), and wheel events on the frame are dropped. Owned by Task 1 (framed golden).

---

### Task 1 (R04-1): flag + page frame, current bodies inside

**Files**
- Modify `internal/catalog/settings/registry.go`: add the Features row `fullscreen_pages` after `session_export`.
- Create `internal/surface/tui/page.go` and `page_test.go`.
- Modify `tui.go` `view()`: branch `FocusHelp` / `FocusSettings` on `pagesOn(m)`.
- Modify `settings_view.go`: extract the body of `renderSettings` into `settingsBody` (below). The overlay calls it, and its output stays byte-identical.
- Modify `help.go`: `layoutHelp` is unchanged. Add `layoutHelpPage`, plus `helpLayoutFor(m, w, h)` that picks one of the two; `updateHelpKey` and `updateHelpMouse` call `helpLayoutFor`. Add `q` to close (page only).
- Modify `settings.go` `settingsKey`: `q` closes (page only).
- Modify `golden_test.go`: add `sizes []goldenSize` to `goldenScreen` (nil = `goldenSizes`) and use it in both golden tests. Pin the existing `help`, `help_filtered`, `settings`, `settings_search` and `settings_onboarding` screens to flag **off** (`m.Store.Set("fullscreen_pages", false)`), so their `.txtar` files do not change. Add `settings_page` and `help_page` at 120×40, 80×24 and 50×20, plus `settings_page_frame` (the `framedGolden` helper) at 80×24.
- Sweep the existing overlay geometry tests (`overlay_fit_test.go`, `help_scroll_test.go`, `help_sections_test.go`, `settings_test.go`, `settings_search_test.go`, `board_mouse_test.go`, `board_mobile_test.go`): any test that asserts overlay layout gets `overlayMode(t, m)` (sets the flag off, in `page_test.go`). Behavior tests stay on the default.
- `docs/settings.md`: add the Features row now (the docs task expands it).

**Interfaces (produced)**
```go
// registry row
{ID: "fullscreen_pages", Label: "Full-screen settings and help", Section: SectionFeatures, Apply: ApplyLive,
 Type: TypeBool, Default: true,
 Help: "Settings and help open as full-screen pages with a section list on the left, like the board. Off: they open as the older floating overlays, and the page keys (tab between panes, 1-9, [ and ], q) do nothing."}

// page.go
type pagePane int
const (
    paneRows     pagePane = iota // zero value: a page opens with the rows focused
    paneSections
)
const (
    pageNarrowW      = 60
    pageSectionsMaxW = 24
    pageChromeRows   = 5 // blank + header + divider + divider + hints
)
func pagesOn(m Model) bool                                   // m.featureOn("fullscreen_pages")
func pageNarrow(w int) bool                                  // w < pageNarrowW
func pageHeader(title, middle, right string, w int) []string // 3 lines: blank, title row, divider
func pageFooter(hints [][2]string, w int) []string           // 2 lines: renderHR(w), renderHints(hints, w-2) indented 2
func pageBodyHeight(h int) int                               // max(h-pageChromeRows, 1)
func composePage(head, body, foot []string, w, h int) string // exactly h lines, each padLine'd to w; body padded or cut to pageBodyHeight(h)

// settings_view.go
// settingsBody is the list + blank + detail of renderSettings at inner width, list
// windowed to listRows; renderSettings wraps it in its search line, footer and box.
func settingsBody(m Model, list []settings.Setting, inner, listRows int) (rows []string, detail []string)
func renderSettingsPage(m Model, w, h int) string

// help.go
func layoutHelpPage(m Model, w, h int) helpLayout // box = the whole page, h lines
func helpLayoutFor(m Model, w, h int) helpLayout  // page or overlay by pagesOn
func renderHelpPage(m Model, w, h int) string
```
- `pageHeader` title row: `"  " + styleLogo("∴ REX") + styleDim(" › " + title)`, then 4 spaces and `middle` (dim), then `right` (dim) right-aligned. Under `pageNarrowW` the right slot is dropped. `middle` is truncated with `…` before `left` is touched. The divider is `renderHR(w)`.
- Settings header: title `SETTINGS`; middle `Section › Label` of the cursor row, or the search (`/ query` plus `cursorLit()` while typing); right `filepath.Base(m.StorePath)`, or `config.yaml` when the path is empty.
- Help header: title `HELP`; middle = the filter (same format); right `"rex " + version.Version`.
- In this task the bodies are single lists. Settings: `settingsBody(m, settingsList(st), w-4, …)` indented 2. Help: `helpBody(m, w+8, cols)` (2 columns at `w >= helpTwoColMinW`) scrolled by `Scroll` with `↑ more` / `↓ more`. Tasks 3 and 4 replace them.

- [ ] **Step 1: failing tests** in `page_test.go`:
  - `TestPagesFlagDefaultOn`: `settings.Find("fullscreen_pages")` has default `true` and Section Features; `pagesOn(Model{})` is true.
  - `TestPageFrameRows`: for `(w,h)` in `{120,40},{80,24},{50,20},{30,10}`, open settings and help. `View()` has exactly `h` lines, every line has `ansi.StringWidth ≤ w`, line 0 is blank (D9), line 1 contains `∴ REX › SETTINGS` (or `› HELP`) at the same column as the board header's logo, line 2 and line `h-2` are all `─`, and line `h-1` contains `esc`.
  - `TestPageNoBoardBehind`: with sessions `layoutSessions(goldenNow)`, the settings page's `View()` contains no session slug.
  - `TestPageEscKeepsSelection`: `SelectedID = "3"`, open settings, press `esc`: `Focus == FocusBoard`, `SelectedID == "3"`. Same for help, also with `q`.
  - `TestPageQInertWhenOff`: `overlayMode`, press `q` in settings and in help: focus unchanged.
  - `TestPageResize`: open settings at 120×40, send `tea.WindowSizeMsg{50,20}`, then `{80,24}`. After each, the line count equals `h` and the cursor label is in `View()`.
  - `TestPageHeaderSlots`: the settings header contains `Appearance › Theme` and `config.yaml`; the help header contains `rex ` + `version.Version`; at `w=50` the right slot is absent.
- [ ] **Step 2:** `go test ./internal/surface/tui -run 'TestPage' -v`. Expect FAIL (undefined `pagesOn`, …).
- [ ] **Step 3:** add the registry row, `page.go`, `settingsBody` (cut out of `renderSettings`), `renderSettingsPage`, `layoutHelpPage`/`helpLayoutFor`/`renderHelpPage`, the `view()` branch, and the `q` keys inside `if pagesOn(m)`.
- [ ] **Step 4:** `go test ./internal/surface/tui -run 'TestPage|TestGolden' -v`. The new goldens fail (no file yet), and the five pinned overlay goldens must PASS **unchanged**. Then `go test ./internal/surface/tui -run TestGoldenScreens -update` and read the new `settings_page.txtar` / `help_page.txtar` / `settings_page_frame.txtar` by eye at every size. Check that `git diff --stat testdata/golden` lists only new files.
- [ ] **Step 5:** `go test ./internal/surface/tui ./internal/catalog/settings`. Fix the overlay tests that fail by adding `overlayMode`, and only those.
- [ ] **Step 6:** commit `feat(tui): settings and help as full-screen pages behind fullscreen_pages`.

---

### Task 2 (R04-2a): settings page state and keys

**Files**
- Modify `settings.go` (state, keys) and `board_mouse.go` (`updateSettingsMouse`).
- Test: `settings_page_test.go`.

**Interfaces**
- Consumes: `pagePane`, `paneRows`, `paneSections`, `pagesOn`, `pageNarrow` (Task 1).
- Produces:
```go
// SettingsState gains
Pane      pagePane
preSearch string // CursorID when `/` opened on the page; esc while typing restores it

func settingsSections(st *SettingsState) []settings.Section // order of first appearance in settingsList with Search "" (Onboarding rows included)
func settingsSectionOf(st *SettingsState) settings.Section  // section of CursorID; first section if not found
func settingsPageList(m Model) []settings.Setting           // Search != "": settingsList(st); else rows of settingsSectionOf
func settingsAfterIn(list []settings.Setting, cur string, delta int) string // settingsAfter's body, list given; settingsAfter becomes a wrapper
func settingsGoSection(st *SettingsState, idx int) string   // first row id of settingsSections[clamped idx]
func settingsModifiedIn(m Model, sec settings.Section) int  // rows of sec with settingModified
```
- Page keys (only when `pagesOn(m)`; the overlay path is untouched):
  - Either pane: `tab`/`shift+tab` toggles `Pane` (no-op when `pageNarrow(m.Width)`). `1`–`9` → `settingsGoSection(n-1)` (no-op past the last section). `[` / `]` → previous/next section, **wrapping**. `/` → `Searching = true`, `Pane = paneRows`, `preSearch = CursorID`. `esc` → clears a kept search, else closes. `q` closes.
  - `paneSections`: `j`/`k`/`down`/`up` → section ±1, clamped (cursor goes to that section's first row, so the right pane follows). `enter`/`l`/`right` → `Pane = paneRows`. Every other key: nothing.
  - `paneRows`: today's keys, with `j`/`k` running through `settingsAfterIn(settingsPageList(m), …)`. `h` on a row that is not adjustable does nothing (it does not move focus).
  - Search editing (`settingsSearchKey`, page only): `KeyEsc` also sets `CursorID = preSearch` when `preSearch` is still listed. `KeyEnter` keeps the cursor on the match.
- Mouse (page): wheel moves the section on `paneSections` and the row (`settingsPageList`) on `paneRows`.

- [ ] **Step 1: failing tests**:
  - `TestSettingsPagePaneCycle`: open → `Pane == paneRows`; `tab` → `paneSections`; `shift+tab` → `paneRows`; at `Width = 50` `tab` leaves `Pane` unchanged.
  - `TestSettingsPageSectionKeys`: `2` → cursor = first row of `settingsSections[1]`; `]` from the last section → first section; `[` from the first → last; `9` with fewer than 9 sections → unchanged.
  - `TestSettingsPageRowsScoped`: on the first row of Audio, `k` stays; on the last row of Audio, `j` stays (no crossing into another section).
  - `TestSettingsPageLeftPane`: `tab`, `j` → section 2, `Pane` still `paneSections`; `enter` → `paneRows`; `h` and `r` on the left pane change no store value.
  - `TestSettingsPageSearchEscRestores`: cursor on `spinner`, `/`, type `volume`, `esc` → `CursorID == "spinner"`, `Search == ""`. Then `/`, type `volume`, `enter`, `esc` → cursor still on the volume row, `Search == ""`.
  - `TestSettingsPageSearchSpansSections`: `/` `on` → `settingsPageList` holds rows from at least 2 sections.
  - `TestSettingsPageEditAndReset`: cursor on a free-form row, `enter` opens `Edit`; `esc`; `l` on `spinner` changes the value; `r` restores the default.
  - `TestSettingsPageFlagFlipLive`: on the page, set `fullscreen_pages` off through the Features row (`enter`). The next `View()` is the overlay (no `∴ REX › SETTINGS`, board slugs visible behind it), `CursorID` is unchanged, and `tab` now jumps a section (U1 behavior). Turn it back on through the store → the page again.
  - `TestSettingsOverlayKeysUnchanged`: `overlayMode`; `1`, `[`, `]`, `q` change no state; `tab` behaves as `TestSettingsSectionJump` expects.
  - `TestSettingsPageWheel`: `paneSections` + wheel down → next section; `paneRows` + wheel down → next row within the section.
- [ ] **Step 2:** `go test ./internal/surface/tui -run 'TestSettingsPage|TestSettingsOverlayKeys' -v`. Expect FAIL.
- [ ] **Step 3:** implement the fields, the helpers and the `pagesOn` branch in `settingsKey` / `settingsSearchKey` / `updateSettingsMouse`.
- [ ] **Step 4:** `go test ./internal/surface/tui`. Expect PASS (overlay tests included).
- [ ] **Step 5:** commit `feat(tui): settings page panes, section keys and scoped rows`.

---

### Task 3 (R04-2b): settings page two-pane render

**Files**
- Modify `page.go` (pane parts), `settings_view.go` (`renderSettingsPage`), `settings.go` (page hints).
- Test: `settings_page_test.go`, plus goldens `settings_page`, `settings_page_search` (`spin`, cursor `spinner`, `moon` set) and `settings_page_narrow` (50×20 only), all styled.

**Interfaces**
- Consumes: Task 2 helpers; `settingsBody`, `settingsDetail` (unchanged).
- Produces:
```go
type paneItem struct{ Title, Badge string; Dim bool }
func renderSectionPane(items []paneItem, cur int, focused bool, w, h int) []string // header "SECTIONS"; ▸ on cur; cur selectedStyle when focused, styleArrow title when not; badge dim, right-aligned; Dim items styleMuted; windowLines around cur
func sectionPaneWidth(items []paneItem) int                                         // min(longest Title + 1 + Badge + 4, pageSectionsMaxW)
func renderSectionStrip(title string, w int) string                                 // "‹ " + title + " ›" centered, styleArrow arrows
func joinPanes(left, right []string, leftW, h int) []string                         // left padded to leftW, " │ " in colorBorder, h lines
func paneDivider(leftW, w int) string                                               // renderHR(w) with ┴ at the separator column
func settingsPageHints(st *SettingsState, pane pagePane) [][2]string
```
- Layout (`w ≥ 60`): body = `joinPanes(sections, rows)` with height `pageBodyHeight(h) - 1 - len(detail)`, then `paneDivider`, then the `detail` indented 2 columns. Right-pane width = `w - leftW - 3`.
- Rows pane: first line is the section title (`styleBold`), then the rows of `settingsPageList`. While a search is active, use today's grouped list with `sectionHeader`s and the first line `N matches`. With zero hits, show the existing `no setting matches “q”` line. The row format is `settingsBody`'s.
- Section badge: `settingsModifiedIn` when > 0, else `""`.
- Narrow (`w < 60`): `renderSectionStrip` as the first body line, then the rows pane at full width, then the detail. There is no left pane.
- Hints: rows `j/k move · ←→ change · enter toggle · / search · tab sections · 1-9 section · r reset · ? help · esc back`; sections `j/k section · enter rows · tab rows · / search · esc back`; edit `settingsEditHints`; searching `enter keep · esc cancel · ctrl+u clear`. `pageFooter` truncates.

- [ ] **Step 1: failing tests**:
  - `TestSettingsPageTwoPane`: at 120×40 the body has `SECTIONS` and ` │ `; every section title is visible in the left pane; the left pane width is ≤ 24 + 3.
  - `TestSettingsPageBadge`: set `spinner` = `moon` → Appearance's left row ends in `1`; reset → no badge.
  - `TestSettingsPageAllRowsReachable80x24`: for every id in `settingsList(&SettingsState{})`, put the cursor on it. `View()` at 80×24 contains its label untruncated (or truncated with `…` only when the label is longer than the label column), and no line exceeds 80.
  - `TestSettingsPageNarrowStrip`: at 50×20, the line under the header contains `‹ Appearance ›`, there is no ` │ `, and `]` changes the strip to the next section.
  - `TestSettingsPageEmptySearch`: `/` + `zzzz` → `View()` contains `no setting matches`.
  - `TestSettingsPageThemeLive`: cursor `theme`, `l` → in the same Update, no `tea.Cmd` is needed for the restyle; the styled `View()` contains the new palette's `Accent` SGR and not the old one's (`withProfile(termenv.TrueColor, …)`).
  - `TestSettingsPageInspectorFixed`: moving the cursor between a row with a long help and one with a short help keeps the line index of the `paneDivider` the same.
- [ ] **Step 2:** run, expect FAIL.
- [ ] **Step 3:** implement. `renderSettingsPage` replaces Task 1's single list.
- [ ] **Step 4:** `go test ./internal/surface/tui -run 'TestSettingsPage|TestGolden'`. Run `-update` for `settings_page*`, read them, and confirm the overlay goldens are unchanged.
- [ ] **Step 5:** commit `feat(tui): two-pane settings page with section list and inspector`.

---

### Task 4 (R04-3): help page two-pane

**Files**
- Modify `help.go`: state, page layout, keys, mouse. Test: `help_page_test.go`; goldens `help_page`, `help_page_filtered` (`undo`), `help_page_narrow` (50×20).

**Interfaces**
- Consumes: `renderSectionPane`, `sectionPaneWidth`, `renderSectionStrip`, `joinPanes`, `pageHeader`, `pageFooter`, `composePage` (Tasks 1 and 3).
- Produces:
```go
// HelpState gains
Group int
Pane  pagePane

func helpGroups(m Model) []helpGroup // markOff(m, helpLeft(m) + helpLegend(m) + helpRight): Navigation, Filter, Actions, Mouse, Modes, Legend, Session commands, App commands, Attached
```
- Right pane: with no filter, `renderGroup(helpGroups(m)[Group], rightW)` scrolled by `Scroll`. With a filter, `renderColumn(filterGroups(all, terms), rightW)`; the left items with no hits get `Dim: true`; with zero hits, show the existing `no match for “q”` line. It uses the full body height (no inspector). `maxScroll` and `page` come from that right-pane line count.
- Keys (page only): `tab`/`shift+tab` pane (no-op when narrow). `1`–`9` → `Group`. `[`/`]` wrap. Left pane `j`/`k` → `Group` ±1, clamped. `enter`/`l` → rows. Right pane keeps today's scroll keys. Any `Group` change sets `Scroll = 0`. `?`, `q`, `esc` close (esc clears the filter first). `/` → `Filtering`, `Pane = paneRows`.
- Mouse (page): wheel on sections → `Group` ±1; on rows → `Scroll` ±3, clamped.
- Narrow: strip `‹ Navigation ›`, rows pane full width.
- Hints: `j/k scroll · tab groups · 1-9 group · / filter · esc back`; filtering `enter keep · esc clear`.

- [ ] **Step 1: failing tests**:
  - `TestHelpPageEveryGroupReachable`: for `i` in `0..len(helpGroups)-1`, press `strconv.Itoa(i+1)`; `View()` contains that group's title and its first row's desc; the group count is 9.
  - `TestHelpPageScrollResetsOnGroup`: `G` on Navigation (`Scroll > 0` at 80×24), `]` → `Scroll == 0`.
  - `TestHelpPageFilterSpansGroups`: filter `undo` → the right pane has rows from ≥ 2 groups; left items with no hits render muted (styled check: `styleMuted` SGR on `Mouse`).
  - `TestHelpPageFilterEmpty`: filter `zzzz` → `no match for`.
  - `TestHelpPageOffRowsDimmed`: `undo_complete` off → the `:uc` row reads `(off)` in its group.
  - `TestHelpPageLegendSpinner`: on the Legend group, `SpinnerTick` 0 vs 1 gives different `View()`s.
  - `TestHelpPageNarrow`: at 50×20 the strip shows `‹ Navigation ›`, there is no ` │ `, and `tab` is a no-op.
  - `TestHelpPageWheel`: sections pane + wheel down → `Group == 1`; rows pane + wheel down → `Scroll == min(3, maxScroll)`.
  - `TestHelpOverlayKeysUnchanged`: `overlayMode`; `1`, `]`, `tab`, `q` leave `HelpState` and focus unchanged.
- [ ] **Step 2:** run, expect FAIL.
- [ ] **Step 3:** implement. `layoutHelpPage` replaces Task 1's single list.
- [ ] **Step 4:** `go test ./internal/surface/tui`. Update and read the `help_page*` goldens. The overlay `help*.txtar` files stay unchanged.
- [ ] **Step 5:** commit `feat(tui): two-pane help page with group list`.

---

### Task 5 (R04-4): bundled themes

**Files**
- Create `internal/catalog/theme/bundled.go`, `bundled_test.go`, `builtin/dracula.yaml`, `builtin/tokyo_night.yaml`, `builtin/pantone_safe.yaml`.
- Modify `theme.go` (`Get`, `Names`), `catalog.go` (`UserNamesExcept`), `contrast_test.go` (`hexThemes` includes bundled).
- Modify `internal/catalog/settings/registry.go` theme `Options`: append the three names after `ansi`.
- Modify the three option lists to `UserNamesExcept(s.Options)`: `tui/theme_cmd.go:16`, `tui/setup_wizard.go:188`, `cli/setup/theme.go:62`.

**Interfaces (produced)**
```go
//go:embed builtin/*.yaml
var bundledFS embed.FS
// Bundled maps the bundled theme names to palettes parsed from builtin/*.yaml at
// init (panics on a parse error or warning: a bundled file must be clean). Read-only.
var Bundled map[string]Palette
func Get(name string) (Palette, bool)                  // Builtin, then Bundled, else default
func Names() []string                                  // Builtin ∪ Bundled, sorted
func (c *Catalog) UserNamesExcept(known []string) []string // UserNames minus known
```
- `Parse` still refuses only `Builtin` names, so a user `dracula.yaml` parses, and `Catalog.Get` (user first) lets it win. `extends:` still accepts only `Builtin` names. Write that down in `docs/themes.md`.
- `cli/core/styles.go` `resolvePalette` needs no change: a bundled name is not in `Builtin`, so the user directory is read first, then `Get` finds the bundled palette. Pin this with a test.
- `rex theme new dracula` is allowed (it makes an override). Keep `-from` as `Builtin` only.
- Palettes are all `dark: true` and `extends: default`, with only `colors:` set. Sources: dracula and tokyo_night use their published palettes for the state and accent hues. `FgDim`/`FgMuted` are raised until `TestBuiltinContrast` passes (the canonical comment grays fail 4.5:1). `pantone_safe` uses the U4 colorblind-safe pool (`theme.EdgeColors`, `darkEdgePool`): Working `cerulean`, Needs `mimosa`, Failed `tangerine`, Accent `very-peri`, Done from the pool, lightened where contrast needs it. Each YAML line carries a `# <Pantone name>` comment. The tests are the gate, not these hexes.

- [ ] **Step 1: failing tests** in `bundled_test.go` and the existing files:
  - `TestBundledLoad`: `Bundled` has exactly the three names, and each one's `Name` field equals its key.
  - `TestBundledParseClean`: re-`Parse` each embedded file → no error and **no warnings**.
  - `TestBuiltinContrast` / `TestStateHuesDistinct` cover the bundled themes (through `hexThemes`).
  - `TestGetBundled`: `Get("dracula")` → ok, `Bg` ≠ default `Bg`.
  - `TestUserOverridesBundled`: write `dracula.yaml` (`colors: {accent: "#123456"}`, 0600) to a temp dir; `LoadDir(dir).Get("dracula").Accent == "#123456"`.
  - `TestUserNamesExcept`: the user dir holds `dracula.yaml` and `mine.yaml` → `UserNamesExcept(registryOptions) == ["mine"]`.
  - `internal/catalog/settings`: `TestThemeOptionsAreThemes`: every theme `Options` entry is in `theme.Names()`.
  - `internal/surface/tui`: `TestThemeOptionsNoDuplicates`: with the user `dracula.yaml` above, `themeOptions(m)` has `dracula` once.
  - `internal/surface/cli/core`: `TestResolvePaletteBundled`: `HOME` = temp dir with no themes dir → `resolvePalette("tokyo_night").Name == "tokyo_night"`.
- [ ] **Step 2:** `go test ./internal/catalog/... ./internal/surface/... -run 'Bundled|Contrast|Hues|ThemeOptions|UserNames|ResolvePalette'`. Expect FAIL.
- [ ] **Step 3:** implement and tune the YAML hexes until the contrast and hue tests pass.
- [ ] **Step 4:** `go test ./internal/catalog/... ./internal/surface/...`. Expect PASS.
- [ ] **Step 5:** commit `feat(theme): bundle dracula, tokyo_night and pantone_safe themes`.

---

### Task 6 (R04-5): attach test + docs

**Files**
- Modify `attach_cmd.go` `executeAttachCommand`. Test: `attach_cmd_test.go`.
- Docs: `docs/tui.md` (rename "Overlay look", "Help overlay" and "Settings overlay" to page sections, with the pane keys, size classes and flag), `docs/settings.md` (`fullscreen_pages` row text; the theme row lists the 3 new names), `docs/themes.md` (bundled themes table and override rule), `CHANGELOG.md` `[Unreleased]`, `docs/plans/ANTI-fullscreen-settings-help-spec.md` (a `Status: superseded by U1, U2, R04, R05, R06` line at the top), `docs/plans/r2/00-roadmap.md` (R04 status).

**Interfaces**
- No behavior change in `executeAttachCommand`: `settings` / `help` keep falling through to detach, then open (D8). Only tests are added, so the page never renders while `Attach != nil`.

- [ ] **Step 1: failing tests**:
  - `TestAttachSettingsDetachesThenOpens`: flag on, attached model (`bannerModel`), `executeAttachCommand(m, "settings")` → `Attach == nil`, `Focus == FocusSettings`, and `View()` is the page (not the PTY). Same for `help`.
  - `TestAttachSettingsOffDetaches`: `overlayMode`, same call → `Attach == nil`, `Focus == FocusSettings`.
- [ ] **Step 2:** run; these pin existing behavior, so they should PASS once Tasks 1–4 land. If one fails, fix the page code, not the attach path.
- [ ] **Step 5:** write the docs. Use synthetic examples only.
- [ ] **Step 6:** `make check`; raise the floors in `tools/coverage/baseline.txt` if `make cover` shows a rise.
- [ ] **Step 7:** commit `test(tui): pin detach-then-open for settings and help pages; docs`.

---

## Spec coverage
| Spec item | Task |
|---|---|
| Mode (D4), flag, off-is-inert golden | 1, 2 (`TestSettingsOverlayKeysUnchanged`), 4 (`TestHelpOverlayKeysUnchanged`) |
| Chrome: header slots, dividers, footer, border | 1 |
| Settings two-pane, badge, search spans sections, inspector reused | 2, 3 |
| Help two-pane, filter spans groups, empty state, legend live | 4 |
| Keys (D5), `h` no-op, `q` close, wheel (D6) | 2, 4 |
| Size classes, 80×24 reachability, narrow strip | 1, 3, 4 |
| Themes (D2), user override | 5 |
| Attach: detach then open (D8) | 6 |
| Overlay code kept (D7) | 1 (overlay goldens pinned) |
| Docs, ANTI superseded | 6 |
