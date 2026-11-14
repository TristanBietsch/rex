# Specification: Full-Screen Settings & Help Pages with Workspace Config Persistence (ANTI)

Status: superseded by U1, U2, R04, R05, R06

**Status:** Proposed / Under Review  
**Author:** Pair Programming Agent & Tristan Bietsch  
**Related TODO Items:**
- `- [ ] make settings its own full screen page with a better layout and more options (like themes, colors, etc), as well as the help page, and make it so that the settings can be saved to a config file and loaded on startup (like cursor's .cursor) ANTI`
- `- [ ] remke the settings config in lua not yaml`
- `- [ ] per-workspace .rex folder (like cursor's .cursor) instead of JUST relying on the daemon...`
- `- [ ] remake the UI and the color scheme; better help menu, remake the help and settings page`
- `- [ ] make more settings modular and editable / hackable via lua config files and add themes`

---

## 1. Executive Summary & Problem Statement

Currently, both **Settings** (`FocusSettings`) and **Help** (`FocusHelp`) are rendered as centered floating overlay popups (`centerOverlayBox`) composited on top of the kanban board in `internal/surface/tui/tui.go`.

### Current Limitations:
1. **Cramped Visual Space:** The overlay has fixed bounds (56–88 columns width, truncated height). Long setting descriptions, help texts, and options must be ruthlessly clipped.
2. **Help is Non-Interactive & Dropping Content:** Help cannot scroll or filter; on smaller terminals, entire help groups ("Badges", "Mouse") are dropped completely. Any keypress other than `esc`/`?` is swallowed.
3. **No Workspace-Level Config:** All settings are saved globally in `~/.config/rex/config.yaml`. There is no per-workspace `.rex/` folder (analogous to Cursor's `.cursor/`) to allow folder-specific overrides (such as default agent, model, effort, auto-trust paths, or border color).
4. **Configuration in YAML vs Lua:** TODO item 7 explicitly requests migrating settings config to Lua. Currently `internal/features/lua` exists for the daemon lifecycle hooks (`init.lua`), but settings persistence is hardcoded to YAML serialization of the entire store.
5. **Limited Theme & Border Options:** Only three hardcoded themes exist (`default`, `noir`, `paper`). No support for custom themes, Pantone colorblind-safe palettes, or configurable colored window borders.

### The Objective:
Transition Settings and Help into first-class, **dedicated full-screen pages** in the Rex TUI, expand configuration options (new themes, Pantone colors, workspace borders, model matrix), provide instant search/filtering, and implement a dual-tier configuration system (Global `~/.config/rex/config.lua` + Workspace `.rex/config.lua`).

---

## 2. Design System Alignment

The new pages strictly adhere to Rex's established TUI design language:
- **Header Structure:** 
  - Left: Accent-colored `∴ REX` logo with mode badge (`› SETTINGS` or `› HELP & CHEATSHEET`).
  - Middle: Breadcrumbs, category indicators, or active search query.
  - Right: Workspace context (Workspace ID, folder basename, active config source).
  - Divider: Crisp horizontal rule (`────────...`) using `colorBorder`.
- **Color Palettes (Expanded):**
  - `default`: Dark slate obsidian background (`#0F1115`), violet accent (`#B392F0`), crisp white text (`#E6E8EE`).
  - `noir`: Deep pitch black (`#05080F`), silver border, cyan/blue accent (`#7AA6F0`).
  - `paper`: Warm minimalist newsprint (`#FBFAF7`), crisp dark gray text (`#1C1D21`), forest green / royal blue accents.
  - `dracula`: Rich purple/slate (`#282A36`), vibrant pink/cyan accents (`#FF79C6`, `#8BE9FD`).
  - `tokyo_night`: Night blue (`#1A1B26`), soft purple/blue accents (`#7AA2F7`, `#BB9AF7`).
  - `pantone_safe`: High-contrast, colorblind-friendly palette utilizing verified Pantone shades (Blue 286C, Amber 123C, Teal 326C, Vermilion 032C).
- **Typography & Glyphs:** Monospace grid alignment (`JetBrains Mono` / `SF Mono`), Unicode glyphs (`λ`, `▸`, `◂`, `◆`, `⟳`, `●`, `✕`, `○`, `◌`, `⋔`), and Braille progress indicators.
- **Footer Hints:** Dynamic context-sensitive key hints (`esc` Back, `tab` Pane switch, `↑↓` Move, `enter` Toggle, `/` Search, `:w` Save).

---

## 3. Configuration & Workspace Architecture (The `.rex/` Concept)

### 3.1 Two-Tier Precedence Hierarchy
Settings load on startup with the following cascading order (higher overrides lower):
```
1. Hardcoded Registry Defaults (`internal/catalog/settings/registry.go`)
   ↓ (overridden by)
2. Global User Config (`~/.config/rex/config.lua` or fallback `config.yaml`)
   ↓ (overridden by)
3. Workspace Config (`<workspace-root>/.rex/config.lua`)
   ↓ (overridden by)
4. Runtime CLI Flags (`rex --model=...`)
```

### 3.2 Workspace Directory (`.rex/`)
Just like Cursor uses `.cursor/` to isolate workspace rules and settings, Rex will detect a `.rex/` folder in the current working directory:
```
<project-root>/
└── .rex/
    ├── config.lua         # Project-specific settings overrides
    ├── session.id         # Unique 8-character random hex ID (e.g., "7f3b89a2")
    ├── state.db / json    # Local workspace agent states & transcript indices
    └── audit.log          # Filterable audit trail of completed tasks
```

### 3.3 Lua Configuration Format (`config.lua`)
Instead of an opaque YAML dump, Rex configuration can be authored and read as clean, idiomatic Lua:
```lua
-- Rex Workspace Configuration: .rex/config.lua
-- Auto-generated by Rex Settings TUI or manually edited

rex.config = {
  theme = "tokyo_night",
  prompt_glyph = "λ",
  
  -- Workspace Window Edge Border
  window_border = {
    enabled = true,
    style = "thick", -- "thin" | "thick" | "double"
    color = "#7AA2F7", -- Specific Pantone or hex color
    random_on_spawn = false,
  },
  
  -- Agents & Models
  spawn = {
    default_tool = "antigravity", -- "antigravity" | "claude"
    default_model = "gemini-3.8-flash",
    effort = "high",
    auto_trust_dirs = { "~/Documents/personal/dev" },
  },
  
  -- Audio & Effects
  audio = {
    enabled = true,
    soundset = "factorio", -- "factorio" | "evangelion" | "chiptune" | "bell" | "off"
    volume = 0.85,
    boot_voice = true,
  },
  
  -- UI Layout & Board
  layout = {
    fixed_columns = true,
    row_density = "normal", -- "compact" | "normal" | "roomy"
    show_help_bar = true,
    header_style = "verbose", -- "verbose" | "glyphs" | "numbers"
  }
}
```

---

## 4. Full-Screen Settings Page Specifications

### 4.1 New & Expanded Settings
| Category | Setting ID | Type | Default | Description |
|---|---|---|---|---|
| **Appearance** | `theme` | Enum | `default` | Color theme (`default`, `noir`, `paper`, `dracula`, `tokyo_night`, `pantone_safe`) |
| **Appearance** | `window_border_enabled` | Bool | `true` | Show full-window colored border identifying the workspace session |
| **Appearance** | `window_border_color` | String | `random` | Border color: `random`, `pantone_blue`, `pantone_amber`, `pantone_teal`, or custom hex |
| **Appearance** | `window_border_style` | Enum | `thick` | Border weight: `thin` (`│`), `thick` (`█` / `┃`), `double` (`║`) |
| **Appearance** | `fixed_column_widths` | Bool | `true` | Fix column widths to prevent jitter; responsive reflow on resize |
| **Workspace** | `workspace_config_scope` | Enum | `workspace` | Target save destination: `workspace` (`.rex/config.lua`) or `global` (`~/.config/rex/`) |
| **Workspace** | `workspace_id` | String | `(auto)` | 8-char session ID shown in top-left header & audit log |
| **Spawn** | `default_spawn_tool` | Enum | `antigravity` | Primary agent: `antigravity`, `claude` |
| **Spawn** | `default_spawn_model` | Enum | `flash` | Preferred model (auto-detected from agent CLI) |
| **Spawn** | `default_spawn_effort` | Enum | `high` | Thinking/reasoning effort level (`low`, `medium`, `high`, `max`) |
| **Spawn** | `auto_trust_dirs` | String | `""` | Paths skipping "trust this folder" prompts |
| **AI Summary** | `summary_model` | Enum | `gemma2:2b` | Ollama model for activity summaries |
| **Audio** | `soundset` | Enum | `factorio` | Sound pack: `factorio`, `evangelion`, `chiptune`, `bell`, `off` |
| **Audio** | `master_volume` | Float | `0.80` | Volume scale (0.0 to 1.0) |

---

## 5. Full-Screen Help Page Specifications

### 5.1 Content Structure
1. **Interactive Quick Filter (`/`):** Instant fuzzy-filtering across all commands, shortcuts, and descriptions.
2. **Keybinding Matrix:**
   - **Navigation:** `j`/`k`, `g`/`G`, `tab`, `1`/`2`/`3` (jump to Needs/Working/Done), `v` (detailed mode), `t` (tool filter).
   - **Session Actions:** `Enter` (attach), `n` (new agent), `r` (rename), `cc` (mark done / undo), `C` (kill & complete), `dd` (delete), `i` (quick prompt).
   - **Command Mode (`:`):** Complete index of all colon commands (`:new`, `:name`, `:done`, `:undone`, `:prune`, `:reload`, `:theme`, `:settings`, `:help`, `:quit`).
   - **Attached Session (`::`):** Detach (`qq` or `ctrl+]`), backscroll (`ctrl+pgup`), pass-through (`:::`).
   - **Workspace Management:** Session IDs, syncing, audit log inspection.
3. **Glyph & Status Legend:**
   - Markers: Braille `⠋` (working), `◆` (needs input), `●` (done), `✕` (failed), `○` (crashed), `◌` (queued), `⋔` (subagents running).
   - Long-running badges: `▴12m` (dim), `▴45m` (amber), `▴3h` (red), `▴7h` (red inverse).

---

## 6. The Three Design Directions (Summary)

### Option 1: "The Two-Pane Inspector" (Lazygit / Neovim style)
- **Layout:** Vertical 2-pane split. Left pane lists categories with status badges; right pane lists settings or shortcuts.
- **Strength:** Familiar to terminal power users; rapid keyboard jumping (`tab` or `h`/`l` to change panes); bottom inspector box shows rich metadata and live previews without cluttering the list.

### Option 2: "The Modern Tabbed Studio" (OpenCode / Modern Workbench)
- **Layout:** Horizontal tab bar along the top; multi-column card-based grid layout in the body.
- **Strength:** Visually spacious, highly intuitive; visual toggle pills `[ ON | OFF ]` and volume sliders `[████████░░]`; dedicated Workspace Config status strip at the bottom.

### Option 3: "The Command & Dual Lua Buffer" (Helix / Cursor-Terminal style)
- **Layout:** Split view: visual TUI setting list on the left (40%), live syntax-highlighted `.rex/config.lua` buffer on the right (60%).
- **Strength:** Perfect bridge between TUI visual controls and Lua code editing; power users can see the exact Lua config generated in real time, edit directly, or save to disk with `:w`.
