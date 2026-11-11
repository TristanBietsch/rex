# T1 Filter board sections / hide completed

**Summary**
- Add a section facet to one shared row filter (built in F1). Sections are matched by stable section IDs, so this keeps working when board sections become configurable.
- New keys: `zh` hides or shows completed sections, `z1`–`z9` hide or show section N, `z0` shows every section. `:filter` gains `hide`, `show` and `only`, and a bare `:filter` clears every filter. The header's chip row becomes a "filter bar" that names what is hidden.
- `board_hidden_sections` (config.yaml) sets which sections start hidden. Toggles only last for the current run, and `:bg` saves them. This also fixes two existing `:filter` / tool-filter bugs.

**Current behavior**
- `internal/surface/tui/command.go:47-51`: `:filter <x>` assigns `m.Filter = args[0]` directly.
  - It does not reselect or reset the scroll (unlike `setFilter`, keymap.go:227-248).
  - It accepts unknown tools silently. The board empties while `SelectedID` still points at a hidden row, so `enter` does nothing.
  - `:filter` with no argument does nothing.
- The tool filter is the only filter there is. It is a bare string threaded through `filterByGroup` (board.go:111-124), `partitionSessions` (board.go:126-144), `doneCollapsed`/`visibleRows` (board.go:89-100), `groupTitleText` (board.go:268), and `toggleDone`/`keepSelectionVisible` (keymap.go:40,57,64).
- Sections are the fixed slice `boardGroups` (board.go:39-47) with index constants `groupNeeds/Working/Done` (board.go:78-82). Their empty-column texts sit in a separate parallel slice, `emptyGroupText` (board.go:103).
- The closest thing to "hiding completed" today is the Done auto-fold: `3` toggles it, and it only applies when Done has more than 5 rows (board.go:84-100, keymap.go:39-48). It resets once the column is short again.
- Bug: the daemon always sends `Snapshot.Filter = "all"` (daemon/server/handlers.go:51). `applyEvent` EventSnapshot (model.go:262-264) and `adoptConnection` (reconnect.go:152-154) adopt it, so a reconnect silently resets the user's tool filter without reselecting. At boot this is harmless because state.restore runs after handshake (splash_steps.go:204-222).
- Header: `showFilterChips` (header.go:61-74) shows the chip row only when more than one tool is in use or a tool filter is set. `headerHitAt` (board_mouse.go:157-182) hit-tests the chips by searching the stripped line for tool names in order.
- tui-state.json (persist.go) holds selection and filter only. It is written by `:bg`, read once, then deleted.

**Design**
1. **Facet**: `rowFilter.hidden map[string]bool` holds the IDs of hidden sections (rowfilter.go from F1), plus `func (f rowFilter) showsSection(g boardGroup) bool`. The Model gets `Hidden map[string]bool`, treated copy-on-write: the only writer is `setSectionHidden(m, id, hide) Model`, which clones the map, because Model is copied by value in Bubble Tea.
2. **Rendering**: a hidden section is left out of the board entirely. `boardSlots` (board.go:234-258) skips it. The gap is inserted "before every rendered section except the first", not `i > 0`, so a hidden first section leaves no leading blank. Navigation, mouse and scroll all read `boardSlots`/`visibleRows`, so they follow with no further changes.
   - Header counts stay global, as with the tool filter today.
   - Hiding the last visible section is refused with `m.Err = "filter: can't hide every section"`.
3. **Selection**: generalize `keepSelectionVisible` (keymap.go:56-78). If `SelectedID` is still a live session but not in `orderedSessions`, select the first visible row (the rule already documented for the fold). This covers rows that move into a hidden section (`cc` while Done is hidden), toggles, and selections restored from disk. Session removal still goes through `neighborOf`.
   - `jumpToSection` (`1`, `2`), `toggleDone` (`3`) and `nextNeedsYou` (`tab`) aimed at a hidden section set a Notice instead, e.g. `Done is hidden — zh to show`.
   - When a session you just spawned (`PendingSelectSlug`, model.go:171) is filtered out, show the Notice `<slug> is hidden by the filter`.
4. **Keys**: a `z` chord added to F1's chord table. This follows the vim fold and ranger `zh` "hidden" conventions, and keeps single letters (`h`/`l`) free for a future horizontal kanban.
   - `zh` toggles every section with `Completed: true` (today: Done).
   - `z1`–`z9` toggle section N in `boardGroups` order. An out-of-range number gives a Notice.
   - `z0` shows every section.
   - While `PendingChord == "z"`, the help line shows `z: h completed · 1-9 section · 0 show all`, in `renderHelpLine` (prompt.go:70-104).
   - Collision check: board keys in use are d c j k g G C r 1 2 3 tab v t n ? S i enter : ctrl+c (update.go:327-441). T4 takes `u` as a chord prefix. `z`, `/` and `esc` are free.
