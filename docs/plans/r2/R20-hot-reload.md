# R20: hot reload, hot swap and hot restart

Date: 2026-11-12. Status: **spec, decisions taken with the recommended option** (user rule: pick the recommended default, record it). Plan: [R20-00-plan.md](R20-00-plan.md). Number: R19 is left for the READY edge-case work (`fix/ready-edge-cases`).

User request (verbatim): "make the reload option usable and hot swappable, and rex can restart live or reload configs while in use."

The hard constraint: the user's agent sessions (including the Claude session that orchestrates rex work) are PTY children of the live `rex-daemon`. Upgrading or restarting rex must never kill or disturb them.

## 1. Today (base `integration/todo` @ dd943b5)

### `rex daemon restart` kills every running session

Evidence, read from code (not tried on the live daemon):

1. `lifecycle.daemonCtl.restart` (`internal/surface/cli/lifecycle/daemon.go:271`) calls `stop`, which sends a `Shutdown` intent (`daemonctl.ShutdownDaemon`).
2. `handleShutdown` (`internal/daemon/server/handlers.go:423`) calls `cfg.OnShutdown`, which is `stop` of the daemon's `signal.NotifyContext` (`cmd/rex-daemon/main.go`): the root context is cancelled.
3. Every session's supervisor context derives from it: `handleNewSessionIntent` passes `c.ctx` (the serve context, `server.go:426`) to `spawnSession` → `startSupervisor` → `context.WithCancelCause(ctx)`.
4. `Supervisor.Run` (`internal/daemon/pty/supervisor.go`, `case <-ctx.Done()`) calls `killAndReap`: `SIGKILL` to the child's whole process group (`unixConsole.KillTree`, `kill(-pid, SIGKILL)`), then the session goes to `failed`. `exec.CommandContext` would kill the child on the same cancel as well.
5. The new daemon remaps anything persisted as `queued`/`working`/`needs_input` to `crashed` (`docs/daemon.md`: "child processes do not survive daemon exit").

So: yes, every agent dies with SIGKILL mid-turn. Only sessions with a known `resume_id` can be brought back with `Resume`, as a new process.

### Reload

