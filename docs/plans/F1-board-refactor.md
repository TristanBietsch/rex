# F1 board refactor

**Summary**
- Small behavior-neutral refactor that should land before T1, T5 and T4. Sections get stable IDs, there is one composable `rowFilter` in place of the bare `filter string`, and board chords come from a table.
- It turns T1, T5 and T4 into additive changes and gives configurable sections later on a clean seam (`boardGroups` holds IDs and flags; code stops depending on `emptyGroupText` indexes).
- It deliberately does not touch the row grid (`rowCols`/`layoutRows`/`renderRow`, board.go:335-542). I do not recommend a column registry for T2/T3: two new columns fit the existing `rowCols` degrade steps, and T2/T3 can extend them in parallel with no overlap with F1.

**Current behavior**
- `boardGroup` (board.go:18-27) has no ID. Empty texts live in the parallel slice `emptyGroupText` (board.go:103), indexed by position.
- The tool filter string is passed to `filterByGroup` and `partitionSessions` (board.go:111-144) and used in the predicates at board.go:90, 99, 268 and keymap.go:40, 57, 64.
- `setFilter` (keymap.go:227-248) mixes "set the tool" with "reselect and reset scroll".
- Chords are hand-written `if` blocks for `d` and `c` in `updateKey` (update.go:323-359). T4's `uc` must be inserted before the `cc` block to be correct.

**Design**
1. **Section registry**: `boardGroup` gains `ID string` (`"needs"`, `"working"`, `"done"`), `Empty string` (absorbs `emptyGroupText`) and `Completed bool` (Done only). Add `func resolveSection(ref string) (int, error)`: ID, unique prefix, title with spaces ignored, or 1-based number; errors list valid IDs.
   - The `groupNeeds/Working/Done` constants stay for the header and screensaver counts. Making those generic belongs to the future configurable-sections task.
2. **Filter chain** (new `rowfilter.go`):
   ```go
   type rowFilter struct {
       tool   string          // "" / "all": any
       terms  []string        // T5
       hidden map[string]bool // T1
   }
   func rowFilterOf(m Model) rowFilter
   func (f rowFilter) keep(s protocol.SessionSummary) bool
   func (f rowFilter) showsSection(g boardGroup) bool
   func (f rowFilter) rowsActive() bool
   ```
   - F1 fills only `tool`.
   - `filterByGroup(sessions, g, f rowFilter)` and `partitionSessions(sessions, f rowFilter)`. Every caller passes `rowFilterOf(m)`.
   - Split `setFilter` into `setFilter(m, tool)` plus `refilter(m) Model` (scroll reset and keep-or-first selection), which T1 and T5 reuse.
3. **Chord table** (new `chords.go`):
   ```go
   type chordHandler func(m Model, second string) (Model, tea.Cmd, bool) // false: not a chord; handle second normally
   var boardChords = map[string]chordHandler{"d": chordDelete, "c": chordDone}
   func handleBoardChord(m Model, key string) (Model, tea.Cmd, bool)
   ```
   - Algorithm: if a chord is pending, clear it and offer the key to the pending prefix's handler.
     - If that completes a chord, return.
     - Else if the key is itself a prefix, arm it (this keeps today's `c` → `d` behavior).
     - Else clear `Notice` and fall through to the normal switch.
   - T4 adds `"u"`, T1 adds `"z"`.

**Files to change**
- `internal/surface/tui/board.go` (modified):
  - `boardGroup` and `boardGroups` (L18-47); remove `emptyGroupText` (L103).
  - `filterByGroup`, `partitionSessions` (L111-144).
  - `doneCollapsed`, `visibleRows` (L89-100).
  - `groupTitleText` (L268); `renderBoard` (L322).
- `internal/surface/tui/keymap.go` (modified): `toggleDone` (L40), `keepSelectionVisible` (L57, L64), `setFilter` (L227-248) plus new `refilter`.
- `internal/surface/tui/update.go` (modified): `updateKey` L323-359 becomes a call to `handleBoardChord`.
- `internal/surface/tui/rowfilter.go`, `internal/surface/tui/chords.go` (new).
- Tests touched mechanically: `board_layout_test.go` L201 and L213 (`emptyGroupText[...]` becomes `boardGroups[...].Empty`); `rename_retire_test.go` L226 and L236 (`partitionSessions(…, rowFilter{})`).

**Test plan (write the test first)**
- `TestBoardGroupIDsUnique`, and every group has a non-empty `Empty`.
- `TestResolveSection`: table of `needs`, `need`, `3`, `needs you`, `NEEDS`, `x` (error), `0` (error).
- `TestRowFilterToolMatchesLegacy`: on the demo sessions, `partitionSessions` with `rowFilter{tool: t}` gives the same IDs as before, for every entry in `filterTools`.
- `TestBoardChords`:
  - `dd` gives FocusConfirmDelete.
  - `cc` calls the retirer fake.
  - `c` then `d` arms `d`.
  - `d` then `j` moves the selection and clears the chord.
  - A chord key leaves `Notice` alone; a non-chord key clears it.
- All existing TUI tests and snapshot tests pass unchanged except the two mechanical edits above. `go test ./internal/surface/tui/...` plus `make check`.

**Docs / CHANGELOG**
- docs/agents/modules/tui.md: list rowfilter.go and chords.go.
- No user-facing docs, and no CHANGELOG entry (refactor).

**Conflict map**
- T4: if F1 lands first, `uc` is one `boardChords` entry; if T4 lands first, F1 moves T4's block into the table. Tell the T4 planner.
- T2/T3: none. Row-grid lines are untouched, and F1 adds no fields to `SessionSummary` or `applyPatch`.
- S4/S5, U1, U2, U3: none. F1 does not touch header.go, styles.go, prompt.go or registry.go.

**Security notes:** None. Pure refactor; no new input, output or persistence.

**Open questions for the user:** None. This is a mechanical prerequisite.

**Ordered steps** (each step is one commit and passes `make check`)
1. `refactor(tui): give board sections ids, empty text and a completed flag` (board.go, `resolveSection`, test edits + new tests).
2. `refactor(tui): one row filter for every board view` (rowfilter.go, signatures, `refilter`; tests).
3. `refactor(tui): board chords from a table` (chords.go, `updateKey`; chord tests).

**Size:** S

**Recommended overall order:** F1 → T1 → T5, on one branch or as stacked PRs. T2/T3 can run in parallel with all three. T4 should rebase onto F1 step 3.

### Critical Files for Implementation
- /Users/tristan/Documents/personal/dev/rex/internal/surface/tui/board.go
- /Users/tristan/Documents/personal/dev/rex/internal/surface/tui/keymap.go
- /Users/tristan/Documents/personal/dev/rex/internal/surface/tui/update.go
- /Users/tristan/Documents/personal/dev/rex/internal/surface/tui/command.go
- /Users/tristan/Documents/personal/dev/rex/internal/surface/tui/header.go
