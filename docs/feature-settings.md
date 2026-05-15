# Feature: Settings (TUI)

Canonical notes for the Rex board settings surface. **UI reference:** `docs/mockup.html` — screen 8 (spinner gallery), screen 13 (settings), screen 14 (models sub-page).

## Entry

- **`S`** from board focus, or **`:settings`** in command mode, opens the settings overlay.
- **Navigation:** `j` / `k` select row, **enter** edit, **`r`** reset row to default, **`?`** help, **esc** close.

## Appearance

### Color scheme

- **Catalog:** `default` · `noir` · `paper`
- **Default selection:** `default`
- **noir:** Deeper black, cooler accents (night / OLED-friendly).
- **paper:** Light background, dark foreground (daytime / contrast variant).

### Spinner type

- **Catalog (in order, no other names in v1):**

  | # | id | frames / notes |
  |---|-----|------------------|
  | 1 | `braille` | `⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏` — ~10 fps; fallback `*` when ASCII-only, non-TTY, reduce-motion, or no animation. |
  | 2 | `ascii_line` | `\| / - \` — safest fonts; same `*` fallback as braille when motion off where applicable. |
  | 3 | `moon` | `◐ ◓ ◑ ◒` — single cell, low noise. |
  | 4 | `pulse` | `· • ● •` — subtle heartbeat. |
  | 5 | `blocks` | `░ ▒ ▓ █ ▓ ▒` — one-column “loading bar” feel. |

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

### Motion & chrome

| Setting | Type | Default |
|---------|------|---------|
| reduce motion | toggle | off |
| turn off blinking | toggle | off |
| show help bar | toggle | on |

## Audio

| Setting | Default | Notes |
|---------|---------|--------|
| soundset | factorio | Catalog TBD (“more soundsets coming” in mockup). |
| master volume | 80% | Step with `-` / `+`. |

## Behavior

| Setting | Default | Notes |
|---------|---------|--------|
| keybind preset | default | More presets TBD. |
| mouse enabled | on | |
| max concurrent sessions | 16 | Adjust with `-` / `+`. |

## Onboarding

- **add / rm models** — opens sub-page (mockup screen 14): per-model visibility toggles, paid/local hints.

## Advanced

- **Lua config** path: `~/.config/rex/init.lua` — **`rex config edit`** opens it.

## Persistence & implementation notes

- Store user choices in Rex config (exact path and schema to be defined by implementation).
- Spinner implementation must respect **reduce motion** and TTY capability; match fallback rules in the table above.
- When the mockup and this doc disagree, **update this doc to match the mockup** after intentional UI changes.
