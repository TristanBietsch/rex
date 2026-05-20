# protocol

**Path:** `internal/wire/protocol`
**Depends on:** (none) | external: standard library (`encoding/json`, `io`, `time`)
**Depended on by:** `internal/daemon/server`, `internal/wire/client`, `internal/daemon/state`, `internal/surface/cli`, `internal/surface/tui`, `internal/daemon/adapter`, `internal/features/lua`, `internal/features/summarizer`, `internal/daemon/pty`
**Entry points:** `ProtocolVersion`, `Envelope`, `Reader`, `Writer`, intent/event constants and payload structs

## Purpose

Defines the newline-delimited JSON wire format between `rex` clients and `rex-daemon`.

## Public surface

### Constants

- `ProtocolVersion = 1`
- `KindIntent`, `KindEvent` (`Kind` type)
- Intent types: `IntentHello`, `IntentSubscribe`, `IntentNewSession`, `IntentOpenSession`, `IntentSendInput`, `IntentReply`, `IntentRename`, `IntentDelete`, `IntentResize`, `IntentFocusFilter`, `IntentShutdown`, `IntentSetMaxConcurrent`, `IntentSetSessionFleet`, `IntentComplete`
- Event types: `EventSnapshot`, `EventSessionAdded`, `EventSessionUpdated`, `EventSessionRemoved`, `EventSessionOutput`, `EventSummarizerHealth`, `EventError`
- States: `StateQueued`, `StateWorking`, `StateNeedsInput`, `StateDone`, `StateFailed`, `StateCrashed`
- Error codes: `ErrCodeBadIntent`, `ErrCodeUnknownSession`, `ErrCodeAmbiguousID`, `ErrCodeRegistry`, `ErrCodeSpawn`, `ErrCodeTooManySessions`, `ErrCodeBadState`

### Types

- `Envelope` — `V`, `Kind`, `Type`, `ID`, `Data json.RawMessage`
- `Hello`, `Subscribe`, `NewSession`, `OpenSession`, `SendInput`, `Reply`, `Rename`, `Delete`, `Resize`, `FocusFilter`, `SetSessionFleet`, `Complete`, `SessionSummary`, `Snapshot`, `SessionUpdated`, `SessionRemoved`, `SessionOutput`, `SummarizerHealth`, `ErrorEvent`
- `Reader` / `NewReader` / `Read` — newline-delimited decode
- `Writer` / `NewWriter` / `WriteIntent`, `WriteEvent`

## Internal structure

- `envelope.go` — `Envelope`, `Kind`, version.
- `intents.go` — client→daemon payloads.
- `events.go` — daemon→client payloads and `SessionSummary`.
- `codec.go` — read/write helpers.
- `codec_test.go`, `envelope_test.go`

## Invariants

Every message is one JSON object per line. `V` must equal `ProtocolVersion` on read (server rejects mismatch).

`SendInput.Bytes` JSON-encodes as base64 on the wire (stdlib `encoding/json` behavior for `[]byte`).

## Side effects

I/O only through passed `io.Reader` / `io.Writer`.

## Error handling

Codec returns decode errors; server maps handler failures to `EventError` with stable `ErrCode*` strings.

## Tests

`codec_test.go`, `envelope_test.go` — round-trip and version checks.

## Gotchas

`IntentOpenSession` and `IntentShutdown` exist in `intents.go` but `internal/daemon/server/client.go` does not handle them (would return not-implemented style error if sent). `IntentFocusFilter` is cosmetic per-client state.

## See also

- `data/schemas.md` — field-level wire shapes.
- `modules/client.md` — typed intent helpers.
- `modules/server.md` — handler switch.
- Human reference: [../../protocol.md](../../protocol.md).
