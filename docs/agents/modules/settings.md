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
- `Section` constants — `SectionAppearance`, `SectionBehavior`, `SectionSpawn`, `SectionSummary`, `SectionAudio`, `SectionAdvanced`.
- `Setting` — `ID`, `Label`, `Section`, `Type`, `Default`, `Options`, `Min`, `Max`, `Help`, `ReadOnly`, `Apply`, `Hidden`, `Free`, `Normalize`. `Apply` (`ApplyLive`, `ApplyNextAttach`, `ApplyNextSpawn`, `ApplyDaemonRestart`; `ApplyMode.Label()`) says when a change takes effect and every row must set it. `Hidden` keeps a row off the TUI page (CLI and store unaffected). `Free` marks a string whose `Options` are only suggestions: the TUI also offers a text editor, and `Normalize` (run by `Store.Set`, the one validator) decides what is accepted.
- `Store` — `NewStore`, `Load`, `Save`, `Get`, `String`, `Set`, `Reset`, `Snapshot`.

### Variables

- `Registry []Setting` — full catalog (source of truth for TUI settings page and `rex config`).

### Functions

- `DefaultPath() string` — `~/.config/rex/config.yaml`.
- `Visible() []Setting` — the settings the TUI page lists (no `Hidden` ones).
- `Find(id string) (Setting, bool)`
- `SplitTrustDirs`, `TrustDir`, `CheckAutoTrustDirs`, `CanonicalDir`, `CanonicalHome` (`trust_dirs.go`) — the `auto_trust_dirs` checks. The daemon (`boot.AutoTrustDirs`, `server.underAutoTrustDir`) ignores what fails them; `rex setup` refuses it.

### Setting IDs (from `registry.go`)

`theme`, `spinner`, `row_density`, `prompt_glyph`, `reduce_motion`, `blinking_enabled`, `show_help_bar`, `header_style`, `sound_enabled`, `soundset`, `master_volume`, `notify_on_state_change`, `boot_voice`, `keybind_preset`, `mouse_enabled`, `max_concurrent_sessions`, `screensaver_enabled`, `screensaver_after_minutes`, `screensaver_keep_awake` (TUI only, `screensaver.go`), `summary_enabled`, `summary_model`, `desc_animation`, `default_spawn_tool`, `default_spawn_model`, `default_spawn_effort`, `auto_trust_dirs` (read by the daemon at each spawn via `boot.AutoTrustDirs`), `lua_config_path` (read-only in registry).

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