- `rex reload`, `:reload` and `SIGHUP` re-read `tools.yaml` and `config.yaml` (+ `config.lua`) in the daemon (`boot.ReloadAll`). The TUI's `:reload` also re-reads `config.yaml`, themes and `config.lua` in the board.
- Not reloaded: `init.lua` (the daemon's Lua event runtime, `boot.StartLuaRuntime`, loaded once), the model cache only on `cat.Reload(..., true)` (already forced), workspace `config.lua` is part of the settings layer.
- Feedback: `rex reload` prints "reloaded tools.yaml" or an error string; nothing says what changed or what still needs a restart.
- The settings registry `Apply` class is not always true (audit in §5).

### Board restart

There is no in-process restart. `rex --quiet-boot` / `REX_QUIET_BOOT=1` (commit 413ef39) exists for scripted relaunches; the hourly relaunch that uses it lives outside the repo (not found in the repo, launchd or crontab). A relaunch loses the open page, attach popup and scroll position; `tui-state.json` keeps selection, filter, hidden sections and search only.

## 2. Goals

1. One reload action that reloads every layer that can change live and says what changed, what needs a restart, and errors with `file:line`.
2. A board can replace itself with a newly installed binary without losing its view.
3. The daemon can replace itself with a newly installed binary while sessions keep running: same child processes, same PTYs, output keeps flowing.
4. One command, `rex upgrade`, does 3 then 2 for every board, and reports per process.
5. Same-user only; Lua/config reload stay fail-closed (R05/W3).

Non-goals: building or installing binaries (the user runs `make install` / `rex update`); Windows hot restart (cold restart with a warning).

## 3. Daemon hot restart: architecture

### Options

| | (a) re-exec in place | (b) new process + SCM_RIGHTS | (c) separate PTY holder |
|---|---|---|---|
| Child stays our child | **yes**, same PID; `wait4` keeps working, exit codes kept | no: re-parented to launchd/init when the old daemon exits; no `waitpid`, exit codes lost (needs polling, `PR_SET_CHILD_SUBREAPER` is Linux-only) | yes, child of the holder |
| Rollback if the new binary is bad | up to `execve`: the old image keeps running when exec fails or the preflight refuses | full: old keeps running until the new one confirms | n/a |
| Socket gap | none: the listening fd is inherited, queued connections wait in the backlog | small: two listeners or a rename dance | none |
| State lock | the flock fd is inherited (flock belongs to the open file description) | must hand over, or race | — |
| Size | small: one package + supervisor adopt path | medium: fd passing protocol, two processes alive at once | large: new process type, its own protocol, and the holder itself cannot be upgraded without the same problem |
| macOS + Linux | both (spiked on darwin: same PID, PTY flows, `Wait` reaps) | both, but loses `wait` | both |

**Decision D1: (a) re-exec in place.** It is the only option where the agents stay children of the daemon (exit codes, zombie reaping and `KillTree` keep working unchanged), it needs no second process, no socket gap and no lock transfer. Its one weakness, no rollback after `execve`, is covered by a preflight: before exec, the old daemon runs the new binary as `rex-daemon -handoff-check <file>`, which must parse the exact handoff file it will receive and exit 0. A binary that is missing, not executable, from an older build (unknown flag), or that cannot read the format, is refused and the old daemon keeps running untouched.

### Handoff sequence (old image)

Triggered by the `Restart` intent (`mode: "hot"`), never by a signal.

1. Refuse when: Windows; another handoff is running; the executable cannot be resolved. Reply with the reason; nothing changes.
2. **Quiesce sessions** (each supervisor's `Freeze`, bounded by 3 s total):
   - the input forwarder stops taking client input; the PTY writer finishes the unit it is writing (a paste with its Enter) and its queue, then parks;
   - the reader parks at its next read: the reader waits for readability with a 50 ms `select`/`poll` before each `read`, so it never sits inside a blocking `read` and every byte it took from the kernel was already written to the transcript, fanned out and applied to the screen. Bytes the agent writes after the park stay in the kernel PTY buffer for the new image;
   - the session's screen is captured as `ModePreamble + Repaint` (the attach repaint, exact cell state) plus cols/rows.
   A session that does not park in time aborts the whole handoff: every session is thawed and the reply says which one (rollback).
3. Persist every session's `meta.json` (existing `state.WriteMeta`).
4. Write the handoff file `<state-dir>/handoff-<pid>.json` (0600, `O_EXCL`), format `rex-handoff/1`: daemon pid, uid, socket path, listener fd, lock fd, and per session: id, child pid, PTY fd, cols, rows, tool id, hook file, screen bytes (base64), argv0.
5. Preflight: `newbin -handoff-check <file>` with a 10 s timeout; must print `ok rex-handoff/1` and exit 0.
6. Clear `FD_CLOEXEC` on exactly the listener, lock and PTY master fds; `execve(newbin, argv + -handoff <file>, env)`.
7. If `execve` returns: restore `FD_CLOEXEC`, delete the file, thaw every session, reply with the error. The old daemon serves on.

Existing client connections are closed right before exec (the kernel would close them anyway); clients reconnect (the board already does, `reconnect.go`; attach popups re-attach, §4). Connections that arrive during the exec wait in the listen backlog.

### Restore (new image)

`rex-daemon -handoff <file>`:

1. Read the file; refuse unless: owner uid == our uid, mode has no group/other bits, not a symlink, `pid == os.Getpid()` (only the image we exec'd into can take it: a file planted by another process names another pid), format known. Delete it after reading.
2. Adopt the lock fd (no second `flock`) and the listener fd (`net.FileListener`), and check the listener's path is our `-socket`.
3. Load sessions from disk as usual, but the handed-off ids are not remapped to `crashed`.
4. For each session: build the supervisor in **adopt** mode: `os.NewFile(fd)` as the PTY, `os.FindProcess(pid)` as the child (our own child, so `Wait` works), a fresh vt10x screen of cols×rows fed with the captured screen bytes, the tool's adapter rebuilt from the registry; the Claude hook adapter re-reads its hook file from offset 0 (the file is the source of truth; subagent/wait timestamps restart at "now"). Input, resize, attach, mode preamble and writer registrations are the same as for a spawn. A session whose fd or pid is not valid (it exited during the exec) is reaped and finished through the normal exit path.
5. Serve on the inherited listener.

If the new image dies after exec, the PTY masters close and the agents get SIGHUP: there is no rollback past exec. The preflight makes that unlikely; the CLI says so before a hot restart.

### Version skew

- The handoff file carries `format` and the writer's build id. A reader accepts only formats it knows (`rex-handoff/1` now); `-handoff-check` is how the old image asks.
- An old daemon that predates `Restart` answers `bad_intent`: the CLI reports "this daemon predates hot restart; the first upgrade needs `rex daemon restart -cold`".
- `Snapshot` gains `daemon_pid`, `daemon_build` (first 12 hex of the executable's SHA-256) and `daemon_started_at` (exec time of the current image), so clients can tell a new image from the old one at the same pid.

### Security

- The trigger is the socket, which already checks peer credentials (`verifyPeer`, same uid) and is 0600.
- The daemon only ever execs its own `os.Executable()` path; a client cannot name a binary.
- The handoff file is created `O_EXCL` 0600 in the 0700 state dir, and the reader checks uid, mode, no symlink and pid (above). No fd is received from another process; fds only cross our own `execve`.

### `rex daemon restart`

- Default **hot** where supported. With live sessions and hot not possible (Windows, old daemon): asks `N sessions will be killed. Continue? [y/N]` at a terminal; without a terminal it needs `-yes` (exit 6), the same rule as `rex update`.
- `-cold` keeps today's stop + start, with the same confirmation when sessions are live.
- Output: `✓ rex-daemon hot-restarted (pid 123, 4 sessions kept, build abc → def)`.

## 4. Board hot swap

- `:swap` (and `rex upgrade`) re-execs the board in place: the board saves a swap state file (`tui-swap-<pid>.json`, 0600, removed on read, pid-checked) with selection, filter, search, hidden sections, scroll offset, focus/open page (board, help, settings, usage, stats, overview, loops) and the attach target, quits Bubble Tea cleanly, then `execve(os.Executable(), args + --quiet-boot)` with `REX_TUI_SWAP=<file>`.
- The new board boots quiet (no boot sounds, D-item reuse of `QuietBoot`), restores the view, and re-opens the attach popup on the same session when that session is still live. Unsafe states are not carried: an open wizard, confirm dialog, rename or inline editor blocks an automatic swap.
- **Detection (D3):** the board stats its executable every 10 s. When size or mtime changes it shows `new rex build installed · :swap to load it` on the status line, and swaps on its own once it is idle: board focus, no overlay or editor open, no key for 60 s. Setting `auto_swap` (Features, default **on**); off = status line only.
- Attach popups survive a daemon hot restart: a popup whose connection closed while its session is still live re-attaches (backoff 250 ms → 2 s, 10 tries) instead of showing "session ended".
- `execve` failing leaves an error on the status line; the board keeps running (rollback).

## 5. Reload that is actually usable

One reload covers: `tools.yaml`, `config.yaml`, `config.lua` + `lua/`, workspace `.rex/config.lua` (part of the settings layer), themes (board), the model cache (already re-detected on a manual reload), and **`init.lua`** (new: D4 — the daemon's Lua event runtime is rebuilt; a file that fails to load keeps the previous runtime and reports the error with `file:line`).

- `Reload` replies with a report: `changed` (setting ids whose value changed), `restart` (changed ids whose Apply class is `daemon_restart`), `errors` (`file:line: message`). `rex reload` prints it: `✓ reloaded · changed: theme, usage · needs restart: summary_model` and each error on its own line; `agent`/`json` carry the same fields; exit 5 when any layer failed. An old daemon's plain ack still prints `reloaded`.
- `:reload` and the Settings page's reload key show the same report on the status line.
- `SIGHUP` logs the report.

### Apply-class audit

Every setting's declared class was checked against the code (TUI and daemon). Settings whose class was wrong, and the fix, are listed in the plan's Task 9 and in the final report; audio (`sound_enabled`, `soundset`, `master_volume`, `boot_voice`, `notify_on_state_change`) is audited but fixed on `fix/audio-settings-live`, not here.

## 6. `rex upgrade`

`rex upgrade [-yes] [-cold] [-o FORMAT]`: after the user installed a new build:

1. Hot-restart the daemon (as `rex daemon restart`), wait until it answers with the new build.
2. Send `SwapBoards`: the daemon sends a `swap` event to every board connection. Boards on an older build swap (now, not when idle); boards already on the new build do nothing.
3. Wait up to 10 s for the boards to reconnect, listing board connections (`Boards` intent: pid, build) and report per process: `daemon pid 123: hot-restarted (build abc → def, 4 sessions kept)`, `board pid 456: swapped`, `board pid 789: still old build`.

## 7. Platforms

- macOS, Linux: everything.
- Windows: no hot restart (`Restart` hot answers `unsupported`), cold restart with a confirmation; board swap falls back to a status-line message (Windows has no `execve`).

## Decisions

| # | Question | Choice (recommended) |
|---|---|---|
| D1 | Daemon handoff mechanism | (a) re-exec in place + preflight `-handoff-check` |
| D2 | Reader quiesce | 50 ms readability wait before each PTY read, park on freeze; no blocking read with bytes in flight |
| D3 | Board on a new binary | status line + auto swap when idle 60 s (`auto_swap`, default on) |
| D4 | `init.lua` on reload | rebuild the runtime; failure keeps the old one |
| D5 | vt emulator state across the handoff | carry `ModePreamble + Repaint` bytes and replay into a fresh vt10x (exact cells, no transcript re-parse) |
| D6 | Hook adapter state | re-read the hook file from offset 0 in the new image |
| D7 | Restart default | hot; `-cold` keeps the old path; confirmation when sessions would die |
| D8 | Who may trigger | socket only (same uid); daemon execs only its own executable |
| D9 | Feature goroutines (usage ledger, audit, snapshots) at exec | not drained; their files are written atomically or append-only and tolerate a cut line. Residual risk recorded |
