# server

**Path:** `internal/daemon/server`
**Depends on:** `internal/wire/protocol`, `internal/catalog/registry`, `internal/daemon/state`, `internal/daemon/adapter`, `internal/runtime/ids`, `internal/daemon/pty` | external: standard library
**Depended on by:** `cmd/rex-daemon`, tests in `internal/wire/client`
**Entry points:** `New`, `(*Server).Serve`, `Config`, session gate and channel registration APIs

## Purpose

Unix domain socket listener: one goroutine per client, intent dispatch, PTY session lifecycle, event fan-out to subscribers.

## Public surface

### Types

- `Config` — `Socket`, `StateDir`, `Registry`, `Store`, `MaxConcurrentSessions`, `SummaryRequest chan<- string`.
- `Server` — `New(cfg Config) (*Server, error)`, `Serve(ctx context.Context) error`, `SetRegistry`, `Registry`.

Session lifecycle / IO registration (used by `client.go` handlers and supervisor):

- `TryAcquireSession`, `ReleaseSession`, `SetMaxConcurrentSessions`, `MaxConcurrentSessions`
- `RegisterInputChannel`, `UnregisterInputChannel`, `InputChannel`, `SendInput` (in-process input through the `Reply` gate; Lua `rex.send`)
- `SubscribeSessionOutput`, `SubscribeSummarizerHealth`, `BroadcastSummarizerHealth`
- `RegisterResize`, `UnregisterResize`, `Resize`
- `RegisterStop`, `UnregisterStop`, `StopSession`
- `RegisterComplete`, `UnregisterComplete`, `CompleteSession`
- `TranscriptDir`

Per-connection handling lives in unexported `client.go` (`handleClient`, intent switch).

## Internal structure

- `server.go` — listener, accept loop, connection tracking, shutdown, registry swap, concurrency gate.
- `client.go` — per-connection intent switch.
- `conn.go` — per-client outbound queue, writer goroutine, `Hello` gating.
- `spawn.go` — `handleNewSession`; `spawn_prompt.go` — initial prompt on argv vs PTY paste.
- `input.go` — input gate, `Reply` sanitizing and delayed Enter.
- `output.go`, `wire.go` — PTY output streams, replay, store-event encoding.
- `validate.go` — field checks and normalization.
- `daemonlock*.go` — single-instance lock.
- `retire.go` — retire deadlines and reaper.
- `*_test.go` — unit, streaming, concurrency, e2e, gate tests.

## Invariants

`New` refuses with `ErrAlreadyRunning` when a daemon answers on the socket or holds `<state-dir>/daemon.lock`; otherwise it takes that lock and unlinks a stale socket file. `Serve` chmods the socket `0600`, then calls `Config.OnListening` (where the daemon prints its banner; never called when the bind fails), and on return unlinks it only if the path is still the one it bound. After `Hello`, client receives `EventSnapshot`, then the board events held while the snapshot was taken, then live ones (`conn.go` `hello`). Every event to a client goes through its bounded queue (`clientQueueLen` 2048) and writer goroutine (`clientWriteTimeout` 10 s); overflow or a stalled write disconnects that client. On shutdown `Serve` closes every client connection and waits up to 5 s (`supervisorStopTimeout`) for supervisors to stop.

Concurrent session cap enforced at spawn via `TryAcquireSession`.

## Side effects

Unix socket listen. Spawns supervisor goroutines per session. Unlinks socket path on shutdown.

## Error handling

Intent errors → `EventError` with `protocol.ErrCode*`. Spawn failures do not consume slot if acquire rolled back (see handler code).

## Tests

Broad coverage: `server_test.go`, `client_test.go`, `streaming_test.go`, `concurrency_test.go`, `e2e_test.go`, `gate_test.go`.

## Gotchas

Unknown intents (e.g. the removed `OpenSession`) fall to the `default` case: `bad_intent` "intent not implemented". `IntentShutdown` / `IntentReload` call `Config.OnShutdown` / `OnReload` (nil = `bad_intent`; a reload error is `registry_invalid`); the daemon wires them so Windows can stop and reload without signals, and `daemonctl` uses them first on every OS. `FocusFilter` is accepted and ignored. Client text is sanitized in `validate.go` (`normalizeTitle`, `normalizeFleet`, `validateCWD`) and `input.go` (`sanitizeReplyText`); `input.go` also orders a `Reply`'s delayed Enter against other input. `StopSession` blocks until supervisor exits; `CompleteSession` non-blocking signal.

## See also

- `modules/client.md`
- `modules/pty.md`
- `modules/protocol.md`
- `modules/cmd.md` — daemon wires `Config`.
