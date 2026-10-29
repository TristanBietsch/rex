# daemonctl

**Path:** `internal/runtime/daemonctl`
**Depends on:** `internal/wire/client`, `internal/wire/protocol` | external: standard library (`net`, `os`, `os/exec`, `path/filepath`, `time`, `log/slog`)
**Depended on by:** `internal/surface/cli`, `internal/surface/tui`
**Entry points:** `DefaultSocket`, `FindBinary`, `Reachable`, `Start`, `Stop`, `Reload`, `CheckSocketPath`

## Purpose

Spawns, stops, and reloads `rex-daemon`, resolves its binary path, and checks Unix socket reachability. Shared by the TUI boot splash, `:reload`, and `rex daemon` / `rex reload` / `rex update` / `rex uninstall`.

## Public surface

### Types

- `StartResult` — `PID int`, `Elapsed time.Duration`.
- `Start(socket string, stderrLog *os.File) (*StartResult, error)` — checks the socket path, execs `rex-daemon -socket <socket>`, polls up to `StartTimeout` (2 s) for the socket.

### Functions

- `DefaultSocket() string` — `$XDG_RUNTIME_DIR/rex.sock` if `XDG_RUNTIME_DIR` is set; else `~/.cache/rex/rex.sock`.
- `DefaultStateDir()`, `DefaultToolsPath()`, `LogPath()` — default paths (`paths.go`).
- `FindBinary() string` — sibling of current executable, then `PATH`, then literal `rex-daemon`.
- `Reachable(socket string) bool` — `DialTimeout` 200 ms on the socket.
- `CheckSocketPath(path string) error` — rejects a path longer than `sun_path` allows (103 bytes macOS, 107 Linux/Windows), naming the limit.
- `Stop() error` — `Shutdown` intent on the default socket, then waits up to `StartTimeout` for the socket to go away. Unix fallback (no answer, old daemon, or still up): SIGTERM to `FindPID`. `StopQuietly` ignores errors.
- `Reload() error` — `Reload` intent; a daemon rejection (bad `tools.yaml`) is returned. Unix fallback when nothing answers or the daemon predates the intent: SIGHUP to `FindPID`.
- `FindPID() (string, error)` — Unix: `pgrep -u <euid> -x rex-daemon` (this user's processes named exactly `rex-daemon`); Windows: `tasklist`. `ErrNotRunning` when none.

## Internal structure

- `daemon.go` — socket default, binary lookup, spawn, reachability.
- `process.go` — `sendIntent` (dial, send, wait 500 ms for an `Error`), `waitSocketGone`.
- `process_unix.go` / `process_windows.go` — `FindPID`, `Stop`, `Reload`.
- `socketpath*.go` — `CheckSocketPath` and the per-OS `sun_path` size.
- `spawn_unix.go` / `spawn_windows.go` — detach attributes for the child.
- `paths.go`, `timing.go`.

## Invariants

`DefaultSocket` must match `rex-daemon` default socket logic in `cmd/rex-daemon/main.go`.

## Side effects

`Start` spawns a child process (`rex-daemon`) and writes its stderr to the optional `stderrLog`. `Stop` / `Reload` connect to the socket and may signal a process.

## Error handling

`Start` returns an error if the socket path is too long, exec fails, the daemon exits before its socket comes up (the error carries the last line it wrote to `stderrLog`, its fatal error, or names the log), or the socket never answers within 2 s.

## Tests

`daemon_test.go` — binary discovery, socket dial. `start_unix_test.go` — `-socket` passed, early-exit cause. `process_test.go` — intent-first stop/reload and signal fallback. `socketpath_test.go`.

## Gotchas

`Start` passes only `-socket` (so a client run with `-socket` gets a daemon on that path); every other flag takes the `rex-daemon` default. `Stop` and `Reload` always use `DefaultSocket()`, so for a daemon on another socket only the Unix signal fallback reaches it.

## See also

- `modules/cmd.md` — `rex-daemon` flags.
- `interfaces/config.md` — `XDG_RUNTIME_DIR`.
