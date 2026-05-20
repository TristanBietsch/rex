# state

**Path:** `internal/daemon/state`
**Depends on:** `internal/wire/protocol` | external: standard library
**Depended on by:** `internal/daemon/server`, `internal/daemon/pty`, `internal/features/summarizer`, `cmd/rex-daemon`, `internal/surface/cli`
**Entry points:** `Store`, `Session`, `LoadAll`, `WriteMeta`, `AppendTranscript`

## Purpose

In-memory session table with pub/sub events and persistence under `<state-dir>/sessions/<id>/`.

## Public surface

### Types

- `EventKind` — `EventAdded`, `EventUpdated`, `EventRemoved`.
- `Event` — `Kind`, `SessionID`, `NewState`, `Patch`, `Summary`.
- `Session` — fields mirror `protocol.SessionSummary` plus internal mutex.
- `Store` — `NewStore`, `Add`, `Get`, `GetByShortID`, `Remove`, `All`, `Snapshot`, `CurrentState`, `SetFleet`, `BroadcastTokenPatch`, `Subscribe` / unsubscribe pattern (see `store.go`).
- `(*Session) Summary() protocol.SessionSummary`

### Functions

- `LoadAll(root string) ([]*Session, error)` — reloads `meta.json` per session; live states `queued`/`working`/`needs_input` become `crashed`.
- `LoadMeta(root, id string) (*Session, error)`
- `WriteMeta(root string, s *Session) error` — atomic write via temp file + rename.
- `AppendTranscript(root, id string, b []byte) error` — append `transcript.log`.
- `TranscriptTail(root, id string, max int) ([]byte, error)` — last `max` bytes; missing file → `(nil, nil)`.
- `RemoveSessionDir(root, id string) error`

## Internal structure

- `session.go` — `Session` struct and `Summary`.
- `store.go` — `Store` CRUD and broadcast.
- `persist.go` — disk I/O.
- `store_test.go`, `persist_test.go`, `fields_test.go`

## Invariants

`short_id` map in `Store` must stay consistent with `Session.ShortID`. Meta writes are atomic per session.

## Side effects

Filesystem: `meta.json`, `transcript.log` under `<stateDir>/sessions/<id>/`.

## Error handling

`Add` errors on duplicate ID. `Remove` errors if missing. Persist functions return I/O errors.

## Tests

Store subscription, persist round-trip, field patches, crash recovery classification.

## Gotchas

`rex log` reads transcript files directly from disk (CLI), not only via daemon. Token counts are heuristic (`OutputBytes/4`). Summarizer reads sanitized tail via injected `TranscriptReader`.

## See also

- `modules/server.md` — owns `Store` instance.
- `data/schemas.md` — on-disk layout.
- `modules/summarizer.md`
