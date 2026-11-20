# UP1 Update script

**Summary**
- `tools/update/update.sh` (and `make update`) rebuilds rex from the checkout it lives in, installs the binaries that changed, and moves every running rex process (daemon and boards) onto the new build. Every session keeps running.
- It is safe to run while rex runs: from inside a rex-managed agent session, beside open boards, while hooks exec `rex`, and while another copy of the script runs.
- One job per tool. The script only composes `git`, one `go build`, `cmp`, `mv` and `rex upgrade`. `rex upgrade` owns "make the running processes match the installed binaries". Both converge: a re-run after any failure finishes the job, and a re-run with nothing new changes nothing.

**Current behavior**
- `make install` and `install.sh` `install(1)` each binary over the old one. `install` unlinks then copies, so for a moment `BIN/rex` is missing and a hook or board that execs it fails. Neither touches the daemon (install.sh:306-318, Makefile `install`).
- `rex update -source DIR` stops the daemon first (setup/update.go:171), which kills every running session.
- `rex upgrade` (lifecycle/upgrade.go) has four problems a script cannot work around:
  1. **Any hot refusal becomes a cold restart.** The daemon's refusals ("a restart is already running", "still starting; retry", "new binary refused the handoff") come back as `client.Error`, which `daemonctl.HotRestart` wraps as `ErrHotUnavailable` (daemonctl/hot.go:94-96). `upgrade` then falls back to cold (upgrade.go:86-101). The cold path asks only when `Live > 0` (daemon.go:341), so with no live session it restarts at once, including onto a binary that just failed its preflight.
  2. **Boards are compared with the wrong hash.** A board's hello carries `version.Build()` of `rex` (client/client.go:267). `upgrade` sends the daemon's `rex-daemon` hash to `SwapBoards` (upgrade.go:114). The server skips boards whose build matches (server/hot.go:432), the board skips a swap whose build matches its own (tui/swap.go:270), and the wait loop waits for a match (upgrade.go:131). None can ever match: every board re-execs every time, and every upgrade with a board open waits the full 10 s and reports "still on build".
  3. **Boards are asked while they are away.** A hot restart drops every other connection (server/hot.go:291). The client returns as soon as the new image answers (20 ms polls), while boards redial after 250 ms, 500 ms, 1 s… (tui/reconnect.go:32). `SwapBoards` sees only connected boards, so it usually asks none.
  4. **No idea of "current".** It restarts the daemon even when it already runs the installed build, and never checks that the restart picked the installed build up. The daemon re-execs the path it started from, not necessarily the binary `rex` would start (`daemonctl.FindBinary`: next to `rex`, then `$PATH`).
- `version.Build()` is a lazy `sync.OnceValue` that hashes `os.Executable()` by path (version/build.go:14). Computed after a rename, it hashes the new file while old code runs.
- A board stamps its binary's size and mtime and offers a swap when they change (tui/swap.go:173). Renaming an identical file still changes the mtime.

**Design**

*Part A: `rex upgrade` (lifecycle/upgrade.go)*
1. **Pin builds early.** `version.Build()` is called first thing in `upgrader.run()`, in rex-daemon's `run()`, and in the board before it dials. This means it always hashes the code that is running. Plain `rex` commands don't call it: hashing a 30 MB file on every hook exec costs too much.
2. **Boards follow `rex`.** The target build for boards is `self := version.Build()` of the `rex` running `upgrade`. `SwapBoards`, the board's own check and the wait loop are unchanged and start working.
3. **Ask boards after they are back.** Boards are listed before the restart. After a restart, `upgrade` waits until those PIDs have reconnected, then calls `SwapBoards(self)`, then waits for the swaps. One budget (`boardSwapWait`, 10 s) covers both waits.
4. **`-if-stale`.** `installed := version.FileBuild(daemonctl.FindBinary())`.
   - When `installed != ""` and the daemon's `Info().Build == installed`, the daemon is `current` and is not restarted. Boards on another build than `self` still swap.
   - When nothing is stale, one line: "daemon on build X, N board(s) on build Y: nothing to do", exit 0.
   - An empty build on either side never counts as equal.
