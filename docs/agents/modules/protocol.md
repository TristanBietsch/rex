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
- Intent types: `IntentHello`, `IntentSubscribe`, `IntentNewSession`, `IntentSendInput`, `IntentReply`, `IntentRename`, `IntentDelete`, `IntentResize`, `IntentShutdown`, `IntentSetMaxConcurrent`, `IntentSetSessionFleet`, `IntentComplete`, `IntentSetSessionName`, `IntentRetire`, `IntentUnretire`, `IntentReload`, `IntentFocusFilter` (obsolete; accepted and ignored)
- `MaxIntentLineBytes` (1 MiB) — daemon-side line cap; `MaxSlugRunes` (64)
- Event types: `EventSnapshot`, `EventSessionAdded`, `EventSessionUpdated`, `EventSessionRemoved`, `EventSessionOutput`, `EventSummarizerHealth`, `EventError`
- States: `StateQueued`, `StateWorking`, `StateNeedsInput`, `StateDone`, `StateFailed`, `StateCrashed`
- Error codes: `ErrCodeBadIntent`, `ErrCodeUnknownSession`, `ErrCodeRegistry`, `ErrCodeSpawn`, `ErrCodeTooManySessions`, `ErrCodeBadState`, `ErrCodeBusy`

### Types

- `Envelope` — `V`, `Kind`, `Type`, `ID`, `Data json.RawMessage`
- `Hello`, `Subscribe`, `NewSession`, `SendInput`, `Reply`, `Rename`, `Delete`, `Resize`, `SetSessionFleet`, `SetMaxConcurrent`, `Complete`, `SetSessionName`, `Retire`, `Unretire`, `SessionSummary`, `Snapshot`, `SessionUpdated`, `SessionRemoved`, `SessionOutput`, `SummarizerHealth`, `ErrorEvent`
- `Reader` / `NewReader` / `NewLimitedReader` / `Read` — newline-delimited decode; typed errors `ErrBadEnvelope`, `ErrUnsupportedVersion`, `ErrLineTooLong`
- `ValidateSlug`, `SingleLine`, `NormalizeSessionName` — shared input rules
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

`IntentShutdown` and `IntentReload` are handled (socket forms of SIGTERM / SIGHUP); a failed reload is `ErrCodeRegistry`. `ErrCodeBadState` means the session exists but can't take the intent (`SendInput` / `Reply` / `Complete` after exit, `Unretire` once the reaper claimed it); `ErrCodeBusy` is a full input buffer. `IntentFocusFilter` is obsolete: the daemon accepts and ignores it. The daemon reads with `NewLimitedReader(conn, MaxIntentLineBytes)`; clients use the unlimited `NewReader` (a `Snapshot` grows with the board). Unknown intent types, including the removed `OpenSession`, get `bad_intent` "intent not implemented".

## See also

- `data/schemas.md` — field-level wire shapes.
- `modules/client.md` — typed intent helpers.
- `modules/server.md` — handler switch.
- Human reference: [../../protocol.md](../../protocol.md).
