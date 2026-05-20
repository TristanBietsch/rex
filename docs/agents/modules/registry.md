# registry

**Path:** `internal/catalog/registry`
**Depends on:** (none) | external: `gopkg.in/yaml.v3`, standard library (`embed`)
**Depended on by:** `internal/daemon/adapter`, `internal/daemon/server`, `cmd/rex-daemon`, `internal/surface/tui`
**Entry points:** `Load`, `Registry`, `Tool`, `BuiltinBytes`

## Purpose

Loads the agent tool catalog: embedded `builtin.yaml` merged with optional user `tools.yaml`. Supplies spawn commands, models, and detection config for adapters.

## Public surface

### Types

- `Tool` — `ID`, `Name`, `Category`, `Command`, `CWDStrategy`, `Detect`, `Icon`, `Color`, `EnabledByDefault`, `Models`.
- `Model` — `ID`, `Name`, `Args`, `ArgsPrompt`, `Effort`.
- `Effort` — `Options`, `Default`, `ArgTemplate`.
- `Detect` — `Kind`, `Format`, `PromptRegex`, `DoneRegex`, `IdleMs`.
- `File` — YAML root `{ tools: []Tool }`.
- `Registry` — `Tools []Tool`.
- `(*Registry) Find(id string) (Tool, bool)`
- `(*Registry) FindModel(toolID, modelID string) (Tool, Model, bool)`

### Functions

- `Load(userPath string) (*Registry, error)` — merges builtin with user file when path exists.
- `BuiltinBytes() []byte` — embedded YAML bytes.

## Internal structure

- `types.go` — structs and YAML tags.
- `loader.go` — merge logic.
- `builtin.go` — `//go:embed builtin.yaml`.
- `builtin.yaml` — shipped tools (claude, codex, echo fixtures, etc.).
- `loader_test.go` — merge behavior (**note:** `TestLoad_UserExtends` currently fails in workspace — expects 4 tools, gets 3).

## Invariants

Tool `id` values must be unique after merge. User YAML extends/overrides by tool `id` (see `loader.go` for exact merge rules).

## Side effects

Reads user `tools.yaml` from disk when path provided and file exists.

## Error handling

YAML parse errors and validation failures return from `Load`. Daemon exits on registry load failure at startup.

## Tests

`loader_test.go` — builtin load, user extension.

## Gotchas

Default user path: `~/.config/rex/tools.yaml` (`rex-daemon -tools`). `SIGHUP` to daemon reloads registry via `server.SetRegistry` without killing live sessions. Repo does not ship a committed `tools.yaml`; only `builtin.yaml`.

## See also

- `modules/adapter.md` — `Detect` → adapter.
- `interfaces/config.md` — `tools.yaml` path.
- Human reference: `docs/registry.md`.
