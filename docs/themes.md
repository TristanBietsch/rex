# Themes

A theme is a named set of colors (and, optionally, state marker characters) that restyles the TUI and the colored output of the `rex` commands. rex ships six; you can add your own as small YAML files.

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

`rex theme list` shows them all, your own included. `rex theme show <name>` prints one theme's colors and glyphs.

## Your own themes

Each theme is one file, `~/.config/rex/themes/<name>.yaml`. The file name (without `.yaml`) is the theme name: lowercase letters, digits, `-` and `_`, up to 32 characters, and not the name of a built-in.

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
| `fg`, `fg_dim`, `fg_muted` | Primary, secondary and quiet text |
| `bg_sel`, `border`, `border_focus` | Selection background, frames, the focused overlay frame |
| `accent` | The prompt arrow, selection marker, focused input, logo and command names |
| `working`, `queued`, `needs`, `done`, `failed`, `crashed` | One color per session state |
| `warn`, `danger`, `success`, `info` | Feedback messages |
| `fleet` | A list of 1 to 16 colors, one per fleet |

`rex theme new` lists every token with its base value. `rex theme show <name>` prints the result after `extends`.

### Glyphs

Under `glyphs:` you can replace the characters that mark a session's state and the selected row: `needs`, `done`, `failed`, `crashed`, `queued` and `select`. The working marker is the spinner (`spinner` setting). A glyph must be exactly one printable character, exactly one cell wide (emoji and other double-width characters such as `✅` are refused, because they would split the board's fixed columns), with no control, escape, bidirectional or zero-width characters. A line you leave out keeps the built-in marker.

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
| `rex theme list` | Built-in and user themes; marks the current one; reports skipped files |
| `rex theme show <name>` | A theme's colors, glyphs and a sample line |
| `rex theme new <name> [-from <builtin>] [-force]` | Write a commented skeleton; refuses to overwrite without `-force` |
| `:theme [name]` | In the TUI: set the theme, or with no name the next one |

See [cli.md](cli.md#rex-theme) for formats and exit codes, and [settings.md](settings.md) for the `theme` setting.
