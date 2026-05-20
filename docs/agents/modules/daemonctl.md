# daemonctl

**Path:** `internal/runtime/daemonctl`
**Depends on:** (none) | external: standard library (`net`, `os`, `os/exec`, `path/filepath`, `time`, `log/slog`)
**Depended on by:** `internal/surface/cli`, `internal/surface/tui`
**Entry points:** `DefaultSocket`, `FindBinary`, `Reachable`, `Start`

## Purpose

Spawns `rex-daemon`, resolves its binary path, and checks Unix socket reachability. Shared by the TUI boot splash and `rex daemon start`.

## Public surface

### Types

- `StartResult` — `PID int`, `Elapsed time.Duration`.
- `Start(socket string, stderrLog *os.File) (*StartResult, error)` — exec `rex-daemon`, poll up to ~2s for socket.

### Functions

- `DefaultSocket() string` — `$XDG_RUNTIME_DIR/rex.sock` if `XDG_RUNTIME_DIR` is set; else `~/.cache/rex/rex.sock`.
- `FindBinary() string` — sibling of current executable, then `PATH`, then literal `rex-daemon`.
- `Reachable(socket string) bool` — `DialTimeout` 200ms on Unix socket.

## Internal structure

- `daemon.go` — socket default, spawn, reachability.
- `daemon_test.go` — `findBinaryIn` and reachability with temp sockets.

## Invariants

`DefaultSocket` must match `rex-daemon` default socket logic in `cmd/rex-daemon/main.go`.

## Side effects

`Start` spawns a child process (`rex-daemon`). Writes to optional `stderrLog`.

## Error handling

`Start` returns error if exec fails or socket never becomes reachable within ~2s.

## Tests

`daemon_test.go` — binary discovery in directory, socket dial.

## Gotchas

`Start` does not pass daemon flags; the child uses `rex-daemon` defaults. TUI/CLI rely on matching default paths for socket and state.

## See also

- `modules/cmd.md` — `rex-daemon` flags.
- `interfaces/config.md` — `XDG_RUNTIME_DIR`.
