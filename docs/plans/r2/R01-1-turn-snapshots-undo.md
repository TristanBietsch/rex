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
4. **R01-1-4 TUI + CLI.** (TUI part deferred: round 1 is restyling the TUI; only the CLI shipped. `:undo` / `:redo` and the dialog goldens come later.) `:undo`, `:redo`, confirm dialog listing file counts and the "shell effects are not undone" line; `rex undo|redo <sel> [--turns N] [--force] [-o …]`. Goldens for the dialog.
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

## Build notes

Built 2026-11-11 on `feat/r01-1-snapshots` (daemon and CLI; TUI deferred).

- **Package** `internal/features/snapshot`: `Store` (`Capture`, `Restore`, `Changed`, plus `Undo`, `Redo`, `PlanUndo`, `PlanRedo`, `MarkRemoved`, `MarkMissed`, `Retain`, `Usage`, `Entries`, `UntrackedEnvFiles`), one `gitEnv` builder, `ErrNotGitWorktree`. Per-folder lock serializes captures, undo, redo and retention on one folder.
- **Daemon**: `boot.Snapshots` subscribes to the store and keeps a queue and one worker goroutine per folder (the goroutine exits when the queue drains). Off: only in-memory state tracking, no goroutine, no file, no git call (`TestSnapshots_FlagOffCreatesNothing`). It follows the setting through the `WatchSettings` applier list, which is now started once after the summarizer block instead of inside it. Retention runs once at daemon start and only while the flag is on (with it off nothing under the store is touched).
- **Protocol**: `Undo{session_id, turns, force, dry_run}`, `Redo{session_id, force, dry_run}`, reply event `UndoResult{session_id, restored, deleted, safety}`. Additive, no `ProtocolVersion` bump.
- **CLI**: `rex undo` / `rex redo`, kit commands, `-yes` without a TTY.
- **Doctor**: size per folder, git lookup, untracked `.env*` warning; creates nothing.

Deviations from the spec:

