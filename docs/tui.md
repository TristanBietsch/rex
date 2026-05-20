# TUI behavior

## Screen anatomy

```
┌────────────────────────────────────────────────────────────────────────────────────────────┐
│  ∴ REX                                                                                      │
│  2 awaiting input · 4 working · 1 completed                                       [+ new]   │   ← header
│  all · claude · codex · gemini · ollama                                                     │   ← filter chips
├────────────────────────────────────────────────────────────────────────────────────────────┤
│  Needs input                                                                                │
│  ◆ 7d4f  dark-mode           system theme vs explicit toggle…        opus · high      4m   │
│  ◆ b203  release-notes       draft ready — which feature leads?      gpt-5 · medium  11m   │
│                                                                                             │
│  Working                                                                                    │
│  ⠋ 3c8a  perf-audit          events_org_ts index live — p95 38ms     sonnet · def     7m   │
│  ⠋ 9e51  payment-migration   porting billing to the new processor    gpt-5-codex · m   2m   │
│  ⠋ 4f6d  onboarding-copy     rewriting empty-state copy across 6…    2.5-pro           1m   │
│  ⠋ c14a  load-test           k6 against the launch traffic profile   llama3.1          3m   │
│                                                                                             │
│  Completed                                                                                  │
│  ● 2b7e  test-coverage       billing/ from 61% → 92% — PR #408       haiku · def       9m   │
├────────────────────────────────────────────────────────────────────────────────────────────┤
│  λ describe a task for a new session                                                        │   ← prompt
│  i focus · enter open · space peek · n new · t filter · : command · ? help                  │
└────────────────────────────────────────────────────────────────────────────────────────────┘
```

Each row has six columns:

| # | Column | Width | Description |
| --- | --- | --- | --- |
| 1 | **state** | 1.4 ch | Braille spinner (working), `◆` (needs input), `●` (done), `✕` (failed), `○` (crashed). The only colored cell on the row. |
| 2 | **id** | 5 ch | Four-char hex (e.g. `7d4f`) — first four chars of the session UUID. Stable across daemon restarts. Used to disambiguate identical-looking slugs and as a target for `rex attach 7d4f` / `rex rm 7d4f` / `:rm 7d4f`. |
| 3 | **slug** | ~22 ch | Short identifier (kebab-case). `fg.primary`, bold. |
| 4 | **description** | flex (ellipsis) | Last-line summary from the agent. `fg.dim`. |
| 5 | **model · effort** | ~18 ch | Tool model id and effort, e.g. `opus · high`, `gpt-5-codex · med`, or just the model id when no effort applies (`2.5-pro`, `llama3.1`). `fg.dim`. |
| 6 | **time** | 5 ch | Elapsed since `last_event_at`, right-aligned. `fg.dim`. |

State is the only color on the board. Slug, description, model, and time are typeset in `fg.primary` and `fg.dim` — no per-tool tint. The tool itself is recoverable through the filter chips at the top (`t` cycles), through the model id in column 5 (each model name is unique to its tool), and through the modal top strip (visible when a session is open).

## Selection model

- Exactly one row is selected at any time. Selection is sticky across state changes (the row follows its session).
- The selected row has a one-character indent plus a background tint highlight.
- When the focused section empties, selection jumps to the next non-empty section.

## Keybinds

| Key | Action |
| --- | --- |
| `j` / `k`, `↓` / `↑` | Move row selection |
| `g g` / `G` | Top / bottom |
| `1` / `2` / `3` | Jump to Needs input / Working / Completed |
| `enter` | Open modal on selected session |
| `space` | Peek last turn / inline reply when state is `needs_input` |
| `n` | Open new-agent wizard |
| `t` | Cycle tool filter chip |
| `/` | Open the slash command palette (fuzzy picker; type to filter; see `slash.md`). Default fallback is `/find` so `/<query><enter>` still searches. |
| `r` | Rename selected session (inline) |
| `a` | Archive selected (only valid in Completed) |
| `d d` | Delete (vim-style — press `d` twice) |
| `c` | Complete: cleanly terminate the selected session and mark it done |
| `i` | Focus bottom prompt (λ — new-session text) |
| `:` | Enter command mode (Neovim-style; only valid from board focus) |
| `S` | Open the settings page (capital S to leave lowercase `s` free) |
| `H` | Toggle the bottom help bar |
| `esc` | Defocus prompt / close modal / cancel wizard / clear search / exit command mode |
| `z` | Zoom modal to fullscreen (toggle) |
| `?` | Help overlay |
| `q` | Quit TUI (daemon keeps running) |

## Mouse

Bubble Tea's built-in mouse protocol handles motion and click events.

- **Click row** — select.
- **Double-click row** — open modal (same as `enter`).
- **Click `[+ new]`** — open wizard.
- **Click filter chip** — set filter.
- **Click bottom prompt** — focus prompt (same as `i`).
- **Click `›` send glyph** (rendered at the right of the prompt when the prompt is non-empty) — submit.
- **Scroll** — scroll the focused region (board or modal viewport).

