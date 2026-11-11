# Turn snapshots and undo

rex can snapshot a session's folder at the start and end of every agent turn, so you can put the files back the way they were before a bad turn: `rex undo <sel>`. It is **off by default**. Turn it on in Settings → Features (`turn_snapshots`) or with `rex config set turn_snapshots true`. Read [what it stores](#what-it-stores) first.

## Use it

```sh
rex config set turn_snapshots true     # once; the daemon notices within about 5 seconds
rex undo fix-auth                      # restore the folder to before the agent's last turn
rex undo fix-auth --turns 2            # two turns back
rex redo fix-auth                      # bring back what undo replaced
```

`rex undo` asks first and lists how many files it will restore and delete, then adds a reminder that shell side effects are not undone. Without a terminal it needs `-yes`. See [rex undo](cli.md#rex-undo--rex-redo) for every flag and exit code.

Repeated `rex undo` walks further back: each call goes one more turn (or `--turns N` more) before the last. `rex redo` pops the most recent undo. Redo history is dropped as soon as the session starts another turn.

The agent is **not told**. Its conversation still remembers the turn you undid, so tell it ("I reverted your last change") before you continue.

## What counts as a turn

The daemon watches session state. A turn **starts** when a session moves into `working` and **ends** when it leaves `working` (to `needs_input`, `done`, `failed` or `crashed`). It takes one capture at each edge. A session added already `working` gets its start capture then. Captures run on one worker per folder, never on the terminal read path, and are abandoned after 10 seconds; that turn is then not undoable (`rex undo` says so) instead of an earlier turn being undone in its place. A failed capture never changes a session's state.

## What undo does

1. Refuses while the session is `working`: wait until the agent stops, or press `esc` to interrupt it.
2. Refuses when **another live session** (not done, failed or crashed) works in the same folder, because its work would be reverted too. `--force` goes ahead.
3. Takes a **safety capture** of the folder as it is now, so the undo can itself be redone. Your own uncommitted edits are in it.
4. Restores: files the target state has are written; files that exist now but not in the target are deleted, except files that your `.gitignore` files or `.git/info/exclude` ignore, which are never deleted. Empty folders left behind are removed.

Only the files inside the session's folder come back. Shell side effects are **not** undone: installed packages, databases, network calls, files outside the folder.

## What it stores

- **Only folders inside a git worktree** are captured. Elsewhere nothing is captured and `rex undo` answers "snapshots need a git repo". The setting can stay on for other sessions.
- Tracked files **and untracked files**, honoring the folder's `.gitignore` files and `.git/info/exclude` (a global gitignore is not read). Untracked files over 2 MiB are skipped. The folder's own `.git` is never captured; nested repositories and submodules are skipped, not entered.
- Snapshots hold **file contents**. An untracked `.env` with secrets that git does not ignore is copied into the store. The store is created owner-only (0700) and never leaves the machine, but anything that can read your home folder can read it. `rex doctor` warns about untracked `.env*` files in snapshotted folders. Add them to `.gitignore`.
- Nothing is written to your repository: not `HEAD`, the index, branches, stash, hooks or config. A test compares the whole `.git` folder before and after.

## How it works

One shadow git directory per **folder** (sessions in the same folder share objects):

```
~/.local/share/rex/snapshots/<sha256(abs cwd)[:16]>/
  git/            shadow GIT_DIR (bare), with a private index per session
  turns.jsonl     ledger, one line per capture
  cwd             the folder this store belongs to
```

Every git call runs the `git` binary with an explicit environment: `GIT_DIR` the shadow directory, `GIT_WORK_TREE` the folder, `GIT_INDEX_FILE` the session's own index, `GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL` the null device, `GIT_TERMINAL_PROMPT=0`, and `-c core.hooksPath=<null device> -c core.autocrlf=false`. The shadow's `info/attributes` switches off line-ending conversion and filters, so a captured file is byte for byte what is on disk. Any `GIT_*` variable of the daemon's own environment is dropped.

A capture is `git add -A` into the session's index and `git write-tree`: a tree id, no commits. Each capture is kept alive by a ref `refs/rex/<session>/<n>` in the shadow repository.

The ledger line is `{"session_id", "turn", "phase", "tree", "at", "files_changed"}`. `phase` is `start`, `end`, `safety` or `pre-redo` for a capture, and `undo`, `redo` or `removed` for an operation. See [schemas](agents/data/schemas.md#turn-snapshot-ledger).

## Retention

- A session's captures are kept while it is on the board and for **7 days** after it was removed, then its refs are deleted.
- A store is capped at **2 GiB**. Over the cap, the oldest sessions' refs are dropped first (the daemon runs `git gc` to reclaim space).
- `git gc --prune=now` runs on each store at most once a day, when the daemon starts.
- A store with no sessions left is deleted.
- Turning the setting off keeps existing snapshots until retention removes them. With it off, rex makes no git call and creates no file.

Retention runs when the daemon starts, so a long-running daemon applies it at its next restart.

## Check it

`rex doctor` shows whether snapshots are on, whether `git` is found, each folder store's size and session count, and untracked `.env*` files in snapshotted folders.

## Not in this version

- The TUI `:undo` / `:redo` dialog. Use `rex undo` / `rex redo` for now.
- Windows is built and unit tested (null device `NUL`, path handling), but the git-backed tests run where `git` is installed.
