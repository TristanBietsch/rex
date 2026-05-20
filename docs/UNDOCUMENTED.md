# Undocumented and unclear behavior

Facts not fully specified in code comments or stable APIs. Listed for maintainers and doc readers.

## Protocol

| Item | Location | Notes |
|------|----------|-------|
| `IntentOpenSession` | `internal/wire/protocol/intents.go` | Constant exists; server returns `bad_intent` |
| `IntentShutdown` | same | Not implemented |
| `FocusFilter` | `server/client.go` | Accepted, no-op; board filter is client-side |
| `ErrCodeAmbiguousID` | `protocol/events.go` | Defined, never emitted |
| `ErrCodeRegistryInvalid` | same | Defined, never emitted |
| `ErrCodeBadState` | same | Defined, never emitted |

## CLI

| Item | Location | Notes |
|------|----------|-------|
| `@needs`, `@working`, `@done` | `cli/core/help.go` | Documented in help; not in `selector.go` |
| `-short` on `rex ls` | `session/ls.go` | Flag parsed, no effect |
| `-no-attach` on `rex new` | `session/new.go` | Flag parsed, no effect |
| `enabled_by_default` merge | `registry/loader.go` | User YAML cannot override builtin opt-in flag |

## Settings

| Item | Location | Notes |
|------|----------|-------|
| `SectionOnboarding` | `settings/types.go` | No settings use this section |
| `max_concurrent_sessions` help text | `registry.go` | Says "next daemon start"; TUI can live-update cap |

## TUI

| Item | Location | Notes |
|------|----------|-------|
| `tui-state.json` `scroll_offset` | `tui/persist.go` | Saved on read path; save path may omit it |

## Follow-ups (out of scope for this doc pass)

- `docs/bench.md` for Makefile bench targets
- Packaged man pages (`rex(1)`)
- Third-party client SDK beyond in-repo `wire/client`
- Whether to implement or remove dead flags and selector aliases