5. **`:filter` extended, not duplicated.** It moves to `filterCommand(m, args)` in new filter.go:
   - `:filter` → clear tool, sections and search (T5) → `refilter`
   - `:filter <tool>` → as today, but through `setFilter`. Accepts `filterTools` plus any `ToolID` on the board; anything else sets `m.Err "filter: unknown tool or subcommand x"`.
   - `:filter hide <sec…>` / `:filter show <sec…|all>` / `:filter only <sec…>`
   - Sections are resolved with F1's `resolveSection`: ID, unique prefix, title (case-insensitive, spaces ignored) or a 1-based number. The words `hide`, `show` and `only` are reserved.
6. **Display**: `showFilterChips` becomes true while anything filters. It is the old rule OR `len(Hidden) > 0` OR (T5) a search is active. The chip row gets a dim suffix from `renderFilterStatus(m)`, e.g. `all · claude · codex …   ·  done hidden (zh)`. Several hidden sections read `needs, done hidden (z0 shows all)`.
   - The suffix sits after the chips, so `headerHitAt` still finds every chip.
   - `padLine` cuts the line to the width.
   - Only existing styles are used (`styleDim`, `styleMuted`, `styleHeaderApp`), so U1 can restyle it freely.
7. **Persistence**:
   - New setting `board_hidden_sections` (TypeString, default `""`, SectionBehavior): comma-separated section IDs hidden at startup. Unknown IDs are ignored. It is read once in the `settingsResultMsg` handler (update.go:172-187).
   - Toggles are session-only and do not write config.yaml. `Store.Save` writes the whole store, which would clobber a concurrent `rex config set`, and a view toggle should not cause a disk write on every key.
   - `:bg` saves the current `hidden_sections` to tui-state.json, and the restore step (which runs after settings.load) overrides the default once.
8. **Bug fixes bundled**: `:filter <tool>` now reselects and validates. The tool filter adopts `snap.Filter` only while `m.Filter == ""`, in `applyEvent` EventSnapshot, `adoptConnection` and the `snapshotResultMsg` handler.

**Files to change**
- `internal/surface/tui/rowfilter.go` (F1, modified): `hidden` field, `showsSection`, `rowFilterOf` fills `hidden`.
- `internal/surface/tui/board.go` (modified): `boardSlots` (L234-258, skip hidden sections and fix the gap); `groupTitleText` (L267-285, count reads `n/total` when a row facet narrows the section).
- `internal/surface/tui/keymap.go` (modified): `keepSelectionVisible` (generalize), `jumpToSection`, `toggleDone`, `nextNeedsYou` (hidden notices).
- `internal/surface/tui/filter.go` (new): `filterCommand`, `setSectionHidden`, `toggleCompleted`, `showAllSections`, `hiddenSectionsFrom(store)`, `renderFilterStatus`.
- `internal/surface/tui/chords.go` (F1, modified): `"z": chordSection` entry.
- `internal/surface/tui/command.go` (modified): `executeCommand` case `"filter"` (L47-51) becomes one line calling `filterCommand`.
- `internal/surface/tui/header.go` (modified): `showFilterChips` predicate; `renderHeader` chip row (L35-37) appends `renderFilterStatus`.
- `internal/surface/tui/prompt.go` (modified): `renderHelpLine` pending-`z` hint.
- `internal/surface/tui/help.go` (modified): new left-column group "Filter & search" (move `t` from Navigation L35, add `zh`, `z1-9`, `z0`); right column `:filter` row (L70) becomes `:filter [tool|hide|show|only …]`.
- `internal/surface/tui/model.go` (modified): `Hidden` field; `applyEvent` EventSnapshot (L262-264) and SessionAdded (L167-178) notice.
- `internal/surface/tui/reconnect.go` (modified): `adoptConnection` (L152-154).
- `internal/surface/tui/update.go` (modified): `settingsResultMsg` (L172-187) seeds `Hidden`; `stateRestoreResultMsg` (L189-204) restores hidden; `snapshotResultMsg` (L226-228).
- `internal/surface/tui/persist.go` (modified): `persistedState.HiddenSections []string \`json:"hidden_sections,omitempty"\``. `LoadTUIState` returns `persistedState`, so `splash_steps.go` `stepStateRestore` (L326-331) and `stateRestoreResultMsg` change with it.
- `internal/catalog/settings/registry.go` (modified): append `board_hidden_sections`.

