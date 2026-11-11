# T5 Search

**Summary**
- `/` opens an editor in the bottom bar that filters the board live as you type. Rows must contain every whitespace-separated term (case-insensitive substring). The terms are checked against what the board shows plus identifiers.
- `enter` keeps the search, `esc` while editing restores the previous query, and `esc` on the board clears it. The header filter bar shows `/query · N matches`. `:search <text>` and `:search` (clear) cover scripting and use from attach via `::search`.
- A search is the same row filter as T1: a "terms" facet in F1's filter chain. It is session-only, and `:bg` saves it.

**Current behavior**
- There is no search. `/` and `esc` are unbound on the board (update.go:361-441).
- Every board view already shares one path: `orderedSessions` → `visibleRows` → `filterByGroup` (keymap.go:10-16, board.go:93-124). After F1, that path takes a `rowFilter`.
- The bottom-bar inputs follow two patterns:
  - `FocusPrompt`/`FocusCommand`: text fields on Model, edited in `updatePromptKey` and `updateCommandKey` (update.go:532-563, 631-656).
  - `FocusRename`: ctrl+u / ctrl+w editing (rename.go:45-116), rendered by `renderPrompt`/`renderHelpLine` (prompt.go).
- The cursor blinks only for the foci listed in `cursorBlinking` (events.go:98-110). The screensaver only starts while `Focus == FocusBoard` (screensaver.go:110). Mouse presses are dropped outside the board and settings foci (board_mouse.go:49-71).
- The Done column folds when it has more than 5 rows (board.go:89-91), which would hide search hits.

**Design**
1. **Matching** (new search.go):
   - `compileQuery(q string) []string`: lower-case, `strings.Fields`, at most 16 terms.
   - `sessionMatches(s, terms) bool`: every term must be a substring of a haystack built from `displayName(s)`, `s.Slug`, `s.ShortID`, `rowText(s)` (task, or the question line when it needs you), `rowDescription(s)` (AI summary), `s.ToolID`, `modelEffortLabel(s)`, `s.Fleet` and `filepath.Base(s.CWD)`, each passed through `protocol.FlattenDisplayText`.
   - This matches what either view shows, plus the IDs. Raw `LastLine` is excluded because it is spinner noise.
   - No regex and no fuzzy matching. AND of substrings is predictable and costs O(n·len).
2. **Facet**: `rowFilter.terms`, checked in `rowFilter.keep`. `rowFilterOf(m)` compiles `m.Search` once per call.
3. **Editor**:
   - Append `FocusSearch` to the Focus enum (model.go:19-33). Add `Model.Search string` and `searchOrig string`.
   - `/` on the board: set `searchOrig = Search` and `Focus = FocusSearch`. The editor opens prefilled when a search is already active.
   - `updateSearchKey`:
     - Runes and space append, after dropping control runes (pasted escapes) and capping at 128 runes.
     - backspace, ctrl+u and ctrl+w edit, reusing `dropLastRune`/`dropLastWord`.
     - Every edit calls `refilter(m)` (F1): reset the scroll, keep the selection if it is still visible, else select the first match.
     - `enter` returns to the board and keeps the query. An empty draft means the search is off.
     - `esc` restores `searchOrig` and refilters.
   - Board `esc` with a search active clears it. With no search, `esc` does nothing, as today.
4. **Interplay**:
   - The Done fold is suspended while a search is active (`doneCollapsed` gets `&& m.Search == ""`), so hits are never folded away.
   - Search combines with hidden sections (AND). The filter bar adds `+N in hidden sections (z0)` when matches are hidden by T1.
   - An empty section shows `no matches` instead of `emptyGroupText`/`Empty` while a search is active.
   - Section titles read `n/total` (shared with T1).
   - Header counts stay global.
5. **Display**:
   - While editing, the bottom line reads `/ query▌   3 matches`. The query is tail-truncated as in `renderRenamePrompt` (rename.go:143-160) and sanitized.
   - The help line reads `enter keep · esc cancel · ctrl+u clear · ctrl+w delete word`.
   - After `enter`, `renderFilterStatus` (T1) adds `/auth · 3 matches · esc clear`, with the query shown through `truncate(…, 24)`.
   - No match highlighting inside rows: it would change `renderRow`/`cell` truncation, which T2/T3/S1–S3/U1 all touch.
6. **Commands**: `:search <text…>` sets the query to the rest of the line, like `:name` (command.go:90). `:search` alone clears it. `:filter` alone also clears it (T1).
7. **Persistence**: session-only. `:bg` saves `search` to tui-state.json and the next start restores it once, before the selection is restored.

