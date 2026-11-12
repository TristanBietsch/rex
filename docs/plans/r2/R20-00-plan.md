# R20 hot reload / hot swap / hot restart Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restart or upgrade rex (daemon and boards) without killing or disturbing running agent sessions, and make reload cover every live layer with a clear report.

**Architecture:** The daemon re-execs itself in place (`execve`, same PID) after quiescing every supervisor, handing the PTY master, listener and lock fds across exec with a pid-bound handoff file; the new image adopts the children. Boards re-exec themselves with a swap state file. Reload returns a structured report.

**Tech Stack:** Go 1.26, `golang.org/x/sys/unix`, creack/pty, vt10x (vendored), Bubble Tea.

**Spec:** [R20-hot-reload.md](R20-hot-reload.md)

## Global Constraints

- Handoff format string: `rex-handoff/1`. File: `<state-dir>/handoff-<pid>.json`, 0600, `O_EXCL`.
- Preflight: `rex-daemon -handoff-check <file>` prints `ok rex-handoff/1`, exit 0, timeout 10 s.
- Freeze budget 3 s total; reader readability wait 50 ms.
- Board swap state: `tui-swap-<pid>.json` next to `tui-state.json`, 0600, removed on read; env `REX_TUI_SWAP`.
- Binary watch every 10 s; auto swap after 60 s idle; setting `auto_swap` (Features, bool, default true, ApplyLive).
- Attach re-attach backoff 250 ms doubling to 2 s, 10 tries.
- Intents: `Restart{mode:"hot"|"cold"}`, `SwapBoards`, `Boards`. Event: `swap`. Snapshot fields: `daemon_pid`, `daemon_build`, `daemon_started_at`.
- Tests that spawn a daemon use temp sockets and state dirs (`t.TempDir()`, short socket path under `/tmp`); never the user's.
- Windows: hot restart answers `unsupported`; cold path keeps working.
- `supervisor.go` is shared with `fix/ready-edge-cases`: keep its diff small (adopt branch at console start, reader read through a helper, freeze hook registration).

## Review Focus

1. A byte the agent wrote while the handoff ran must appear exactly once in the new image (transcript, screen, attach). Test: Task 3 freeze test writes during freeze; Task 5 e2e checks marker lines after exec.
2. A child that exits during the exec window must end as done/failed, not hang. Task 4 adopt test with an already-exited pid.
3. Handoff file planted by someone else (wrong pid, wrong mode, symlink) is refused. Task 2 tests.
4. A failed preflight or exec leaves the old daemon fully working (input flows again). Task 5 test with a bad binary path.
5. A board whose daemon hot-restarts keeps its attach popup. Task 7 test.

---

### Task 1: Spec + plan (this commit)

### Task 2: `internal/daemon/handoff` package

**Files:** Create `internal/daemon/handoff/handoff.go`, `fd_unix.go`, `fd_windows.go`, `handoff_test.go`.

**Interfaces (Produces):**
- `const Format = "rex-handoff/1"`
- `type Session struct { ID string; PID int; FD int; Cols, Rows uint16; Screen []byte }`
- `type File struct { Format string; PID int; UID int; Build string; Socket string; ListenerFD int; LockFD int; Sessions []Session }`
- `func Path(stateDir string, pid int) string`
- `func Write(path string, f File) error` (O_EXCL 0600)
- `func Read(path string, wantPID int) (File, error)` (checks Lstat regular, no symlink, uid, mode&077==0, pid, format; removes file)
- `func Check(path string) error` (parse + format only, for `-handoff-check`; does not remove)
- `func Inherit(fds ...int) (restore func(), err error)` (clear FD_CLOEXEC; restore sets it back)
- `func BuildID() string` (sha256 of os.Executable, 12 hex; "" on error)

- [ ] Tests: round trip; wrong pid refused; mode 0644 refused; symlink refused; unknown format refused; Inherit clears and restores (`unix.FcntlInt F_GETFD`).
- [ ] Implement; run `go test ./internal/daemon/handoff`; commit `feat(daemon): handoff file and fd inheritance`.

### Task 3: supervisor freeze + adopt

**Files:** Create `internal/daemon/pty/freeze.go`, `readable_unix.go` (select on darwin, poll on linux), `readable_windows.go`, `adopt_unix.go`, `adopt_windows.go`, `freeze_test.go`. Modify `supervisor.go` (small), `writer.go` (idle/park).

