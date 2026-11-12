# How to customize appearance

Prerequisite: [quickstart.md](../quickstart.md).

## Goal

Change colors, density, sounds, and header style.

## CLI

```sh
rex config set theme noir
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
| `theme` | `default`, `classic`, `noir`, `paper`, `high-contrast`, `ansi` |
| `row_density` | `compact`, `normal`, `roomy` |
| `header_style` | `verbose`, `glyphs`, `numbers` |
| `window_border` | Thick colored frame around the screen (off by default) |
| `window_border_color` | `random` (a color per project folder), a name such as `tangerine`, or `#rrggbb` |
| `prompt_glyph` | Character before the bottom prompt |
| `soundset` | UI sound catalog |
| `reduce_motion` | Disable animations |

`:border [on|off|next|random|<color>]` changes the frame for one run without touching the settings.

Colors, glyphs and density apply as soon as you change them; audio initializes at TUI start. The settings page tells you per row when a change applies.

## Themes

| Scheme | Use it when |
|--------|-------------|
| `default` | Dark terminal; violet accent. This is what you get if you never set one |
| `classic` | You liked the look before the violet accent (`rex config set theme classic`) |
| `noir` | You want a deeper, quieter dark |
| `paper` | Your terminal background is light |
| `high-contrast` | Pure black and white, brightest state colors; for low vision or glare |
| `ansi` | You want rex to use your terminal theme's own 16 colors (Solarized, Gruvbox, ...); the selection is reverse video |

The schemes with real colors meet contrast targets against their own background (text 7:1, state colors and accent 4.5:1), so pick `paper` on a light terminal rather than forcing a dark scheme. CLI output (`rex --help`, `rex ls`, ...) follows the same scheme.

## Find and edit a setting in the TUI

- `/` searches by label, id, section or help text; `tab` jumps between sections.
- A `•` marks a value you changed; the pane under the list shows the id (for `rex config set`), the default, the options and when the change applies.
- `enter` on `prompt_glyph` or `auto_trust_dirs` edits the text in place.

## See also

- [tui.md](../tui.md) — board keys
- [settings.md](../settings.md) — full key list
