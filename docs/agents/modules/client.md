# client

**Path:** `internal/wire/client`
**Depends on:** `internal/wire/protocol` | external: standard library (`net`, `time`)
**Depended on by:** `internal/surface/cli`, `internal/surface/tui`
**Entry points:** `Dial`, `Client` methods

## Purpose

Go SDK for `rex-daemon` Unix-domain-socket protocol: dial, send intents, read events.

## Public surface

### Types

- `Client` — wraps UDS connection with `protocol.Reader` / `Writer`.

### Functions

- `Dial(socket string) (*Client, error)` — `net.Dial("unix", socket)`.

### Methods

- `Close() error`
- `SetReadDeadline(t time.Time) error`
- `Hello(clientVersion string) (*protocol.Snapshot, error)` — first message after connect; blocks for `EventSnapshot`.
- `Subscribe(sessionID string) error` — `sessionID==""` for board-wide only.
- `SubscribeReplay(sessionID string) error` — subscribe plus transcript tail as `SessionOutput`.
- `NewSession(req protocol.NewSession) error`
- `SendInput(sessionID string, b []byte) error`
- `Reply(sessionID, text string) error`
- `Resize(sessionID string, cols, rows uint16) error`
- `Rename(sessionID, slug, title, reqID string) error` (reqID echoed on Error, e.g. slug in use)
- `Delete(sessionID string) error`
- `Complete(sessionID string) error`
- `SetSessionName(sessionID, name, reqID string) error`, `Retire(sessionID string, ttl time.Duration, reqID string) error`, `Unretire(sessionID, reqID string) error`
- `Shutdown() error`, `Reload() error`
- `SetMaxConcurrent(n int) error` — the daemon clamps `n` to 1–64.
- `SetSessionFleet(sessionID, fleet string) error`
- `NextEvent() (protocol.Envelope, error)`
- `Drain(handler func(protocol.Envelope) bool) error`

## Internal structure

- `client.go` — implementation.
- `client_test.go`, `client_internal_test.go` — against in-process `server` in tests.

## Invariants

Send `Hello` first: the daemon sends no board events before it (intents sent earlier are still handled), and none are lost between the `Snapshot` and the live stream. One goroutine should call `NextEvent` unless externally synchronized.

## Side effects

Network: Unix socket I/O.

## Error handling

Dial and I/O errors propagate. `Hello` returns error if response is not `EventSnapshot`.

## Tests

Integration-style tests with `internal/daemon/server` test harness.

## Gotchas

There is no `FocusFilter` helper: the intent is obsolete and filtering is client-side. The client reads with the unlimited `protocol.NewReader`; the daemon caps lines it reads at 1 MiB. A client that stops reading is disconnected by the daemon once 2048 events queue up or a write stalls 10 s. `Drain` stops when handler returns false or EOF.

## See also

- `modules/protocol.md`
- `modules/server.md`
- `interfaces/cli.md` — commands that use this client.
