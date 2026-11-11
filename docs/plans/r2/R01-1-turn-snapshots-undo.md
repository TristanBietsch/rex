# R01-1 Turn snapshots and `:undo`

## Summary
- The daemon snapshots a session's working folder at the start and end of every agent turn, into a **shadow git store** owned by rex. The user's repo, index, branches and stash are never touched.
- `:undo` (TUI) and `rex undo <sel>` restore the folder's files to how they were before the agent's last turn; repeated undo walks further back. `:redo` reapplies.
- Inspired by opencode's snapshots (R01 findings). Feeds R01-2 (diff page). Behind `turn_snapshots` (off).

## Current behavior
- A turn is visible to the daemon as a state change: `working` → `needs_input` (Claude hooks: Stop / Notification; heuristic tools: prompt regex), or → a terminal state. `Store.Transition` (`internal/daemon/state/store.go:233`) and `Store.Subscribe` (`:596`) deliver `Event{Kind, SessionID, NewState, …}`.
- `SessionSummary.CWD` is the session's folder. Several sessions can share one folder.
- Nothing records file changes. A bad turn is undone by hand (`git checkout`, editor undo).
- Git is already a soft requirement: Claude hooks on Windows need Git for Windows.

## Design

### Shadow store
- One bare-style git dir per **folder**, not per session: `~/.local/share/rex/snapshots/<sha256(abs cwd)[:16]>/git`, 0700. Sessions in the same folder share objects (cheap, deduplicated).
- All git calls run the `git` binary with an explicit environment: `GIT_DIR=<shadow>`, `GIT_WORK_TREE=<cwd>`, `GIT_INDEX_FILE=<shadow>/index-<session>`, `GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL=/dev/null` (NUL on Windows), `GIT_TERMINAL_PROMPT=0`, no hooks (`-c core.hooksPath=/dev/null`). This keeps the user's repo, config and hooks out of it.
- Capture = `git add -A` (into the session's private index) + `git write-tree` → a tree id. No commits, no refs moved in the user's repo. rex keeps its own ref per session (`refs/rex/<session>/<n>`) so objects survive gc.
- What is captured (D1: only folders inside a git worktree; elsewhere no capture): tracked + untracked files, honoring the folder's `.gitignore` files and `.git/info/exclude`; untracked files over 2 MiB skipped; the folder's own `.git/` never captured; total per capture capped (D3).
- Ledger: `~/.local/share/rex/snapshots/<key>/turns.jsonl`, one line per capture: `{session_id, turn, phase: "start"|"end", tree, at, files_changed}`.

### When to capture
- **start**: on transition into `working` (and at spawn).
- **end**: on transition out of `working` (to `needs_input` or a terminal state).
- Captures run on one daemon worker per folder (serialized), never on the PTY read path. A capture that takes longer than 10 s is abandoned and logged; the turn is marked "not undoable".

### Undo / redo
- `:undo [sel]`, `rex undo <sel> [--turns N]`: restore the folder to the **start** tree of the last undone turn:
  1. Refuse while the session is `working` ("wait until the agent stops, or press esc to interrupt it first").
  2. Refuse when another **live** session shares the folder (its work would be reverted too), unless `--force` / confirm dialog (D2).
  3. Take a safety capture of the current state first (so undo itself can be redone).
  4. Restore: `git read-tree <start tree>` into a temp index, `git checkout-index -a -f`, then delete files that exist now but not in the start tree and were added by the undone turns (`git diff --name-only --diff-filter=A <start> <current>`). Files ignored by `.gitignore` are never deleted.
- `:redo` / `rex redo <sel>`: restore the safety capture. Redo history is dropped when the agent runs another turn.
- The agent is **not** told (D4); its conversation still remembers the turn. The notice says so and suggests telling the agent ("I reverted your last change").
- Shell side effects (installs, databases, files outside the folder) are not undone. The confirm dialog says this.

### Retention
- Captures for a session are kept until the session is pruned + 7 days, then its refs are deleted and `git gc --prune=now` runs on daemon start at most once a day.
- Hard cap 2 GiB per folder store (D3); oldest sessions' refs dropped first.

## Feature flag
- `turn_snapshots` (off). Off = no captures, no shadow git calls, `:undo`/`:redo`/`rex undo` answer "off: enable in Settings → Features". Turning it off keeps existing snapshots until retention removes them.
- Off-is-inert test in R01-1-2.

## Plan
1. **R01-1-1 `snapshot` package.** `internal/features/snapshot`: `Store{root string}`, `Capture(ctx, cwd, sessionID string, phase Phase) (Tree, error)`, `Restore(ctx, cwd string, to Tree, opts RestoreOpts) (Result, error)`, `Changed(ctx, cwd string, a, b Tree) ([]FileChange, error)`. Git env built in one function `gitEnv(shadow, cwd, session)`. Tests (real `git` in `t.TempDir()`; skip if no git): capture/restore round trip; ignored file untouched by restore; file created by the turn removed; >2 MiB untracked skipped; user repo's `HEAD`, index and refs byte-identical before and after (`TestUserRepoUntouched`); user's global git hooks not run (stub `core.hooksPath` in a fake HOME).
2. **R01-1-2 Daemon wiring + flag.** Subscriber on store events → per-folder worker queue; ledger writes; 10 s cap; flag check. Tests: echo-tool session produces start/end captures; flag off → no shadow dir created; capture failure does not change session state.
3. **R01-1-3 Undo/redo intents.** `Undo{session, turns, force}`, `Redo{session}` (additive) → `UndoResult{restored, deleted []string, safety Tree}`. Tests: refuse while working; refuse with another live session in the folder unless force; safety capture allows redo; redo dropped after a new turn; old daemon → `bad_intent`.
4. **R01-1-4 TUI + CLI.** `:undo`, `:redo`, confirm dialog listing file counts and the "shell effects are not undone" line; `rex undo|redo <sel> [--turns N] [--force] [-o …]`. Goldens for the dialog.
5. **R01-1-5 Retention + doctor.** Ref cleanup, daily gc, size cap; `rex doctor` shows store size per folder and whether `git` is found. Tests: retention removes refs of long-pruned sessions only.
6. **R01-1-6 Docs.** `docs/snapshots.md` (new), `docs/paths.md`, `docs/tui.md`, `docs/cli.md`, SECURITY.md (snapshots hold file contents, incl. untracked secrets like `.env` unless ignored), CHANGELOG.

## Review focus
- Folder is not inside a git worktree: no capture (D1); `:undo` says "snapshots need a git repo"; the flag stays on for other sessions.
- `.env` with secrets that is not gitignored gets copied into the shadow store: documented; store is 0700; `rex doctor` warns when the folder has untracked `.env*` files.
- Agent edits files while a capture runs (turn boundary race on heuristic tools): capture reads a consistent-enough state; restore is always from a finished capture.
- Undo while the user has uncommitted edits of their own in the same files: those edits are in the safety capture; redo brings them back. The dialog shows "N files you may have edited".
- Windows: path case, CRLF (`core.autocrlf=false` in the env), long paths, `git.exe` lookup; `/dev/null` vs `NUL`.
- Submodules and nested repos inside the folder: skipped (not recursed), documented.

## Decisions (answered 2026-11-11)

| # | Question | Answer |
|---|---|---|
| D1 | Which folders | Only folders inside a git worktree. Other folders: no captures, `:undo` explains why. |
| D2 | Another live session in the folder | Refuse unless confirmed (dialog names the other session; `--force` on the CLI). |
| D3 | Size cap | 2 GiB per folder store; oldest sessions' refs dropped first. |
| D4 | Tell the agent | No. The notice suggests telling the agent yourself. |
