# How to set settings in Lua (`config.lua`)

Prerequisite: [quickstart.md](../quickstart.md).

## Goal

Keep your settings and themes in a Lua file, with conditions and shared modules, instead of (or on top of) `config.yaml`.

## Steps

1. Open the file. The first time, rex writes a commented template (0600 in a 0700 folder):

```sh
rex config edit -lua          # ~/.config/rex/config.lua in $EDITOR
```

   To start from what you already have, print `config.yaml` as Lua and paste it in:

```sh
rex config lua-export          # prints rex.set(...) lines; writes nothing
```

2. Set settings and themes:

```lua
rex.set("spinner", "moon")
rex.set("reduce_motion", true)

if rex.platform == "darwin" then
  rex.set("screensaver_keep_awake", true)
end

-- A list setting (auto_trust_dirs, board_hidden_sections) takes a Lua list.
rex.set("auto_trust_dirs", { "~/dev", "C:\\work" })

rex.theme("mine", {
  extends = "noir",
  colors = { accent = "#B392F0" },
  glyphs = { needs = "!" },
})
rex.set("theme", "mine")

local ui = require("ui")   -- ~/.config/rex/lua/ui.lua
```

3. Check what took effect:

```sh
rex config list                # the SOURCE column says "lua config.lua:3"
rex doctor                     # the config.lua row reports errors and refusals
```

The TUI and the daemon pick up a save within a few seconds (with `live_reload` on), or at once on `:reload` / `rex reload`.

## The API

| Name | What it does |
|------|--------------|
| `rex.set(id, value)` | Set a setting from `rex config list`. The value must have the setting's type (a whole number for an integer, `true`/`false` for a switch; `master_volume` also takes an old 0–1 fraction such as `0.8`, read as that share of 50); a list setting also takes a list of strings |
| `rex.theme(name, table)` | Define a theme with the theme file schema: `extends`, `dark`, `colors`, `glyphs` ([themes.md](../themes.md)) |
| `rex.platform` | `"darwin"`, `"linux"` or `"windows"` |
| `require("a.b")` | Run `~/.config/rex/lua/a/b.lua` once and return its result. Names are lower-case words joined by dots |

There is nothing else to read: no environment, no clock, no files. The values depend only on the files and the platform, so the TUI, the daemon and the CLI always agree.

## Rules

- **Precedence:** registry defaults, then `config.yaml`, then `config.lua`. A value set in Lua wins and is never written to `config.yaml`.
- **Locked rows:** the settings page shows a Lua-set row dimmed with `lua`, and refuses to change it (`<id>: set in config.lua:12 — press e to open`); `e` opens the file at that line. `rex config set` and `rex config reset` on it exit 5 with the same location. `:theme`, the quick-spawn pickers and `rex setup` leave it alone.
- **Refused calls** are warnings, not errors; the rest of the file still applies. Refused: an unknown id, `lua_config` and `lua_config_path`, a value of the wrong type, a value `rex config set` would refuse, and any string with a control, escape, bidirectional or zero-width character, or longer than 4 KiB. `rex doctor`, `rex config list` and the TUI's boot screen show them (at most 20, then a count).
- **Errors:** a syntax or runtime error, a timeout or a limit discards the whole run. The process keeps the values of its last good run (none right after start) and puts `auto_trust_dirs` and `skip_permissions` back to their defaults until the file runs again. The TUI says so on the status line, `rex doctor` and `rex config` report it, and the daemon logs one line. Other commands never print it, so their output keeps its shape. A refused `rex.set` of `auto_trust_dirs` or `skip_permissions` (wrong type, bad folder) does the same for that one setting: it reads its default, not `config.yaml`'s value, until the call is fixed.
- **Themes:** a Lua theme beats a theme file of the same name, with a warning in `rex theme list`. A theme extends a built-in only.
- **Off switch:** `rex config set lua_config false` (Settings → Features). Off, rex never reads, runs or stats `config.lua` or `lua/`, and every row is unlocked. The switch is read from `config.yaml` only.

## Limits

The file runs in a fresh, restricted Lua 5.1 state each time it changes:

- Libraries: `base`, `table`, `string`, `math`. Removed: `os`, `io`, `package`, `debug`, `coroutine`, `print`, `dofile`, `loadfile`, `load`, `loadstring`, `module`, `getfenv`, `setfenv`, `pcall`, `xpcall`, `collectgarbage`; `require` reads only `~/.config/rex/lua/` (at most 64 modules) and ignores `LUA_PATH`.
- 250 ms per run, in every process; a script stuck in a long string operation is abandoned at the deadline.
- `config.lua` and each module at most 256 KiB, 1 MiB together; `string.rep`, `string.format`, `string.gsub` and `table.concat` results at most 1 MiB; `string.format` takes Lua directives only (width and precision up to two digits, no `%[n]` or `*`); pattern functions refuse subjects over 256 KiB; a run that grows memory by about 64 MiB is stopped.
- `config.lua`, `~/.config/rex` and every folder under `lua/` must be owned by you and not writable by group or others (macOS and Linux). A symlink is followed only to a target that passes the same check. Anything else is refused with the reason.

These limits keep a mistake from hanging or flooding rex. They are not a security boundary: `config.lua` is your own code, like `init.lua` (see [lua-hooks.md](lua-hooks.md)) and [SECURITY.md](../../SECURITY.md).

## `config.lua` and `init.lua`

| | `config.lua` | `init.lua` |
|---|---|---|
| Purpose | Settings and themes | Event hooks (`rex.on`, `rex.send`) |
| Runs in | Every rex process that reads settings (TUI, daemon, CLI) | The daemon only |
| When | At start and after each change | Once at daemon start |
| Can | `rex.set`, `rex.theme`, `require` from `lua/` | Send text to sessions, any Lua file |
| Edit | `rex config edit -lua` | `rex config edit` |

## See also

- [settings.md](../settings.md) — every setting id
- [themes.md](../themes.md) — the theme schema
- [cli.md](../cli.md#rex-config) — `rex config`
