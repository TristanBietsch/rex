# state

**Path:** `internal/daemon/state`
**Depends on:** `internal/wire/protocol` | external: standard library
**Depended on by:** `internal/daemon/server`, `internal/daemon/pty`, `internal/features/summarizer`, `cmd/rex-daemon`, `internal/surface/cli`
**Entry points:** `Store`, `Session`, `LoadAll`, `WriteMeta`, `OpenTranscript`

## Purpose

In-memory session table with pub/sub events and persistence under `<state-dir>/sessions/<id>/`.

## Public surface

### Types

- `EventKind` — `EventAdded`, `EventUpdated`, `EventRemoved`.
- `Event` — `Kind`, `SessionID`, `NewState`, `Patch`, `Summary`.
- `Session` — fields mirror `protocol.SessionSummary` plus internal mutex.
- `Store` — `NewStore`, `Add`, `AddUnique`, `Get`, `Remove`, `All`, `Snapshot`, `CurrentState`, `SetFleet`, `BroadcastTokenPatch`, `Subscribe` / unsubscribe pattern (see `store.go`).
- `(*Session) Summary() protocol.SessionSummary`

### Functions

- `LoadAll(root string) ([]*Session, error)` — reloads `meta.json` per session; live states `queued`/`working`/`needs_input` become `crashed`.
- `LoadMeta(root, id string) (*Session, error)`
- `WriteMeta(root string, s *Session) error` — atomic write via temp file + rename.
- `OpenTranscript(root, id string) (*TranscriptFile, error)` — kept-open `transcript.log` handle (`0600`) the PTY supervisor writes through; `Write` renames the file to `transcript.log.1` (replacing it) before it would cross 64 MiB (`transcriptRotateBytes`). `AppendTranscript` is the open-write-close convenience.
- `TranscriptTail(root, id string, max int) ([]byte, error)` — last `max` bytes, continuing into `transcript.log.1` after a recent rotation; missing file → `(nil, nil)`.
- `SecureSessionsDir(root string) error` — creates and tightens `<root>/sessions` to `0700`.
- Session ids that would escape `<root>/sessions` (empty, `..`, separators) are refused with `ErrUnsafeSessionID`.
- `HookFile(root, id string) string` — path of the session's `hooks.log`.
- `RemoveSessionDir(root, id string) error`

## Internal structure

- `session.go` — `Session` struct and `Summary`.
- `store.go` — `Store` CRUD and broadcast.
- `persist.go` — disk I/O.
- `store_test.go`, `persist_test.go`, `fields_test.go`

## Invariants

`short_id` map in `Store` must stay consistent with `Session.ShortID`. Meta writes are atomic per session.

## Side effects

Filesystem: `meta.json`, `transcript.log` (+ `transcript.log.1`) under `<stateDir>/sessions/<id>/`; dirs `0700`, files `0600`.

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