5. **`-no-cold`.**
   - Never cold-restart. `ErrHotUnavailable`, or no hot restart on this platform: exit `ExitOperationRefused` (6) with the daemon's reason and the hint `rex upgrade`. Nothing was restarted.
   - Any other hot error (for example, the new image never answered): `ExitGeneric` (1), as today.
   - `-cold -no-cold` together: `ExitInvalidArgs` (5).
6. **Verify, always.** After a restart, when `installed != ""` and the daemon's new build `!= installed`: print the report, then exit `ExitGeneric` with "the daemon is on build X but the installed rex-daemon (<FindBinary()>) is build Y; it re-executes the path it was started from".
7. Exit codes: boards on `old_build` never change the exit code. JSON: `result` gains `current`.
8. Follow-up, not in UP1: the default (no flag) should fall back to cold only when the daemon predates hot restart or the platform has none.

*Part B: the script (`tools/update/update.sh`, POSIX sh)*
- Usage: `update.sh [--pull] [--no-restart] [-v] [-h]`. Env: `PREFIX` (default `$HOME/.local`, as `make install`).
- Shape: `set -eu`; every step is a function; the last line is `main "$@"; exit`. Every command that may fail is written `cmd || die …`, because `set -e` is off inside a function called from `if`, `||` or `&&`.
- Steps:
  1. **Parse** with `for arg in "$@"` (no `shift`), so `"$@"` stays whole for the re-exec.
  2. **Paths.** `SRC=$(CDPATH= cd -P -- "$(dirname -- "$0")/../.." && pwd)`; it must hold `go.mod` and `cmd/rex`, else die naming it. `BIN` is `$PREFIX/bin`, made absolute, created with `mkdir -p`.
  3. **Lock** `BIN/.rex-update.lock`: a symlink whose target is the owner's PID, made with `ln -s $$` (atomic, and the PID is there from the first instant).
     - Names `$$`: ours. We were re-exec'd after a pull; skip the pull.
     - `kill -0 PID` fails: stale. `rm -f`, `ln -s` once more, else busy.
     - Alive: stale only on positive evidence, meaning `ps -p PID -o command=` succeeds and prints a non-empty line without `update.sh` (a reused PID). Anything else is busy: exit 75 (`EX_TEMPFAIL`), naming the lock path to remove.
     - Released by the EXIT trap only when it still names `$$`.
  4. **Pull** (`--pull`, not yet pulled): `git -C SRC pull --ff-only || die`, then `exec sh "$SRC/tools/update/update.sh" "$@"`. `exec` keeps `$$`, so the lock stays ours and tells the new script the pull is done. A second run cannot pull new sources under a running build.
  5. **Build** into `STAGE=BIN/.rex-stage`: `rm -rf` and `mkdir` it under the lock (no `mktemp`, no sweep). `VER=$(git -C SRC describe --tags --always --dirty 2>/dev/null || echo dev)`. One call: `go -C "$SRC" build -trimpath -ldflags "-X github.com/tristanbietsch/rex/internal/version.Version=$VER" -o "$STAGE/" ./cmd/rex ./cmd/rex-daemon`, output to `STAGE/build.log`. The log is shown (last 20 lines) on failure, and streamed with `-v`.
  6. **Smoke.** `STAGE/rex --version` and `STAGE/rex-daemon -version` must exit 0 (output discarded). This also catches a stray `GOOS`/`GOARCH` (exec format error) before anything is installed.
  7. **Report** `SRC`, branch and `VER` before touching `BIN`, so a run from an agent's worktree says what it installs.
  8. **Install, per file**, `rex-daemon` first: `cmp -s` equal to the installed one → leave it (no new mtime, so no board is nudged). Otherwise `chmod 0755` and `mv -f` into `BIN`. INT/TERM/HUP are ignored for the loop, then the normal traps come back.
  9. **Switch** (unless `--no-restart`): `"$BIN/rex" upgrade -o text -no-cold -if-stale` with INT ignored, because the restart cannot be undone halfway. Exit 0: done. Exit 4 (`ExitDaemonUnreachable`): not running, done; the next `rex` starts the new daemon. Anything else: the script exits 3.
  10. **Shadow check.** `[ "$(command -v rex)" -ef "$BIN/rex" ]` fails: warn that `$PATH` finds another copy first.