- `Redo` also takes `force` (otherwise a forced undo could not be redone while the other session lives).
- `Undo` and `Redo` take `dry_run`. The spec has the confirmation list file counts, which needs the plan before the change, so the CLI asks the daemon for a dry run first. A dry run writes an unreferenced tree object (pruned by the daily gc) and nothing else.
- `Capture` appends its own ledger line (phase, turn, files changed) and `Restore` takes `RestoreOpts{Session, From}`; the caller passes a fresh capture as `From`. `Undo` / `Redo` wrap both.
- Turn numbering follows a cursor: a `start` line is numbered one past the current count, which drops undone turns (redo history). A `start` whose capture failed is recorded with no tree, so that turn reports "not undoable" and an earlier turn is never undone in its place.
- A session added already `working` captures its start then. A session added `queued` is captured at its first move into `working`; there is no separate spawn capture.
- Retention clock: the daemon appends a `removed` ledger line when a session leaves the board. A session in a ledger that is not on the board and has no such line (daemon crash) is marked removed at the next retention run.
- The store lives under `<state-dir>/snapshots` (the daemon's `-state-dir`, default `~/.local/share/rex`). `paths.SnapshotsDir()` gives the default for `rex doctor`; a daemon on a custom `-state-dir` keeps its store there and doctor reports the default one.
- Retention and the cap run at daemon start only. `git gc --prune=now` runs during retention at most once a day per store, and after each oldest-session drop while over the cap.
- Ignore rules come from the user's repo, read only: before each capture rex lists what git ignores under the folder (`ls-files -o -i --exclude-standard --directory`) and writes it as anchored patterns into the shadow `info/exclude`. That covers the repo's `.gitignore` files above the folder, `.git/info/exclude` and git's default global ignore file; `core.excludesFile` set in global config is not read (global config is off for every call).
- A nested repository is skipped by excluding its directory (git would otherwise fail on one with no commit, or record a link).
- Not run on Windows here: the null-device and path code compiles and is unit tested; the git-backed tests run wherever `git` is installed (CI Windows has it).

### Review fixes (2026-11-11)

- **Ignore set (HIGH 1), option chosen: read the user's ignore set.** Before each capture the daemon runs, read only and with the repo's own config but `core.fsmonitor=false` and `GIT_OPTIONAL_LOCKS=0`, `git ls-files -z -o -i --exclude-standard --directory` in the session folder, and writes the result as anchored folder-relative patterns into the shadow `info/exclude` (replacing the copy of `.git/info/exclude`). Capture, the restore's delete filter (`check-ignore`) and the verification all see what the user's git ignores, including parent `.gitignore` files for a session in a repo subfolder. The alternative (refuse subfolders) was not needed.
- **Preflight (HIGH 2)**: every write and delete path and each parent is checked with `Lstat` before anything is touched; `BlockedError` lists the blockers, no `--force`. A file that is now a directory is dropped from the session index first so a nested repo or clone is skipped, not added. Deletes go through `os.OpenRoot(cwd)`.
- **Undo flow (MED 3, 4, LOW 13)**: plan (preflight) first, with nothing recorded when it blocks; safety capture; `Guard` (session not working, no overlapping live session) re-checked before the first change and again after; restore under the folder lock, on a context detached from the connection; a second capture is compared with the target (ignored paths left out) and mismatches or a late guard failure become `warnings`. A failure part way says `rex redo` recovers. `CaptureWithin` starts its 10 s limit after the lock is held, and lock waits end with the context.
- **Ledger (MED 5)**: a torn last line is skipped and cut off before the next append; damage elsewhere renames the file `.corrupt-<time>` and starts fresh; appends and rewrites fsync; `Retain` continues past a failing folder and returns the joined errors.
- **Size cap (MED 6)**: removed sessions first; live ones only when nothing else is left and never the newest live one (a store with a live session is never emptied); a live drop is logged and written to `over-cap`, which `rex doctor` shows.
- **Folders (MED 7)**: overlap means same, nested or symlink-equal folders.
- **LOW**: `Retain` does not run while the flag is off; git older than 2.32 disables snapshots (daemon log once, doctor row); dubious ownership has its own error.
- Not done: a tool-written `safe.directory` allowance (the shadow calls ignore global config by design, so such a folder is reported, not snapshotted).

### Second review fixes (2026-11-11)

- **Corrupt ledger**: only the write paths (capture, undo, redo, retention) set a corrupt ledger aside; `Usage`, `Entries`, dry runs and `rex doctor` report it (`ErrLedgerCorrupt`) and change nothing. Setting it aside writes `ledger-reset` (time and file name). A store with a reset marker or a `.corrupt-*` file is never deleted by retention until 7 days after the reset; refs with no ledger session ("orphans") are kept for the same time and are the first size-cap candidates. Undo and redo for a session that lost its history return a `ResetError` naming the file and the time. `rex doctor` shows a row for both states.
- **Deadlines**: the 2 minute limit covers lock wait, plan, preflight and the safety capture. After the guard passes the restore and its verification run on `context.WithoutCancel`, so neither a deadline nor a client disconnect can stop them half way. The guard now runs inside the restore, right after the plan and just before the first delete.
- **Names**: `excludeLine` writes `?` for a newline (over-matches, the safe side). A case-only rename (macOS, Windows) is recognized by lowercase path and `os.SameFile` and is not a blocker.
- **Performance** (`TestCapturePerformanceWithManyIgnoredFiles`, 50,000 individually ignored `*.o` files in 500 folders, one repo, macOS APFS): first capture about 2.8 s, a later capture about 1.5 s, a dry-run plan about 1.5 s. (10,000 files by default: 0.25 s, 0.18 s, 0.17 s; `REX_SNAPSHOT_PERF=full` runs the 50,000-file case). git matches the literal exclude lines quickly, so lines are not collapsed by parent folder.
- `begin` checks the worktree with `rev-parse` only; the ignore listing runs once per capture or plan.
