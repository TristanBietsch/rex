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
- `Rename(sessionID, slug, title string) error`
- `Delete(sessionID string) error`
- `Complete(sessionID string) error`
- `FocusFilter(toolID string) error`
- `SetMaxConcurrent(n int) error` — `n <= 0` means uncapped.
- `SetSessionFleet(sessionID, fleet string) error`
- `NextEvent() (protocol.Envelope, error)`
- `Drain(handler func(protocol.Envelope) bool) error`

## Internal structure

- `client.go` — implementation.
- `client_test.go`, `client_internal_test.go` — against in-process `server` in tests.

## Invariants

Caller must send `Hello` before other intents (server enforces). One goroutine should call `NextEvent` unless externally synchronized.

## Side effects

Network: Unix socket I/O.

## Error handling

Dial and I/O errors propagate. `Hello` returns error if response is not `EventSnapshot`.

## Tests

Integration-style tests with `internal/daemon/server` test harness.

## Gotchas

`FocusFilter` does not change daemon-global filter for other clients. `Drain` stops when handler returns false or EOF.

## See also

- `modules/protocol.md`
- `modules/server.md`
- `interfaces/cli.md` — commands that use this client.
