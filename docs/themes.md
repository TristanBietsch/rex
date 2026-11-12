# Themes

A theme is a named set of colors (and, optionally, state marker characters) that restyles the TUI and the colored output of the `rex` commands. rex ships nine (six built-ins and three bundled theme files), plus `auto`, which follows your terminal's background; you can add your own as small YAML files.

Pick one with `rex config set theme <name>`, in the settings page (`S`, row **Theme**), or with `:theme <name>` in the TUI (`:theme` alone cycles). The TUI applies it at once and saves it; CLI output follows it. An unknown name uses `default` and the TUI says so on the status line.

The setting was called `color_scheme`. The old key is still read; if a file holds both keys, `theme` wins. The next save writes only `theme`.

## Built-in themes

| Theme | For |
|-------|-----|
| `default` | Dark terminals. Violet accent |
| `classic` | The look before the violet accent |
| `noir` | A deeper, quieter dark |
| `paper` | Light terminals |
| `high-contrast` | Pure black and white with bright state colors |
| `ansi` | Your terminal's own 16 colors; the selection is reverse video |

### Ready and Done greens

READY and DONE are both green in every theme: a bright, saturated "go" green for READY (the `needs` token: the marker `⚑`, the section title, the header's `N ready`, the `? asks` slot) and a calmer, darker green for DONE (`done`: the marker `✓`). Warnings keep the amber (`warn`). rex's tests hold every shipped theme to this: both greens read at 4.5:1 on the theme's `bg`, they are at least ΔE 20 (CIE76) apart and fall on different 256-color indexes, READY is the more saturated one (and the brighter one on dark themes), and the glyphs differ.

| Theme | READY (`needs`) | DONE (`done`) | ΔE |
|-------|-----------------|---------------|----|
| `default` | `#4BE37F` 11.3:1 | `#4E9E6E` 5.8:1 | 38.7 |
| `classic` | `#4ADE80` 10.8:1 | `#4E9A6A` 5.5:1 | 36.7 |
| `noir` | `#4FD982` 11.1:1 | `#4A9A6C` 5.9:1 | 32.7 |
| `paper` | `#14853B` 4.5:1 | `#3D6650` 6.3:1 | 36.5 |
| `high-contrast` | `#3CF07A` 13.9:1 | `#57B07A` 7.9:1 | 43.0 |
| `ansi` | `2` (green) | `10` (bright green) | terminal |
| `dracula` | `#50FA7B` 12.1:1 | `#52A877` 5.7:1 | 50.7 |
| `tokyo_night` | `#9ECE6A` 9.4:1 | `#4FA57A` 5.7:1 | 34.1 |
| `pantone_safe` | `#B2D040` 10.8:1 | `#009473` 4.9:1 | 62.9 |

`ansi` uses the terminal's own green for READY and bright green for DONE. A terminal that maps bright green to a gray (Solarized) dims DONE, the state that should recede, never READY.

**Color vision.** For deuteranopia and protanopia, green next to green is the hard case, so the glyph carries the meaning first (`⚑` vs `✓`, also on a terminal with no color). The two greens also differ in lightness, which survives color-vision deficiency: in every shipped theme they stay at least 6.5 apart (OKLab ×100) under simulated protan, deutan and tritan vision. `pantone_safe` goes further and keeps every pair of its state colors that far apart. On the other themes READY's green can sit close to FAILED's red for a reader with deuteranopia; `⚑` and `✗` still differ.

## Bundled themes

| Theme | For |
|-------|-----|
| `dracula` | The [Dracula](https://draculatheme.com) palette |
| `tokyo_night` | The [Tokyo Night](https://github.com/folke/tokyonight.nvim) "night" palette |
| `pantone_safe` | State colors kept apart for protan, deutan and tritan vision, Pantone Color of the Year inspired (READY Greenery-lime, DONE Emerald) |

These ship as theme files built into rex, written in the same format as your own. `dracula` and `tokyo_night` take their hues from the published palettes; secondary text is a little lighter than the originals so it stays readable. Two differences from the six built-ins:

- They are not bases. `extends:` and `rex theme new -from` accept only the six built-ins.
- A file of your own with the same name (`~/.config/rex/themes/dracula.yaml`) replaces the bundled theme. `rex theme new dracula` starts one from `default` (or from another built-in with `-from`), not from the bundled colors.

`rex theme list` shows them all, your own included.

### auto

`auto` is not a theme of its own: it is `default` on a dark terminal and `paper` on a light one. It is opt-in; `default` stays the default.

```sh
rex config set theme auto      # or :theme auto, or the settings page
```

The TUI decides once, when it starts, and only when the theme is `auto`: first from `COLORFGBG` (set by rxvt, Konsole and some others; background 7 or 9-15 is light), and when that is unset or says `default`, by asking the terminal for its background color (OSC 11). It asks before the board takes over the keyboard, so the answer can never be read as keys. A terminal that does not answer counts as dark.

Switching to `auto` later (`:theme auto`, the settings page, live reload of `config.yaml`) never asks the terminal again: it reuses what was found at start, or, if the TUI started with another theme and so never asked, picks `default`. Restart the TUI to pick up a terminal whose background changed.

CLI output (`rex ls`, `rex --help`, ...) and `rex setup` never ask the terminal: for them `auto` is `default`. `rex theme list` shows `auto` with source `auto`. A theme file cannot be called `auto.yaml`. `rex theme show <name>` prints one theme's colors and glyphs.

## Accent color

The accent paints the prompt arrow (`λ`), the selection marker (`▸`), the focused input, the modified-setting bullet, key caps in the attach chrome, the row icon strip's unseen mark and command names in help and CLI output. Each theme has its own (violet in `default`). `accent_color` replaces it on top of whatever theme is active, built-in, bundled, a theme file or a `config.lua` theme, and changes nothing else:

```sh
rex config set accent_color very-peri          # a named color
rex config set accent_color "#ff6f61"           # quote it: # starts a shell comment
rex config set accent_color theme               # back to the theme's own (the default)
```

In the TUI: `:accent <color>` (`:accent` alone steps to the next name), or the settings page (`S`, row **Accent color**): `←`/`→` cycle `theme` and the names, `enter`, `e` or `#` open an editor for your own value with a live preview and the reason a value is refused. Lua: `rex.set("accent_color", "#ff6f61")`. A trusted workspace's `.rex/config.lua` may set it too.

**Named colors.** Pantone Color of the Year inspired. The hex values are published sRGB approximations, not official Pantone values, and rex is not affiliated with Pantone. Names shared with the border colors ([settings.md](settings.md#border-colors)) use the same dark and light variants.

| Name | Reference | Hex (dark / light) |
|------|-----------|--------------------|
| `very-peri` | 17-3938 Very Peri | `#6667AB` |
| `classic-blue` | 19-4052 Classic Blue | `#4F83C0` / `#0F4C81` |
| `cerulean` | 15-4020 Cerulean | `#9BB7D4` / `#758FA9` |
| `emerald` | 17-5641 Emerald | `#009473` |
| `greenery` | 15-0343 Greenery | `#88B04B` |
| `illuminating` | 13-0647 Illuminating | `#F5DF4D` / `#A18C00` |
| `mimosa` | 14-0848 Mimosa | `#F0C05A` / `#B18513` |
| `tangerine` | 17-1463 Tangerine Tango | `#DD4124` |
| `living-coral` | 16-1546 Living Coral | `#FF6F61` |
| `peach-fuzz` | 13-1023 Peach Fuzz | `#FFBE98` |
| `rose-quartz` | 13-1520 Rose Quartz | `#F7CAC9` |
| `viva-magenta` | 18-1750 Viva Magenta | `#BE3455` |
| `orchid` | 18-3224 Radiant Orchid | `#B163A3` |
| `ultra-violet` | 18-3838 Ultra Violet | `#5F4B8B` |
| `mocha-mousse` | 17-1230 Mocha Mousse | `#A47864` |
| `ultimate-gray` | 17-5104 Ultimate Gray | `#939597` / `#8A8C8E` |

**Your own color.** Case and spaces do not matter (`Very Peri`, `very_peri` and `very-peri` are one name). Values are separated by commas or spaces.

| Form | Example | Notes |
|------|---------|-------|
| `#rrggbb`, `#rgb` | `#ff6f61`, `#f0f` | |
| `rgb(r, g, b)` | `rgb(255, 111, 97)`, `rgb(100%, 44%, 38%)` | 0–255 or 0–100% per channel |
| `hsl(h, s%, l%)` | `hsl(5, 100%, 69%)` | Hue in degrees (wraps; `deg` allowed); `%` optional |
| `cmyk(c, m, y, k)` | `cmyk(0, 56, 62, 0)`, `cmyk(0, .56, .62, 0)` | 0–100%, or 0–1 when every plain value is at most 1. A naive conversion with no ink profile: printed colors only roughly match it |
| `oklch(l c h)` | `oklch(0.7 0.15 30)`, `oklch(70% 0.15 30)` | Lightness 0–1 or %, chroma 0–0.5 (100% = 0.4), hue in degrees. A color outside sRGB loses chroma until it fits |

Anything else is refused with the reason (`rex config set`, `rex.set` in Lua, `:accent`, the settings editor), and the old value stays. The setting keeps the value as you typed it (lowercased), so the settings row shows what you chose; it is turned into a color whenever a theme is applied. A value edited into `config.yaml` by hand that does not parse is ignored: the theme's accent is used and the boot log says why.

**Contrast.** On a theme with a real background color, the accent must reach 4.5:1 against it, the same floor every shipped theme's accent meets. A color below it is not refused: rex lightens it (dark theme) or darkens it (light theme) in OKLCH, keeping its hue, until it passes. The status line says so (`accent #6667AB adjusted to #7476BB for contrast on default`), and so do the settings page's detail pane (`paints #7476BB (from #6667AB, for contrast)`) and `rex config get accent_color`. Switching themes re-checks it, so one value works on dark and light themes. On a theme with a light and a dark variant (`classic-blue`, `cerulean`, `illuminating`, `mimosa`, `ultimate-gray`), the variant for the theme's mode is used first. On `ansi` the background is the terminal's, so the color is used as given.

**State colors.** The accent should not read as a session state. A color closer than ΔE 20 (CIE76) to the theme's working, ready (`needs`), done or failed color is kept, with a warning (`accent #FF6F61 is close to the failed state color`). Since READY and DONE are green, a green accent warns about one of them.

## Your own themes

Each theme is one file, `~/.config/rex/themes/<name>.yaml`. The file name (without `.yaml`) is the theme name: lowercase letters, digits, `-` and `_`, up to 32 characters, and not `auto` or the name of one of the six built-ins. A bundled theme's name is fine: your file replaces that theme.

Start from a commented skeleton:

```sh
rex theme new my-theme -from noir     # writes ~/.config/rex/themes/my-theme.yaml (0600)
$EDITOR ~/.config/rex/themes/my-theme.yaml
rex config set theme my-theme
```

A theme file:

```yaml
extends: noir          # optional built-in to start from; default "default"
dark: true             # optional: is the background dark?
colors:
  accent: "#FF7A59"
  needs: "#F2B84B"
  fleet: ["#5B8DEF", "#E5B341", "#4ADE80"]
glyphs:
  needs: "!"
  done: "+"
  select: ">"
  loop: "@"
```

Everything you leave out comes from the base theme. An empty file is a copy of `default`. A theme extends a built-in only, not another user theme.

`config.lua` can define themes too, with the same schema and the same checks: `rex.theme("mine", { extends = "noir", colors = { accent = "#B392F0" } })` ([howto/lua-config.md](howto/lua-config.md)). A Lua theme beats a file of the same name, and `rex theme list` warns about the shadowed file and shows the theme's source as `lua`.

### Colors

A color is one of:

| Form | Meaning |
|------|---------|
| `"#RRGGBB"` or `"#RGB"` | A 24-bit color |
| `"0"` to `"255"` | A terminal palette index (your terminal's theme decides what it looks like) |
| `""` | The terminal's own color |

**Quote every color.** An unquoted `#` starts a YAML comment, so `accent: #FF7A59` is an empty value, and rex tells you so.

Tokens under `colors:`:

| Token | Paints |
|-------|--------|
| `bg` | The assumed terminal background. Never painted: used for contrast checks |
| `fg`, `fg_dim`, `fg_muted` | Primary (also the `∴ REX` logo), secondary and quiet text |
| `bg_sel`, `border`, `border_focus` | Selection background (settings and list pages; board rows have none), frames, the focused overlay frame |
| `accent` | The prompt arrow, selection marker, focused input and command names |
| `working`, `queued`, `needs`, `done`, `failed`, `crashed` | One color per session state (`needs` is READY) |
| `warn`, `danger`, `success`, `info` | Feedback messages |
| `fleet` | A list of 1 to 16 colors, one per fleet |

`rex theme new` lists every token with its base value. `rex theme show <name>` prints the result after `extends`.

### Glyphs

Under `glyphs:` you can replace the characters that mark a session's state and the selected row: `needs` (`⚑`), `done` (`✓`), `failed` (`✗`), `crashed` (`⊘`), `queued` (`◌`), `select` (`▸`) and `loop` (`↻`, the marker of a loop session's row). The working marker is the spinner (`spinner` setting). A glyph must be exactly one printable character, exactly one cell wide (emoji and other double-width characters such as `✅` are refused, because they would split the board's fixed columns), with no control, escape, bidirectional or zero-width characters. A line you leave out keeps the built-in marker.

### What rex checks

A file that breaks a rule below is skipped, with the reason in `rex theme list`, the boot log (a WARN row) and, if it is the configured theme, on the status line. The TUI starts anyway, with `default`. Nothing in a theme file can stop rex from starting.

- Unknown keys, unknown color or glyph tokens: a warning; the rest of the file still loads.
- A color or glyph that is not valid, an unknown `extends`, a name that is invalid or a built-in's, a file over 64 KiB, more than 64 files: the file (or the files past the limit) is skipped.
- Low contrast between a color and the theme's own `bg` (text under 4.5:1, dim text and the accent and state colors under 3:1): a warning only.
- Symlinks work, so a theme can live in a dotfiles repo and be linked in (GNU stow does this). What the link points to must be a regular file that passes the same checks as any other theme file: owned by you, not writable by group or others, within the size cap. A dangling or looping link, a link to a directory, a FIFO or a device, and a link to a file someone else owns or others can write are skipped, with the reason. On Windows a link must stay inside the themes directory.
- On macOS and Linux a theme file (or the file a link points to), and the `themes` directory, must be owned by you and not writable by group or others. Otherwise rex skips them and says `chmod go-w`. On Windows this check does not apply.

Theme files are data, never code. They are safe to share, but rex still treats a downloaded one as untrusted: nothing in it can reach your terminal except through the checks above.

## Live reload

While `live_reload` is on (the default), the TUI notices an edit to `config.yaml`, to a theme file or (with `lua_config` on) to `config.lua` within about 2 seconds and restyles without a restart. Save a theme file and the TUI changes as you type. `:reload` does the same at once, with `live_reload` off too. A `config.yaml` that fails to parse leaves the previous settings in place and says so on the status line.

## Commands

| Command | Does |
|---------|------|
| `rex theme list` | Built-in and user themes and `auto`; marks the current one; reports skipped files |
| `rex theme show <name>` | A theme's colors, glyphs and a sample line |
| `rex theme new <name> [-from <builtin>] [-force]` | Write a commented skeleton; refuses to overwrite without `-force` |
| `:theme [name]` | In the TUI: set the theme, or with no name the next one |
| `:accent [color]` | In the TUI: set `accent_color` (see [Accent color](#accent-color)), or with no value the next name |

See [cli.md](cli.md#rex-theme) for formats and exit codes, and [settings.md](settings.md) for the `theme` setting.
