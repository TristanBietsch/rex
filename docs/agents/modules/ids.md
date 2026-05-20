# ids

**Path:** `internal/runtime/ids`
**Depends on:** (none) | external: standard library (`crypto/rand`, `fmt`)
**Depended on by:** `internal/daemon/server`
**Entry points:** `NewSessionID`, `ShortID`, `ExtendShortID`

## Purpose

Generates RFC 4122 v4 session UUIDs and derives 4-character `short_id` prefixes for board display and selector resolution.

## Public surface

### Functions

- `NewSessionID() string` — random UUID string.
- `ShortID(id string) string` — first four hex characters of `id`.
- `ExtendShortID(id string, taken map[string]struct{}) string` — shortest prefix of `id` (minimum 4 chars) not present in `taken`.

## Internal structure

- `ids.go` — ID generation and short-ID helpers.
- `ids_test.go` — collision and prefix extension cases.

## Invariants

- `ShortID` assumes `id` is at least four characters (callers pass full UUIDs).
- `ExtendShortID` never returns a prefix shorter than four characters.

## Side effects

None.

## Error handling

`NewSessionID` panics if `crypto/rand` fails (same pattern as typical UUID helpers).

## Tests

`ids_test.go` covers `ShortID` and `ExtendShortID` disambiguation.

## Gotchas

Only `internal/daemon/server` imports this package; session IDs are assigned at spawn time in `handleNewSession`.

## See also

- `modules/server.md` — assigns IDs at session creation.
- `data/schemas.md` — `short_id` on `SessionSummary`.
