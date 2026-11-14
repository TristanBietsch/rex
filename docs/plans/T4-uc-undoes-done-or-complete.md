# T4 `uc` undoes done or complete

**Summary**
- `uc` (board chord) and `:uc` mean "undo complete": the selected Done row goes back to Working or Needs you.
  - If the agent is still running (soft mark from `cc`, `:done` or `rex done`), it unmarks (Unretire).
  - If the agent has exited (`C`/`:complete`, a natural exit, failed, or crashed after a daemon restart) and has a resume id, it respawns in place with T3's Resume intent.
  - Otherwise it explains why it can't.
- It also fixes a bug: `:undone` currently toggles, so on an unmarked session it marks it done.

**Current behavior**
- `command.go:92-97`: `case "done", "undone"` both call `toggleRetire` (`retire_view.go:35`). Verified: `:undone` on an unmarked session sends Retire.
- `update.go:323-359`: hand-written `dd`/`cc` blocks. `cc` toggles the mark (`toggleRetire`).
- `u` is unbound on the board.
- Unretire on a reaping session gives `bad_state` (`handlers.go:325-349`), mapped to "too late" (`model.go:359-367`).
- A hard-completed or exited session cannot return to Working at all (no respawn path), and pruned sessions are deleted from disk (`retire.go:47-69`).
- CLI: `rex undone` is unmark-only (`session/done.go:69-100`).
- The screensaver drops pending chords (`screensaver.go:118`).

**Design**
- **Chord.** On top of F1's `chords.go`, add one `boardChords` entry under `"u"` whose completing key is `"c"`.
  - F1's map as described is keyed by a doubled key (`d`, `c`). If its `chordHandler` cannot express "first key u, second key c", extend it with a `second string` field (default: same key). Flag this to F1 now.
  - Precedence: a `c` arriving while `u` is pending completes `uc`. It must not arm `cc`.
  - Any other key cancels, as today.
- **`undoComplete(m, id)`**: new file `internal/surface/tui/undo_complete.go`, shared by the chord, `:uc` and `:resume`.

  | Row | Action | Notice / error |
  |-----|--------|----------------|
  | Not in Done (not marked, live state) | nothing | notice "<name> isn't done" |
  | Marked, live state (queued/working/needs_input) | Unretire (`retireToggleCmd` with `retiring=true`) | "unmarked <name>"; row returns to its live column. Works on old daemons too. |
  | Terminal state (done/failed/crashed) with `ResumeID` | T3 Resume intent, reqID `resume-<id>` | "resuming <name>" (+ id prefix). Row moves to Working (queued → working), then Needs you when Claude's SessionStart/Stop hook reports it. |
  | Terminal, no resume id (non-resumable tool, legacy session) | nothing | `m.Err` "uc: <name> has exited and can't be resumed — start a new session". The mark is not touched, because clearing it would leave the row in Done anyway. |

  - The daemon stays authoritative. A process that exits between keypress and intent is handled inside `handleResume` (live → unmark, dead → respawn).
  - A heuristic `done` session whose process is still alive: Resume only clears the mark. The notice says it is still running but reported done.
- **Pruned or removed session.** Nothing to undo: `:uc <sel>` gives "no match for <sel>" (`resolveLocal`). A row being reaped gives `bad_state`, shown as "too late — session is already being cleared". Recovery after a prune is T3 open question 1.
- **Commands** (`command.go`):
  - `case "uc", "resume"` → `undoComplete`.
  - `case "undone"` → unmark only: if not marked, notice "<name> is not marked done".
  - `case "done"` keeps the toggle (doc: "Mark done / undo"). Alternative: make `:done` mark-only (open question 2).
- **Attach `::uc`** falls through to the board command (existing behavior). No change.
- **Before T3 lands:** step 1 can ship `uc` with only the first two table rows (unmark). The third row is added in step 3.

