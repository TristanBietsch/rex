# Settings

Settings are user preferences that affect appearance, behavior, audio, onboarding, and advanced runtime. They are surfaced in three layers:

1. A **registry** in Go that defines every setting once: id, label, section, type, default, options, help text.
2. A **TUI settings page** rendered directly from the registry. Adding a setting requires no TUI changes.
3. A **Lua config file** (`~/.config/rex/init.lua`) and a **YAML config file** (`~/.config/rex/config.yaml`) that override defaults at startup. Lua wins over YAML.

Adding a new setting is **one struct literal** in `internal/settings/registry.go`. The page, the Lua API, the CLI, and the persistence layer all pick it up automatically.

## Registry shape

```go
type Setting struct {
    ID       string         // stable identifier, e.g. "spinner"
    Label    string         // display name in the TUI
    Section  Section        // Appearance | Audio | Behavior | Onboarding | Advanced
    Type     Type           // Enum | Bool | Int | Float | String | List
    Default  any
    Options  []SettingOption // for Enum
    Min, Max any            // for Int / Float
    Help     string         // one-line description shown when selected
}

type SettingOption struct {
    ID    string  // value stored
    Label string  // display
}
```

A setting with a single `Option` is allowed — it renders as a "locked" value in the page (visible but not editable). This is the v0 default for most enums: one canonical choice now, more added over time without UI changes.

## Sections and v0 settings

### Appearance

| ID | Type | Default | v0 Options | Notes |
| --- | --- | --- | --- | --- |
| `color_scheme` | Enum | `default` | `default` | Themes are pluggable — adding one is a registry entry plus a palette JSON. |
| `spinner` | Enum | `braille` | `braille` | Future: line, arc, quadrant, half-circle, static. |
| `row_density` | Enum | `normal` | `normal` | Future: `compact` (no blank row between sections), `comfortable` (extra padding). |
| `prompt_glyph` | String | `λ` | any single grapheme | Replaces the leading glyph on the bottom prompt. Examples: `λ`, `›`, `❯`, `…`, `$`. The `:` command-mode prefix is always `:` and not affected. |
| `reduce_motion` | Bool | `false` | — | Disables every animation. Equivalent to `--no-animation`. |
| `blinking_enabled` | Bool | `true` | — | When off, the done-blink doesn't fire (row just lands in Completed). |
| `show_help_bar` | Bool | `true` | — | When off, the bottom help line is hidden — extra room for the board. Toggle with `H` keybind. |

### Audio

| ID | Type | Default | v0 Options | Notes |
| --- | --- | --- | --- | --- |
| `soundset` | Enum | `factorio` | `factorio`, `off` | `off` mutes every event. Future: `default` (clean beeps), `mechanical` (heavier clicks), custom user-defined sets. |
| `master_volume` | Float | `0.80` | 0.0 – 1.0 | Linear scale; applied to every event PCM at playback. |

### Behavior

| ID | Type | Default | v0 Options | Notes |
| --- | --- | --- | --- | --- |
| `keybind_preset` | Enum | `default` | `default` | Future: `vim` (stricter modal), `emacs`, `nano`. Lua can rebind individually regardless of preset. |
| `mouse_enabled` | Bool | `true` | — | When off, the TUI ignores mouse events; keyboard alone drives everything. |
| `max_concurrent_sessions` | Int | `16` | 1 – 64 | Daemon's PTY cap. Above this, `NewSession` returns `ErrTooManySessions`. |

### Onboarding

| ID | Type | Default | v0 Options | Notes |
| --- | --- | --- | --- | --- |
| `onboarding_models` | List | each tool's models when `enabled_by_default != false` | every model in the registry | Multi-select sub-page. Tools marked `enabled_by_default: false` in the registry (Grok, DeepSeek, Kimi) ship hidden and must be ticked here to appear in the wizard. Likewise, users can hide any default model they never use to keep the wizard short. |

### Advanced

| ID | Type | Default | v0 Options | Notes |
| --- | --- | --- | --- | --- |
| `lua_config_path` | String | `~/.config/rex/init.lua` | path | Read-only display in the page; edit on disk and `:reload`. |

## Lua config

Optional. If `~/.config/rex/init.lua` exists, it's executed once at startup. It runs in `gopher-lua` (pure Go, no cgo).

### Surface

```lua
-- ~/.config/rex/init.lua

-- set a setting by id
rex.settings.set("spinner", "braille")
rex.settings.set("prompt_glyph", "›")
rex.settings.set("max_concurrent_sessions", 24)
rex.settings.set("master_volume", 0.6)
rex.settings.set("mouse_enabled", false)

-- toggle onboarding models
rex.onboarding.hide("ollama:mistral")        -- hide from wizard
rex.onboarding.show("claude:opus")           -- ensure visible
rex.onboarding.only({ "claude:opus", "claude:sonnet" })  -- whitelist

-- custom keybind (rebind in normal mode)
rex.keys.bind("normal", "D", { action = "delete" })
rex.keys.unbind("normal", "dd")

-- register a custom command (`:foo` triggers this Lua function)
rex.commands.register("doublecheck", function(args)
    rex.notify("are you sure?")
end)

-- conditional config based on env
if os.getenv("REX_PROFILE") == "ci" then
    rex.settings.set("master_volume", 0.0)
    rex.settings.set("mouse_enabled", false)
end
```

### Resolution order

```
default value  <  ~/.config/rex/config.yaml  <  ~/.config/rex/init.lua  <  TUI override (:settings)
```

The TUI override is volatile by default — it lives in memory for the session. Pressing `enter` on a setting persists the change by:

1. Writing the new value to `~/.config/rex/config.yaml` if the user has no `init.lua` (simple case).
2. If `init.lua` exists, asking inline: *"persist to config.yaml? init.lua may override on next launch. [y/N]"* — most Lua users want the source of truth to stay Lua.

### Adding a new setting (recipe)

1. Add a `Setting` entry to `internal/settings/registry.go`.
2. Reference its `ID` from any code that needs the value via `settings.Get("foo")`.
3. **Done.** The settings page, the Lua API, and `rex config` all expose it automatically. No TUI / Lua / CLI code changes.

## TUI settings page

Triggered by:

- `:settings` (command mode)
- `S` (keybind, capital-S so it doesn't collide with `s`)

Renders as a centered modal listing every setting grouped by Section, current value shown in the right column. Vim-navigable; mouse-clickable.

| Key | Action |
| --- | --- |
| `j` / `k` | Move row |
| `enter` | Edit selected setting (sub-modal for enums, inline toggle for bool, inline numeric for int/float, text input for string) |
| `r` | Reset selected setting to default |
| `esc` | Close the page |
| `?` | Show help for the selected setting (the `Help` field) |

Editing a setting validates the value against its type/range/options. Invalid values stay in the modal with an inline error and the previous value is kept.

## CLI

```sh
rex config list                         # print all settings + current values (table or --json)
rex config get spinner                  # print one value
rex config set spinner braille          # set one value (writes to config.yaml)
rex config reset spinner                # reset to default
rex config edit                         # open init.lua in $EDITOR
```

`rex config set` exits non-zero with a useful diagnostic when the value is invalid for that setting's type/range/options.