- Output formats are explicit flags (`-o text`, `-o txt`), never `REX_OUTPUT`/`REX_AGENT`. That way an agent's `CLAUDECODE` changes nothing the script reads, and nothing leaks into the environment.
- Traps: `trap cleanup EXIT; trap 'exit 130' INT; trap 'exit 143' TERM; trap 'exit 129' HUP`. `cleanup` removes `STAGE` and releases the lock. The explicit `exit` in each signal trap makes dash run the EXIT trap too.
- Exit codes: 0 converged, 1 failed with nothing new installed, 2 usage, 3 installed but the daemon is not on the new build (the reason is printed above; re-run or follow the hint), 75 another update is running.
- Log: one line per step to `${REX_LOG_DIR:-$HOME/.local/state/rex}/update.log` (made 0700): time, PID, step, `SRC`, versions. Never the build output.

*Part C: `make update`*: `@PREFIX="$(PREFIX)" sh tools/update/update.sh $(UPDATE_FLAGS)`.

**Why it works while rex runs**
- **Binaries in use.** `mv` within one directory is `rename(2)`: a running daemon, board or hook keeps its old inode, and a new exec gets the whole old file or the whole new one, never a missing or half-written one. Writing in place would be worse than a gap: on macOS the kernel kills a process whose signed pages change under it, and Linux refuses with `ETXTBSY`. Staging inside `BIN` keeps the rename on one filesystem, so it is atomic and copy-free.
- **Run from inside a rex session.** A hot restart keeps every agent's process and PTY, so the agent running the script, and the script itself, survive. `-no-cold` guarantees the script can never kill its own session; a needed cold restart is left to a human at a terminal.
- **The script is replaced under itself.** `git pull` writes new files (unlink, create), so the running sh keeps reading the old inode. `main "$@"; exit` on one line also covers an in-place edit. The pulled version takes over by re-exec.
- **Two copies at once.** The lock makes the second exit 75. Without it, two checkouts could interleave their renames and install a mixed pair.

**Performance**
- One `go build` for both binaries: packages load once and compile in parallel. Warm cache: link-bound, ~1-2 s.
- Built straight into the staging folder in `BIN`: install is at most two renames, no copy.
- `cmp -s` stops at the first differing byte. A no-change run is a build, two compares and one `upgrade -if-stale` (a hello, a board list and a file hash).
- An unchanged binary is not renamed, so boards are not nudged. Current boards are not re-exec'd, and `upgrade` no longer waits 10 s for a match that cannot happen.
- No sleeps in the script. Network only for `--pull` or a cold module cache.

**Edge cases**
| Case | Behavior |
|------|----------|
| Daemon not running | Install only; `upgrade` exits 4; script exits 0. |
| Everything already current | Nothing renamed, nothing restarted: "nothing to do". |
| Only `rex` changed | Only `rex` renamed. Daemon `current`, boards swap. |
| Only `rex-daemon` changed | Only it renamed. Daemon hot-restarts; boards already on `self` are not asked. |
| Restart already running / session still starting | Refused with the daemon's reason; exit 3. Binaries installed; a re-run only switches. |
| New `rex-daemon` fails the daemon's preflight | Refused; the old daemon keeps serving; exit 3. Smoke step 6 makes this rare. |
| Daemon predates hot restart / platform has none | Refused; exit 3; hint: run `rex upgrade` in a terminal (it asks before a cold restart). |
| Daemon started from another path than `BIN` | Hot restart re-execs that path; verify fails naming the installed path; exit 3. Every re-run repeats it until the daemon is restarted from `BIN`. |
| New image never answers after the exec | `upgrade` exits 1 (not "nothing restarted"); script exits 3. |
| Board started from another path, or closed during the upgrade | Reported `old_build` after the wait; does not change the exit code. |
| A cold-started daemon or new board races the rename | Its build is pinned at startup (A1), so it reports the code it runs. |
| Build fails / smoke fails | Nothing installed; exit 1. |
| `--pull` not fast-forward / no upstream | git's message; exit 1, before any build. |
| Ctrl-C | Before install: nothing installed. During install and `upgrade`: ignored. |
| `SIGKILL` | The lock names a dead PID: reclaimed. `STAGE` is recreated by the next run. |
| `ps` missing or denied (sandbox, busybox) | A live lock holder counts as busy: exit 75, never a takeover. |
| Stale lock reclaimed by two runs at the same instant | Both may run. Accepted: it needs a killed run plus two starts in the same moment. |
| Run from an agent's worktree | Builds and installs that worktree; step 7 names it. |
| `BIN/rex` is a symlink | Replaced by a file, like `make install`. |
| Windows | Not supported (sh). Use `install.ps1 -FromSource`, then `rex upgrade`. |