**Files to change**
- `internal/surface/tui/chords.go` (F1's file): add the `"u"` entry (and the `second` field if needed).
- New `internal/surface/tui/undo_complete.go`: `undoComplete`, using `retirerOf` and T3's `resumerOf`.
- `internal/surface/tui/command.go`: `executeCommand` cases `done`, `undone`, `uc`, `resume`.
- `internal/surface/tui/help.go`: entries (`uc` undo done / resume; `:uc`; `:undone` text).
- `internal/surface/tui/model.go`: `friendlyDaemonError` (the `resume-` prefix; shared with T3).
- No daemon or protocol changes beyond T3.

**Test plan (test-first, TUI unit with the existing `fakeRetirer` plus a new `fakeResumer`)**
- `TestUndone_UnmarkedDoesNotMark`: regression. Fails today (`:undone` marks).
- `TestUC_MarkedLive_Unretires`: `u`, `c` gives `fr.unretired == ["a"]`, `fr.retired` empty, and a notice.
- `TestUC_NotDone_Noop`.
- `TestUC_Exited_WithResumeID_SendsResume` (after T3).
- `TestUC_Exited_NoResumeID_Errs`: no intents sent.
- `TestUC_ChordInterrupted`: `u j c` sends nothing. `u c c`: one uc, and the trailing `c` only arms.
- `TestUC_CommandAliases`: `:uc aa`, `:resume aa`, `:uc nope` gives "no match".
- `TestFriendlyDaemonError_ResumeClearing`.
- Screensaver wake clears a pending `u` (extend the existing test).
- `TestHelpBarHasFiveKeys` unchanged. The help overlay test checks `uc` is listed.
- Integration is covered by T3 (Resume on live, marked, dead and reaping). Add one testutil-daemon TUI e2e (pattern `attach_e2e_test.go`): Complete a long echo-style resumable fake session, `uc`, and the row returns to the Working group.

**Docs, CHANGELOG**
- `docs/tui.md`:
  - KEYS: `uc`.
  - MARK DONE: a `uc` paragraph and the respawn caveat.
  - FOCUS MODES unchanged.
- `docs/slash.md`: `uc`/`resume`; `undone` is unmark-only.
- CHANGELOG:
  - Added: `uc` / `:uc`.
  - Fixed: `:undone` no longer marks an unmarked session.

**Conflict map**
- F1: `chords.go` and `boardChords` (T4 adds one entry; F1 must land first; the possible `second` field is an F1 API detail). F1 removes `update.go:323-359`, so T4 never touches update.go.
- T1 (filter) and T5 (search): none, except the shared `help.go` list.
- U1/U2: help/settings remake rewrites `help.go`. T4's entries are 2-3 lines; whoever lands second re-adds them.
- T3: hard dependency for the respawn row only.
- P1–P3: none, since the CLI is unchanged by default.

**Security notes:** none new. Resume safety is covered under T3. `uc` only sends existing intents for a session already on the board.

**Open questions**
1. Also add `rex uc` as a CLI alias of `rex resume`? Default: no. TUI only; the CLI keeps `rex resume` and `rex undone`.
2. Make `:done` mark-only (symmetry with the fixed `:undone`)? Default: keep the toggle. Only `:undone` is fixed, because it was the surprising one.
3. When `uc` respawns a hard-completed session, should rex also send a prompt (for example "continue")? Default: no. It returns idle (Needs you) after Claude's SessionStart hook.

**Ordered steps (each one commit, `make check` green)**
1. `fix(tui): :undone only unmarks`: command.go, regression test, slash.md, CHANGELOG Fixed.
2. `feat(tui): uc chord and :uc unmark a done session`: chords.go entry, undo_complete.go (unmark rows), help.go, tests, tui.md. Can land before T3.
3. `feat(tui): uc resumes an exited session`: after T3 step 5. Adds the Resume row, `:resume` alias wiring, e2e test, docs, CHANGELOG Added.

**Size:** S (steps 1-2), plus S for step 3 once T3 exists.

### Critical Files for Implementation
- /Users/tristan/Documents/personal/dev/rex/internal/daemon/server/spawn.go
- /Users/tristan/Documents/personal/dev/rex/internal/daemon/state/store.go (with session.go and persist.go)
- /Users/tristan/Documents/personal/dev/rex/internal/daemon/adapter/claude_hooks.go
- /Users/tristan/Documents/personal/dev/rex/internal/surface/tui/board.go
- /Users/tristan/Documents/personal/dev/rex/internal/wire/protocol/events.go
