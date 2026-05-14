# Settings

Canonical notes for the Rex board settings surface.

**UI reference:** `docs/mockup.html` — screen 8 (spinner gallery), screen 13 (settings), screen 14 (models sub-page).

Settings are user preferences that affect appearance, behavior, audio, onboarding, and advanced runtime. They are surfaced in three layers:

1. A **registry** in Go that defines every setting once: id, label, section, type, default, options, help text.
2. A **TUI settings page** rendered directly from the registry. Adding a setting requires no TUI changes.
3. A **Lua config file** (`~/.config/rex/init.lua`) and a **YAML config file** (`~/.config/rex/config.yaml`) that override defaults at startup. Lua wins over YAML.

Adding a new setting is **one struct literal** in `internal/settings/registry.go`. The page, the Lua API, the CLI, and the persistence layer all pick it up automatically.

## Entry

- **`S`** from board focus, or **`:settings`** in command mode, opens the settings overlay.
- **Navigation:** `j` / `k` select row, **enter** edit, **`r`** reset row to default, **`?`** help, **esc** close.

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

A setting with a single `Option` is allowed — it renders as a "locked" value in the page (visible but not editable).

## Appearance

### Color scheme

- **Catalog:** `default` · `noir` · `paper`
- **Default selection:** `default`
- **noir:** Deeper black, cooler accents (night / OLED-friendly).
- **paper:** Light background, dark foreground (daytime / contrast variant).

### Spinner type

- **Catalog (in order, no other names in v1):**

  | # | id           | frames / notes |
  |---|--------------|----------------|
  | 1 | `braille`    | `⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏` — ~10 fps; fallback `*` when ASCII-only, non-TTY, reduce-motion, or no animation. |
  | 2 | `ascii_line` | `\| / - \` — safest fonts; same `*` fallback as braille when motion off where applicable. |
  | 3 | `moon`       | `◐ ◓ ◑ ◒` — single cell, low noise. |
  | 4 | `pulse`      | `· • ● •` — subtle heartbeat. |
  | 5 | `blocks`     | `░ ▒ ▓ █ ▓ ▒` — one-column "loading bar" feel. |

- **Removed from catalog:** `arrows` (8-direction ring) — do not ship or document.
- **Default selection:** `braille`

### Row density

- **Catalog:** `compact` · `normal` · `roomy`
- **Default:** `normal`
- **compact:** More rows visible, tighter vertical rhythm.
- **roomy:** Larger hit targets and spacing.

### Prompt glyph

- **Constraint:** exactly **one grapheme** (user-defined Unicode allowed).
- **Built-in presets:** `>` · `%` · `❯` · `▸` · `∴` (plus default **`λ`** as the Rex default).
- **Not** in preset list: `»`, `➜` (dropped in favor of `%` and `▸`).
- **Default:** `λ`

### Header style

Controls how the aggregate counts at the top render. State colors apply in every mode (yellow needs-input / blue working / green done / red failed).

- **Catalog:** `verbose` · `glyphs` · `numbers`
- **Default:** `verbose`

| id | example | notes |
|---|---|---|
| `verbose` | `2 awaiting input · 4 working · 1 completed` | Spelled out, bullet-separated. Default. |
| `glyphs`  | `◆ 2   ⠋ 4   ● 1` | State glyph + colored count. Adds the needs-input `◆`, the working spinner frame, and the done `●` (using the same glyph vocabulary as row state markers). |
| `numbers` | `2 4 1` | Just colored counts, no glyphs or labels. Densest. Yellow / blue / green left-to-right. |

Failed and crashed totals are appended in red only when non-zero, regardless of mode (`… ✕ 1` / `… ○ 2`).

### Motion & chrome

| Setting | Type | Default |
|---------|------|---------|
| reduce motion | toggle | off |
| turn off blinking | toggle | off |
| show help bar | toggle | on |

## Audio

| Setting | Default | Notes |
|---------|---------|-------|
| `sound_enabled` | `true` | Master on/off toggle. When `false`, every event is muted regardless of `soundset` or `master_volume`; the ANSI BEL fallback is also suppressed. |
| `soundset` | `factorio` | Catalog TBD ("more soundsets coming" in mockup). |
| `master_volume` | `0.80` | Step with `-` / `+`. Linear scale applied to every event PCM at playback. |

## Behavior

| Setting | Default | Notes |
|---------|---------|-------|
| `keybind_preset` | `default` | More presets TBD. |
| `mouse_enabled` | `true` | When off, the TUI ignores mouse events; keyboard alone drives everything. |
| `max_concurrent_sessions` | `16` | Daemon's PTY cap (1–64). Adjust with `-` / `+`. Above this, `NewSession` returns `ErrTooManySessions`. |

## Onboarding

- **add / rm models** — opens sub-page (mockup screen 14): per-model visibility toggles, paid/local hints.
- Tools marked `enabled_by_default: false` in the registry (Grok, DeepSeek, Kimi) ship hidden and must be ticked here to appear in the wizard.

## Advanced

- **Lua config** path: `~/.config/rex/init.lua` — **`rex config edit`** opens it.

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

## CLI

```sh
rex config list                         # print all settings + current values (table or --json)
rex config get spinner                  # print one value
rex config set spinner braille          # set one value (writes to config.yaml)
rex config reset spinner                # reset to default
rex config edit                         # open init.lua in $EDITOR
```

`rex config set` exits non-zero with a useful diagnostic when the value is invalid for that setting's type/range/options.

## Persistence & implementation notes

- Store user choices in Rex config (exact path and schema to be defined by implementation).
- Spinner implementation must respect **reduce motion** and TTY capability; match fallback rules in the table above.
- When the mockup and this doc disagree, **update this doc to match the mockup** after intentional UI changes.