Modal-specific:

- **Click outside the modal** — close.
- **Click inside the modal but outside the reply box** — focus the PTY (subsequent keystrokes forward as bytes).

## State machine

```
              ┌────────────┐
              │   queued   │
              └──────┬─────┘
                     │ spawn
                     ▼
        ┌─── working ◀───────┐
spawn   │       │            │
error   │       │ adapter says "input needed"
  ▼     │       ▼
failed  │  needs_input
        │       │ user replies / inline reply
        │       └─────────────► working
        │
        │ adapter says "turn ended" and no further input expected
        ▼
      done
        │
        ▼
   archived (hidden by default; visible with --show-archived)
```

Plus a top-level `crashed` assigned at daemon startup to sessions whose PTY didn't survive.

When a row enters `done`, the TUI plays the `done` audio tone and runs the *Done blink* animation: four background flashes in `state.done` green, ~1.6 s total, then the row returns to its normal appearance (see `design.md`).

## Modal (Enter behavior)

- Centered overlay, ~85% of width and ~85% of height, scale-in 120 ms ease-out.
- Top strip: **tool monogram (colored)**, slug, model, state pill. This is where the tool's brand color appears on screen.
- Body: PTY viewport, fed by `SessionOutput` events.
- Bottom: reply line. `enter` submits, `shift+enter` inserts a newline, `esc` closes.
- `z` toggles fullscreen (board hidden, viewport fills the terminal).
- `space` outside the modal performs **peek**: opens the modal in read-only mode, no reply box, dismisses on any key.

## New-agent wizard

Adaptive 5-step flow, centered modal, scale-in. Tool monograms in the wizard are rendered with their **brand color** — this is the wizard's job, to show what you're about to spawn.

| Step | Title | Skipped when |
| --- | --- | --- |
| 1 | Choose a provider | never |
| 2 | Choose a model | the tool has exactly one model |
| 3 | Reasoning effort | the selected model has no `effort` block |
| 4 | Name your agent | never |
| 5 | Confirm and launch | never |

All option lists are vim-navigable (`j`/`k`/`enter`) and mouse-clickable. `b` goes back, `esc` cancels, `tab` cycles fields in step 4.

After launch the modal closes, the new row pulse-in animates inside Working, and the create tone plays.

### Step 1 — provider

Groups entries by `category`:

```
Paid
  ◆ Claude Code         opus / sonnet / haiku
  ◇ OpenAI Codex        gpt-5 / gpt-5-codex
  ◈ Gemini CLI          2.5 pro / 2.5 flash

Self-hosted
  ◉ Ollama              llama3.1 / mistral / custom
```

### Step 2 — model

Shows each model with display name and a short hint (e.g., "most capable" / "balanced" / "fastest").

### Step 3 — effort (skipped when not applicable)

Lists `effort.options` with the current default pre-selected.

### Step 4 — name

Three fields, tab-cycled:

- `slug` (required, kebab-case, derived from cwd basename + timestamp if blank)
- `title` (optional one-line description)
- `cwd` (defaults to `$PWD`, editable)

### Step 5 — confirm

Shows the resolved command:

```
Claude Code · Opus 4.7 · effort: high
~/code/acme · slug: payment-migration

[ launch ]   [ back ]   [ cancel ]
```

`enter` submits a `NewSession` intent. If the daemon returns `Error`, the modal stays open and surfaces the message; otherwise it closes.

## Bottom prompt

Always visible. Has two modes:

### λ mode (default, new-session text)

The prompt line shows `λ` at the left and accepts a free-form prompt. Typed text registers only when focused (`i` or click). `enter` submits a new session with the currently filtered tool's default model, slug derived from the first 32 characters of the prompt, initial prompt set to the typed text.

The wizard is preferred for first-time use; the λ prompt is for power users with a default tool already configured.

### `:` command mode (Neovim-style)

Pressing `:` while focused on the board (not on the λ prompt) switches the bottom line to command mode. The prefix changes to `:` (in `state.needs` yellow) and the input is interpreted as a command.

