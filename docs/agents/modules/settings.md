# settings

Human reference: **[../../settings.md](../../settings.md)** — keys, defaults, YAML shape.

**Path:** `internal/catalog/settings`
**Depends on:** (none) | external: `gopkg.in/yaml.v3`, standard library
**Depended on by:** `cmd/rex-daemon`, `internal/surface/cli`, `internal/surface/tui`
**Entry points:** `Registry`, `Store`, `Find`, `DefaultPath`, `NewStore`

## Purpose

Canonical list of Rex user settings (`Registry`) and YAML load/save (`Store`) at `~/.config/rex/config.yaml`.

## Public surface

### Types

- `Type` — `TypeEnum`, `TypeBool`, `TypeInt`, `TypeFloat`, `TypeString`.
- `Section` constants — `SectionAppearance`, `SectionAudio`, `SectionBehavior`, `SectionSummary`, `SectionSpawn`, `SectionAdvanced`.
- `Setting` — `ID`, `Label`, `Section`, `Type`, `Default`, `Options`, `Min`, `Max`, `Help`, `ReadOnly`.
- `Store` — `NewStore`, `Load`, `Save`, `Get`, `String`, `Set`, `Reset`, `Snapshot`.

### Variables

- `Registry []Setting` — full catalog (source of truth for TUI settings page and `rex config`).

### Functions

- `DefaultPath() string` — `~/.config/rex/config.yaml`.
- `Find(id string) (Setting, bool)`

### Setting IDs (from `registry.go`)

`color_scheme`, `spinner`, `row_density`, `prompt_glyph`, `reduce_motion`, `blinking_enabled`, `show_help_bar`, `header_style`, `sound_enabled`, `soundset`, `master_volume`, `keybind_preset`, `mouse_enabled`, `max_concurrent_sessions`, `summary_enabled`, `summary_model`, `desc_animation`, `default_spawn_tool`, `default_spawn_model`, `default_spawn_effort`, `lua_config_path` (read-only in registry).

## Internal structure

- `types.go` — `Setting`, `Type`, sections.
- `registry.go` — `Registry` slice.
- `store.go` — YAML persistence.
- `registry_test.go`, `store_test.go`

## Invariants

Unknown keys in YAML are ignored on load. Only `Registry` IDs are stored. Defaults apply when key absent.

## Side effects

Reads/writes `config.yaml`.

## Error handling

`Load`/`Save` return file I/O or YAML errors. `Set` validates against `Setting` type and options where enforced in `store.go`.

## Tests

Registry completeness; store round-trip; type coercion.

## Gotchas

`max_concurrent_sessions` documents "next daemon start" — live cap is `rex-daemon -max-concurrent-sessions` / `SetMaxConcurrent` intent. `lua_config_path` is read-only in UI. Adding a setting requires one `Setting` literal in `registry.go` only (comment in file).

## See also

- `interfaces/config.md` — paths and env vars.
- `modules/tui.md` — settings overlay.
- Human reference: [../../settings.md](../../settings.md).
