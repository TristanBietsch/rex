# ids

**Path:** `internal/runtime/ids`
**Depends on:** (none) | external: standard library (`crypto/rand`, `fmt`)
**Depended on by:** `internal/daemon/server`
**Entry points:** `NewSessionID`, `ExtendShortID`

## Purpose

Generates RFC 4122 v4 session UUIDs and derives 4-character `short_id` prefixes for board display and selector resolution.

## Public surface

### Functions

- `NewSessionID() string` — random UUID string.
- `ExtendShortID(id string, taken map[string]struct{}) string` — shortest prefix of `id` (minimum 4 chars) not present in `taken`.

## Internal structure

- `ids.go` — ID generation and short-ID helpers.
- `ids_test.go` — collision and prefix extension cases.

## Invariants

- `ExtendShortID` never returns a prefix shorter than four characters. There is no separate `ShortID` helper (removed; it was test-only): a short id is always `ExtendShortID` against the store's `TakenShortIDs()`.

## Side effects

None.

## Error handling

`NewSessionID` panics if `crypto/rand` fails (same pattern as typical UUID helpers).

## Tests

`ids_test.go` covers `NewSessionID` and `ExtendShortID` disambiguation.

## Gotchas

Only `internal/daemon/server` imports this package; session IDs are assigned at spawn time in `handleNewSession`.

## See also

- `modules/server.md` — assigns IDs at session creation.
- `data/schemas.md` — `short_id` on `SessionSummary`.