| Command | Effect |
| --- | --- |
| `:q` / `:quit` | Quit the TUI. **Shows a confirm prompt** (see *Quit confirmation* below). The daemon keeps running. Does **not** save TUI state — next `rex` starts on a clean board. |
| `:q!` | Force-quit the TUI immediately, no confirm prompt. Vim convention — `!` = "I know what I'm doing". |
| `:bg` / `:detach` | Detach the TUI without confirm. Saves selection + filter + scroll to `~/.local/state/rex/tui-state.json` so the next `rex` invocation resumes exactly where you left off. Daemon and sessions continue. |
| `:new` | Open the new-agent wizard (same as `n`). |
| `:filter <tool>` | Set the filter chip. `<tool>` ∈ `all`, `claude`, `codex`, `gemini`, `ollama`, or any registry id. |
| `:attach <selector>` | Open the modal on a session. `<selector>` ∈ four-char hex id, slug, or full UUID. |
| `:reply <selector> <text>` | Send a one-shot reply to a session in `needs_input`, without opening the modal. Mirrors `rex reply`. |
| `:rm <selector>` | Delete a session. |
| `:rename <selector> <new-slug>` | Rename a session. |
| `:archive <selector>` | Archive a completed session. |
| `:reload` | Re-read `tools.yaml`, `config.yaml`, and `init.lua` (equivalent to `kill -HUP rex-daemon`). |
| `:settings` | Open the settings page (same as the `S` keybind). See `settings.md`. |
| `:help` | Open the help overlay (same as the `?` keybind). |

- `enter` executes the command. On success the bottom line returns to λ mode. On error the message is rendered inline below the prompt for ~2 seconds, then the command line is restored for editing.
- `esc` cancels and returns to λ mode.
- `tab` triggers completion (v0: completes command names; selector completion is planned for v1).
- The command vocabulary intentionally mirrors keybinds so muscle memory transfers either way.

`:` is only honored from **board focus** — when the λ prompt is focused (`i`), `:` is just a character in the prompt text.

### Backgrounding and reattach

The daemon supervises sessions regardless of whether the TUI is attached, so "running in the background" is really "detaching the TUI cleanly":

- **Detach (`:bg` or `:detach`).** Persists `selection`, `filter`, and `scroll_offset` to `~/.local/state/rex/tui-state.json`, then exits the TUI. The shell prompt returns. Sessions keep working.
- **Check from outside.** `rex status` (one-liner) and `rex ls` (table) hit the same daemon socket the TUI uses. They print and exit.
- **Reattach.** Just run `rex` again. If `tui-state.json` exists, it's loaded and the previous selection / filter / scroll are restored. Otherwise default state. The state file is removed after a successful reattach.

`:bg` is the right command for *"I'm stepping away and coming back"*. `:q` is for *"I'm done for now."*

### Quit confirmation

When `:q` or `:quit` is executed, the bottom prompt line transforms into an inline confirm:

```
quit rex? running sessions stay alive in the daemon — y / N
```

- `y` / `Y` / `enter` → quit TUI.
- `n` / `N` / `esc` → cancel, returns to the λ prompt (empty).

`:q!` skips the confirm and quits immediately.

The `q` keybind (single-key quit) also skips the confirm — it's the muscle-memory escape hatch for users who know what they want. Only command-mode `:q`/`:quit` gates on the prompt.

## Help overlay

`?` (keybind) and `:help` (command) both open the same overlay — a centered modal that lists every interaction in four titled sections:

```
┌────────────── help ─────────────────────────────┐
│                                                  │
│  Navigation                                      │
│    j  k  ↓  ↑          move row selection         │
│    g g  G              top / bottom               │
│    1  2  3             jump to section            │
│    /                   incremental search         │
│    esc                 close modal / cancel       │
│                                                  │
│  Actions                                         │
│    enter               open modal                 │
│    space               peek / inline reply        │
│    n                   new-agent wizard           │
│    t                   cycle tool filter          │
│    r                   rename                     │
│    a                   archive                    │
│    d d                 delete                     │
│    c                   complete (terminate, done)  │
│    z                   zoom modal fullscreen      │
│    i                   focus λ prompt             │
│                                                  │
│  Modes                                           │
│    :                   command mode               │
│    ?                   this help overlay          │
│    q                   quit (no confirm)          │
│                                                  │
│  Commands                                        │
│    :q  :quit           quit (confirm)             │
│    :q!                 force quit                 │
│    :bg  :detach        detach, save place         │
│    :new                new-agent wizard           │
│    :filter <tool>      set filter chip            │
│    :attach <sel>       open modal                 │
│    :reply <sel> <txt>  reply without opening      │
│    :rm <sel>           delete                     │
│    :rename <sel> <new> rename                     │
│    :archive <sel>      archive a Completed        │
│    :reload             reload tools.yaml          │
│    :help               this overlay               │
│                                                   │
│         esc / ? / :help close — scrolls if tall   │
└──────────────────────────────────────────────────┘
```

The content is generated from the same key/command table that drives the binder — single source of truth, so when a new command lands the help text updates automatically.

`esc`, `?`, or `:help` again closes the overlay. The overlay scrolls (`j`/`k`/wheel) if the terminal is too short to fit it in one screen.

## Accessibility

- All animations gated by `--no-animation` and auto-disabled when stdout is not a TTY.
- Spinner falls back to a static `*` glyph when animation is off.
- Color is never the sole carrier of state — every state has a distinct glyph.
- Mouse is opt-in via terminal capability; keyboard alone is always sufficient.
