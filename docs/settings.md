# Settings reference

User settings live in `~/.config/rex/config.yaml`. Keys are flat top-level YAML entries. Unknown keys are ignored on load. Invalid values for known keys are skipped silently.

The TUI settings page (`S`), `rex config`, and the daemon read the same registry defined in code.

## Files

| Path | Role |
|------|------|
| `~/.config/rex/config.yaml` | Persisted values |

## Commands

```sh
rex config list
rex config get <id>...
rex config set <id> <value>...
rex config reset <id>...
rex config edit          # opens init.lua in $EDITOR, not config.yaml
```

## Keys

### Appearance

| ID | Type | Default | Options |
|----|------|---------|---------|
| `color_scheme` | enum | `default` | `default`, `noir`, `paper` |
| `spinner` | enum | `braille` | `braille`, `ascii_line`, `moon`, `pulse`, `blocks` |
| `row_density` | enum | `normal` | `compact`, `normal`, `roomy` |
| `prompt_glyph` | string | `λ` | Suggested graphemes in TUI |
| `reduce_motion` | bool | `false` | |
| `blinking_enabled` | bool | `true` | Completed-row blink |
| `show_help_bar` | bool | `true` | Bottom help line |
| `header_style` | enum | `verbose` | `verbose`, `glyphs`, `numbers` |

### Audio

| ID | Type | Default | Options |
|----|------|---------|---------|
| `sound_enabled` | bool | `true` | |
| `soundset` | enum | `factorio` | `factorio`, `evangelion`, `bell`, `chiptune`, `off` |
| `master_volume` | float | `0.80` | `0.0`–`1.0` |

### Behavior

| ID | Type | Default | Range | Notes |
|----|------|---------|-------|-------|
| `keybind_preset` | enum | `default` | `default` only | |
| `mouse_enabled` | bool | `true` | | |
| `max_concurrent_sessions` | int | `16` | 1–64 | See below |

`max_concurrent_sessions` in YAML does **not** set the daemon cap at startup. The daemon uses `-max-concurrent-sessions` (default 16). The TUI can apply a new cap live via the settings UI (`SetMaxConcurrent` intent). Restart the daemon to pick up a YAML change for startup behavior.

### AI summary

| ID | Type | Default | Options |
|----|------|---------|---------|
| `summary_enabled` | bool | `true` | |
| `summary_model` | string | `gemma2:2b` | Use a small model: `gemma2:2b`, `llama3.2:3b`, `llama3.2:1b`, `gemma3:1b`, `qwen2.5:3b`, `phi3:mini` |
| `desc_animation` | enum | `typewriter` | `typewriter`, `decode`, `wipe`, `off` |

Requires a running Ollama instance when enabled. If `summary_model` is not pulled, the daemon substitutes the first pulled model from that small-model list; it never falls back to an arbitrary (large) model. The board shows the session's task (`title`) until the first summary arrives. See `OLLAMA_HOST` in [paths.md](paths.md).

### Spawn

| ID | Type | Default | Notes |
|----|------|---------|-------|
| `default_spawn_tool` | string | `claude` | Tool id for `i` quick-spawn |
| `default_spawn_model` | string | `opus` | Model id on that tool |
| `default_spawn_effort` | string | `max` | Ignored if tool has no effort axis |

### Advanced

| ID | Type | Default | Notes |
|----|------|---------|-------|
| `lua_config_path` | string | `~/.config/rex/init.lua` | Read-only in TUI; path for Lua hooks |

## Example

```yaml
color_scheme: noir
sound_enabled: false
default_spawn_tool: echo
default_spawn_model: short
```

## See also

- [howto/customize-appearance.md](howto/customize-appearance.md)
- [howto/lua-hooks.md](howto/lua-hooks.md)
- [registry.md](registry.md) — tool ids for spawn defaults
- [paths.md](paths.md)