**Files to change**
- `internal/surface/tui/search.go` (new): `openSearch`, `updateSearchKey`, `compileQuery`, `sessionMatches`, `searchHaystack`, `renderSearchPrompt`, `searchHelpLine`, `searchMatchCount`.
- `internal/surface/tui/rowfilter.go` (modified): `terms`, `keep`, `rowsActive`.
- `internal/surface/tui/model.go` (modified): `FocusSearch` appended to the enum; `Search`, `searchOrig` fields.
- `internal/surface/tui/update.go` (modified): `updateKey` routes `FocusSearch` next to the `FocusPrompt` branch (L318-321); board switch adds `case "/"` and `case "esc"` (L361-441).
- `internal/surface/tui/events.go` (modified): `cursorBlinking` (L100) adds `FocusSearch`.
- `internal/surface/tui/prompt.go` (modified): `renderPrompt` case `FocusSearch`; `renderHelpLine` `FocusSearch` block.
- `internal/surface/tui/board.go` (modified): `doneCollapsed` (L89-91); `renderBoard` slotNone text (L321-322).
- `internal/surface/tui/filter.go` (T1, modified): `renderFilterStatus` search segment and hidden-hits hint.
- `internal/surface/tui/command.go` (modified): `executeCommand` adds `case "search"`.
- `internal/surface/tui/persist.go` (modified): `Search string \`json:"search,omitempty"\``; update.go `stateRestoreResultMsg` restores it.
- `internal/surface/tui/help.go` (modified): `/` and `esc` rows in the "Filter & search" group; `:search [text]` row.
- `internal/surface/tui/board_bench_test.go` (modified): add `BenchmarkRenderBoard_200_Search`.

**Test plan (write the test first in each step)**
- Unit (new `search_test.go`):
  - `TestSessionMatches`: table covering each haystack field, case-insensitivity, multi-term AND, no match, empty query matching all, and session text containing ESC/newline/bidi runes.
  - `TestSearchEditorLive`: `/`, then typing narrows `orderedSessions` live; backspace, ctrl+u, ctrl+w; `enter` keeps; `esc` restores the previous query; `/` reopens prefilled; board `esc` clears.
  - `TestSearchSelection`: the selection stays when visible, else moves to the first match; zero matches leaves `SelectedID == ""`.
  - `TestSearchUnfoldsDone`: 7 Done rows, the query hits `done-6`, and it is visible and navigable.
  - `TestSearchWithHiddenSectionHint`.
  - `TestSearchInputSanitized`: control runes dropped, 128-rune cap.
  - `TestSearchCommand`.
  - `TestBGSavesSearch` (with `HOME` set to a temp dir).
  - `TestSearchCursorBlinks`.
  - `TestNoScreensaverWhileSearching`.
  - `TestRenderHelpCommandVerbs` gains `"search"`.
- Snapshot (`snapshot_test.go`): add `board-search-editing` and `board-search-active` cases. Assert the bottom line has `/ auth` and `matches`, the header has `/auth`, and non-matching slugs are absent. Every line is exactly `Width` wide after `padLine`, and the view is exactly `Height` lines.
- Bench: compare the 200-row render with and without a query; aim for under 2x.

**Docs / CHANGELOG**
- docs/tui.md: KEYS (`/`, `esc`), FOCUS MODES table (Search row), FILTER AND SEARCH section (fields searched, AND semantics, Done unfold), DETACH (search saved).
- docs/slash.md: `:search`.
- docs/paths.md and schemas.md: `search` key.
- CHANGELOG `[Unreleased]` Added: board search.

**Conflict map**
- T3 (resume ID): add the new `SessionSummary` resume-ID field to `searchHaystack`, one line. Whichever task lands second adds it.
- T2: none. Timestamps are not searched.
- S1–S3: the haystack calls `rowText`/`rowDescription`, so a verbosity change follows automatically. If S-tasks rename those helpers, update search.go.
- T4: none beyond F1's chord table. `/` and `esc` are plain keys in the board switch.
- S4/S5: share the header chip-row and `headerHeight` concerns from T1.
- U1: restyles prompt.go and help line. Only existing styles are used.
- Focus enum: append-only. Any other task adding a Focus should also append at the end.
- T1: same planner, same files (filter.go, rowfilter.go, persist.go). Land T5 after T1 on the same branch.

**Security notes**
- The query is user input that gets echoed back. Control runes are dropped on input, and the query is rendered only through `truncate`/`FlattenDisplayText`.
- Session text being matched is untrusted agent output. It is only matched, never re-rendered by this code, and there are no regexes.
- Length and term caps bound the CPU cost.
- Log the query at Debug only, never Info (the rename logging precedent, rename.go:77-78).
- tui-state.json is 0600 and may contain the query, which is the user's own text.
- `:prune` warns that it ignores filters (shared with T1).

**Open questions for the user**
1. When Done is hidden, should search still look inside it? Recommended: no (filters combine), with a `+N in hidden sections` hint.
2. Case: always insensitive, or smart-case (an uppercase letter makes it case-sensitive)? Recommended: always insensitive.
3. Highlight matched text in rows? Recommended: not in v1; revisit during U1.

**Ordered steps** (each step is one commit and passes `make check`; after F1 and T1)
1. `feat(tui): search terms facet for the board filter` (`compileQuery`, `sessionMatches`, `rowfilter.terms`; unit tests; nothing wired to keys yet).
2. `feat(tui): / searches the board live` (FocusSearch, editor, board `esc`, `cursorBlinking`, prompt/help line; tests + snapshot).
3. `feat(tui): show search state and unfold Done while searching` (filter bar segment, title counts, no-matches text, hidden hint; tests).
4. `feat(tui): :search command; :bg saves the search` (tests).
5. `docs: board search` (docs, help overlay rows, CHANGELOG, bench).

**Size:** M
