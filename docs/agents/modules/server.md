# server

**Path:** `internal/daemon/server`
**Depends on:** `internal/wire/protocol`, `internal/catalog/registry`, `internal/daemon/state`, `internal/daemon/adapter`, `internal/runtime/ids`, `internal/daemon/pty` | external: standard library
**Depended on by:** `cmd/rex-daemon`, tests in `internal/wire/client`
**Entry points:** `New`, `(*Server).Serve`, `Config`, session gate and channel registration APIs

## Purpose

Unix domain socket listener: one goroutine per client, intent dispatch, PTY session lifecycle, event fan-out to subscribers.

## Public surface

### Types

- `Config` — `Socket`, `StateDir`, `Registry`, `Store`, `MaxConcurrentSessions`, `SummaryRequest chan<- string`, `SkillsDir` (skill library; `NewSession.Skills` names resolve under it, staged by `internal/features/skills`), `SkillsEnabled func() bool` (the `spawn_skills` flag; off: skills refused as bad_intent, no `--plugin-dir`).
- `Server` — `New(cfg Config) (*Server, error)`, `Serve(ctx context.Context) error`, `SetRegistry`, `Registry`.

Session lifecycle / IO registration (used by `client.go` handlers and supervisor):

- `TryAcquireSession`, `ReleaseSession`, `SetMaxConcurrentSessions`, `MaxConcurrentSessions`
- `RegisterInputChannel`, `UnregisterInputChannel`, `InputChannel`
- `SubscribeSessionOutput`, `SubscribeSummarizerHealth`, `BroadcastSummarizerHealth`
- `SetOverview` (sanitizes the headline again, stores it in memory, fans it out as an `Overview` event), `Overview`, `SubscribeOverview`, `BoardClients` (open connections that said Hello as `protocol.ClientTUI`), `Config.OnBoardClient`
- `RegisterResize`, `UnregisterResize`, `Resize`
- `RegisterModePreamble`, `UnregisterModePreamble`, `ModePreamble`
- `RegisterAttach`, `UnregisterAttach` — the supervisor's output-hold hook; `handleSubscribe` sends replay/repaint and registers its sink inside it (`withOutputHeld`)
- `RegisterStop`, `UnregisterStop`, `StopSession`
- `RegisterComplete`, `UnregisterComplete`, `CompleteSession`
- `TranscriptDir`

Per-connection handling lives in unexported `client.go` (`handleClient`, intent switch).

## Internal structure

- `server.go` — listener, accept loop, registry swap, concurrency gate.
- `client.go` — per-connection protocol handler and `handleNewSession`.
- `*_test.go` — unit, streaming, concurrency, e2e, gate tests.

## Invariants

`New` unlinks stale socket file. `Serve` unlinks socket on return. After `Hello`, client receives `EventSnapshot` then live store events.

Concurrent session cap enforced at spawn via `TryAcquireSession`.

## Side effects

Unix socket listen. Spawns supervisor goroutines per session. Unlinks socket path on shutdown.

## Error handling

Intent errors → `EventError` with `protocol.ErrCode*`. Spawn failures do not consume slot if acquire rolled back (see handler code).

## Tests

Broad coverage: `server_test.go`, `client_test.go`, `streaming_test.go`, `concurrency_test.go`, `e2e_test.go`, `gate_test.go`.

## Gotchas

`IntentOpenSession` has no handler in `internal/daemon/server/handlers.go` (`bad_intent`). `IntentShutdown` is handled (`handleShutdown`, needs `Config.OnShutdown`). `FocusFilter` stored per connection only. `StopSession` blocks until supervisor exits; `CompleteSession` non-blocking signal.

## See also

- `modules/client.md`
- `modules/pty.md`
- `modules/protocol.md`
- `modules/cmd.md` — daemon wires `Config`.