**Interfaces (Produces):**
- `type Frozen struct { FD int; PID int; Cols, Rows uint16; Screen []byte }`
- `SupervisorConfig.RegisterFreeze func(freeze func(ctx context.Context) (Frozen, func(), error))` — the returned func thaws.
- `SupervisorConfig.Adopt *Adopted` with `type Adopted struct { File *os.File; PID int; Screen []byte }`; when set, `Run` skips the spawn.

- [ ] Test `TestFreezeParksReaderAndWriter`: run `sh -c cat` supervisor, freeze, write to child via raw fd, verify nothing reaches OutputSink while frozen; thaw, verify the bytes arrive once.
- [ ] Test `TestAdoptRunsExistingChild`: start `sh -c cat` with creack/pty directly, hand fd+pid to a supervisor with Adopt, send input, see echo in OutputSink, close via CompleteCh, state done.
- [ ] Test `TestAdoptExitedChild`: Adopt a child that already exited → session ends done/failed promptly.
- [ ] Implement; `go test -race ./internal/daemon/pty`; commit.

### Task 4: server restart orchestration

**Files:** Create `internal/daemon/server/hot.go`, `hot_test.go`. Modify `handlers.go`, `spawn.go` (register freeze; adopt start), `protocol/intents.go`, `protocol/events.go`.

**Interfaces:**
- `Config.OnHotRestart func() error`; `func (s *Server) FreezeAll(ctx) ([]handoff.Session, func(), error)`; `func (s *Server) AdoptSession(sess *state.Session, a pty.Adopted, ...) error`; `func (s *Server) ListenerFile() (*os.File, error)`; `Config.Listener net.Listener` (inherited).
- `protocol.IntentRestart`, `protocol.Restart{Mode string}`, `protocol.IntentSwapBoards`, `protocol.IntentBoards`, `protocol.EventSwap`, `protocol.BoardInfo{PID int; Build string}`, Hello gains `PID`, `Build`.
- [ ] Tests: FreezeAll aborts and thaws when one session cannot park; Restart intent unsupported → error code.
- [ ] Commit.

### Task 5: daemon main: `-handoff`, `-handoff-check`, exec

**Files:** Create `cmd/rex-daemon/hot.go`, `cmd/rex-daemon/hot_test.go` (e2e: build daemon to temp, start with temp socket/state, spawn `sh -c cat` session via a test tool overlay, Restart hot, assert same daemon pid, same child pid, output keeps flowing and the transcript has the pre- and post-marker lines exactly once). Bad-binary rollback test via `REX_HANDOFF_EXE` test hook (only honoured when `-test-allow-exe-override` is passed... no: env read only in test builds via a package var set in the test binary? Use a hidden flag `-handoff-exe` that tests pass at daemon start).
- [ ] Commit.

### Task 6: CLI `rex daemon restart` (hot default, `-cold`, confirm) and `rex upgrade`

**Files:** `internal/surface/cli/lifecycle/daemon.go`, new `upgrade.go`, `internal/runtime/daemonctl/hot.go`, `cmd/rex/main.go`, help golden, `docs/cli.md`.
- [ ] Tests with fakes (no real daemon): hot path reports kept sessions; old daemon (`bad_intent`) suggests `-cold`; cold with live sessions without terminal and without `-yes` → exit 6.
- [ ] Commit.

### Task 7: board hot swap

**Files:** `internal/surface/tui/swap.go`, `swap_test.go`, `persist.go`, `tui.go`, `meta/tui.go`, `keys.go` (`:swap`), `attach.go` (re-attach), settings registry (`auto_swap`).
- [ ] Tests: swap state round trip restores selection, filter, focus and attach target; binary change sets the notice; idle + auto_swap triggers swap, wizard open does not; attach closed by conn error schedules re-attach.
- [ ] Commit.

### Task 8: reload report + init.lua

**Files:** `internal/daemon/boot/reload.go`, `settings_hub.go`, `lua.go` (host with Reload), `protocol` (`ReloadReport`), `daemonctl/control.go`, `lifecycle/reload.go`, TUI `command.go`.
- [ ] Tests: hub reload reports changed + restart ids; bad init.lua keeps old runtime and returns `init.lua:N` error; CLI prints changed/needs-restart.
- [ ] Commit.

### Task 9: Apply-class fixes from the audit + docs + CHANGELOG

- [ ] Fix each wrong class (flag change or live applier), test per fix; docs (`daemon.md`, `cli.md`, `settings.md`, `tui.md`), CHANGELOG `[Unreleased]`.

### Task 10: review, merge integration/todo, `make check`