**Test plan (write the test first in each step)**
- Unit (new `board_filter_test.go`):
  - `TestHideSectionRemovesIt`: hide done, so there is no `DONE` line, it is missing from `orderedSessions`, and the first slot is a title (no leading gap when Needs is hidden).
  - `TestZChords`: `z3`, `z3`; `zh`; `z0`; `z9` gives a Notice; `z` then `j` moves and clears the chord.
  - `TestCannotHideEverySection`.
  - `TestSelectionLeavesHiddenSection`: two cases, a toggle and a live patch with `retire_at` while Done is hidden.
  - `TestJumpToHiddenSectionNotice`: `1`, `3`, `tab`.
  - `TestFilterCommand`: table covering tool reselect, unknown tool error, hide/show/only/prefix/number/title, and bare `:filter` clearing.
  - `TestReconnectKeepsToolFilter`.
  - `TestHiddenSectionsSettingDefault`: `"done, bogus"` gives `{done}`.
  - `TestBGSavesHiddenSections`: `t.Setenv("HOME", t.TempDir())`, then a round trip.
  - Extend `TestFilterChipsOnlyWhenUseful` (hidden alone shows the bar). Extend `TestBoardMouse_FilterChipClick` with hidden + status suffix, so every chip is still hit.
  - `registry_test` passes as is.
- Snapshot (`snapshot_test.go`): add `board-hide-done` and `board-only-needs` cases to `TestBoardSnapshot`. Assert that `View()` has exactly `Height` lines, that no line is wider than `Width`, that the header contains `done hidden`, and that the board lacks `DONE`.
- Help: `TestRenderHelpFits` and `TestRenderHelpGroupTitles` must still pass at 120x50 with the new group.

**Docs / CHANGELOG**
- docs/tui.md: KEYS table (`zh`, `z1-9`, `z0`); new `## FILTER AND SEARCH` section; DESCRIPTION paragraph about the chip row; DETACH section (also saves hidden sections).
- docs/slash.md: `:filter` grammar and examples.
- docs/settings.md: `board_hidden_sections`.
- docs/paths.md L39 and docs/agents/data/schemas.md L108-110: tui-state keys.
- docs/agents/modules/tui.md: `LoadTUIState` signature, filter.go, rowfilter.go.
- CHANGELOG `[Unreleased]`:
  - Added: hide board sections.
  - Fixed: `:filter <tool>` left the selection on a hidden row; a reconnect reset the tool filter.

**Conflict map**
- T4 (`uc`): F1's chord table turns T4's change into one map entry. If T4 lands before F1, F1 folds `uc` into the table. Doing `uc` while Done is hidden makes the row reappear in Working, which needs no extra code.
- T2/T3 (columns): no overlap. T1 does not touch `rowCols`, `layoutRows` or `renderRow` (board.go:335-542). If T3 wants to show the resume ID only in Done, it can key off `boardGroup.Completed` from F1.
- S4/S5 (marquee): both edit `renderHeader` and `headerHeight` (keymap.go:152-161). `headerHitAt` assumes the chip row is header row 1 (board_mouse.go:158). S5 must put the marquee below the chip row or update `headerHitAt`.
- S1–S3: registry.go appends only (trivial merge).
- U1/U2: U1 restyles header.go, prompt.go and help.go. My code uses only existing style variables. With U2, `board_hidden_sections` becomes Lua-editable with no extra work.
- U3/P1–P3: if U3 derives `filterTools` (keymap.go:214) from the registry, `filterCommand`'s tool validation must use the same source.
- A1: none.

**Security notes**
- Section references and setting values are matched only against known IDs and never rendered raw. Unknown setting entries are dropped.
- tui-state.json keeps its 0600 mode in a 0700 directory (persist.go:28-39).
- `:prune` ignores filters (prune.go:18-27). When a filter is active, the confirm text gets "(all Done sessions, filters ignored)" so nobody thinks it only deletes what is visible. This is in `renderDeleteConfirm`.

**Open questions for the user**
1. Should the `zh`/`z` toggles persist on their own, or only through the `board_hidden_sections` default plus `:bg`? Recommended: default + `:bg`, so no config writes on a key press.
2. Should hidden sections disappear entirely, or shrink to a one-line title? Recommended: disappear, with the filter bar saying what is hidden.
3. `zh` (two-key chord) or a single key such as `H` for "hide completed"? Recommended: `zh`, which leaves `h`/`l` free for a future horizontal board.

**Ordered steps** (each step is one commit and passes `make check`; F1 lands first)
1. `fix(tui): :filter <tool> reselects and rejects unknown tools` (filter.go, command.go, test).
2. `fix(tui): keep the tool filter across daemon reconnects` (model.go, reconnect.go, update.go, test).
3. `feat(tui): hide board sections` (rowfilter `hidden`, `boardSlots`, `keepSelectionVisible`, jump notices; tests).
4. `feat(tui): zh / z1-9 / z0 toggle board sections` (chords.go, help line hint, help.go; tests).
5. `feat(tui): :filter hide|show|only, bare :filter clears` (tests).
6. `feat(tui): filter bar names hidden sections` (header.go, mouse test, snapshot cases).
7. `feat(settings): board_hidden_sections startup default; :bg saves it` (registry.go, persist.go, splash_steps.go, update.go; tests).
8. `docs: board filters` (docs + CHANGELOG).

**Size:** M