**Non-goals**
- Release installs: `rex update` + `rex upgrade` cover them.
- `rex update -source` hot-restarting instead of stopping the daemon (follow-up).
- `install.sh` / `make install` renaming atomically (follow-up, same staging trick).
- The default `rex upgrade` cold fallback on transient refusals (A8, follow-up).
- A `daemon_exe` snapshot field to refuse a path mismatch before restarting (the verify message covers it).

**Tests**
- `lifecycle/upgrade_test.go` (fakes):
  - Boards are swapped and awaited against `self`.
  - A board that is absent right after the restart and back later still gets swapped.
  - `-no-cold`: never calls `restart`; exit 6 for `ErrHotUnavailable`, exit 1 for other errors; `-cold -no-cold` exits 5.
  - `-if-stale`: skips a current daemon, still swaps stale boards, prints "nothing to do" when nothing is stale; an empty build never counts as current.
  - Verify fails when the build after the restart is not the installed one.
- `tools/update/update_test.go` (skipped on Windows; runs under `/bin/sh`, and also `dash` when present):
  - Fixture: a temp `HOME`, `REX_LOG_DIR` unset, a temp `SRC` (copied `update.sh`, a `go.mod`, `cmd/rex/`, `cmd/rex-daemon/`), and stub `go` and `git` first on `PATH`. The stub `go` writes stub `rex`/`rex-daemon` scripts whose contents come from the fixture; they log their argv and take their `upgrade` exit code from a state file.
  - Cases: fresh install; no-change run renames nothing (`os.SameFile` before and after) and calls `upgrade -if-stale -no-cold`; only one binary changed; `--no-restart`; `upgrade` exit 4 → script exit 0; `upgrade` exit 6 → script exit 3; build failure and smoke failure install nothing and leave no `STAGE`; live lock (`sh -c 'sleep 60' update.sh`) exits 75; dead-PID lock is reclaimed; `--pull` re-execs the pulled script (the stub `git pull` rewrites it with a marker) and keeps the lock.

**Docs**
- CONTRIBUTING: `make update` next to `make install`. docs/cli.md and docs/daemon.md: `rex upgrade -if-stale -no-cold`, boards swapped after they reconnect. CHANGELOG: Unreleased.

**Revisions**
- *Pass 1.*
  - Moved the staleness test and the verify step from the script into `rex upgrade -if-stale` (no JSON parsing in sh) and dropped the `rex daemon status` change.
  - Added `-no-cold`: a transient refusal used to become a cold restart that could kill the script's own session.
  - Fixed the board hash mismatch.
  - Replaced env-var formats with `-o` flags.
  - Took the lock before the pull, so the re-exec keeps it.
  - Gave the lock and the staging area different names; the sweep used to delete the lock.
  - Added PID-reuse detection, `|| die` instead of `set -e`, signal traps that also run cleanup under dash, the daemon smoke test, an absolute `PREFIX`, `CDPATH`-safe paths and the worktree report.
- *Pass 2.*
  - `SwapBoards` now runs after the boards have reconnected (they are dropped by the restart).
  - Builds are pinned at startup, so a lazy hash cannot see the new file.
  - Install goes per file, so an unchanged binary keeps its mtime.
  - `-no-cold` exits 6 only when nothing restarted.
  - The lock fails closed when `ps` cannot tell.
  - The `REX_UPDATE_PULLED` variable is gone: the lock naming `$$` is the signal.
  - Logs go to `REX_LOG_DIR` like the rest of rex.
  - Cut the status pre-step (`upgrade` exit 4 says "not running"), `mktemp` and the sweep (fixed `STAGE` under the lock), `--force`, and the `env -u`/`GOWORK` isolation (the smoke test catches a cross build).
  - Shadow check uses `-ef`.
  - Tests run on a copied `SRC` with a temp `HOME`.
