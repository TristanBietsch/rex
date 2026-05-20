# How to customize appearance

Prerequisite: [quickstart.md](../quickstart.md).

## Goal

Change colors, density, sounds, and header style.

## CLI

```sh
rex config set color_scheme noir
rex config set row_density compact
rex config set sound_enabled false
rex config list
```

Keys and defaults: [settings.md](../settings.md).

## TUI

1. Run `rex`.
2. Press `S` for settings.
3. Change values; they persist to `~/.config/rex/config.yaml`.

## Common keys

| ID | Effect |
|----|--------|
| `color_scheme` | `default`, `noir`, `paper` |
| `row_density` | `compact`, `normal`, `roomy` |
| `header_style` | `verbose`, `glyphs`, `numbers` |
| `prompt_glyph` | Character before the bottom prompt |
| `soundset` | UI sound catalog |
| `reduce_motion` | Disable animations |

Restart the TUI to see all changes. Audio initializes at TUI start.

## See also

- [tui.md](../tui.md) — board keys
- [settings.md](../settings.md) — full key list
