# Settings reference

User settings live in `~/.config/rex/config.yaml`. Keys are flat top-level YAML entries. Unknown keys are ignored on load. Invalid values for known keys are skipped silently.

Saving is sparse: rex re-reads the file and writes only the keys it changed (`rex config reset` deletes the key), so the TUI and `rex config set` do not overwrite each other. There is no file lock; two saves in the same instant can still lose one.

The TUI settings page (`S`), `rex config`, and the daemon read the same registry defined in code.

## Files

| Path | Role |
|------|------|
| `~/.config/rex/config.yaml` | Persisted values |
| `~/.config/rex/themes/<name>.yaml` | Your own themes ([themes.md](themes.md)) |

## Commands

```sh
rex config list
rex config get <id>...
rex config set <id> <value>...
rex config reset <id>...
rex config edit          # opens init.lua in $EDITOR, not config.yaml
```

## When a change applies

The settings page shows this per row (the detail pane under the list), and the registry carries it as `Apply`:

| Mode | Meaning |
|------|---------|
| applies now | In the TUI at once (also when you edit `config.yaml` by hand: the TUI polls it every 2 s while `live_reload` is on), or through the daemon's settings watcher within seconds |
| next attach | Read when an attach popup opens or resizes (`attach_banner`, `attach_scrollback_lines`) |
| applies now (set from the TUI) | `max_concurrent_sessions`: the TUI sends it to the running daemon; the daemon ignores the YAML |
| next new session | Read at each spawn; running sessions keep their value (`default_spawn_*`, `auto_trust_dirs`, `skip_permissions`, `resume`, `spawn_skills`, `usage_statusline_tap`) |
| after a daemon restart | Read once when the daemon starts (`summary_enabled`, `summary_model`, `lua_config_path`) |

## Themes

`rex config set theme <name>` or `:theme <name>` in the TUI (your own theme files are in [themes.md](themes.md)); the TUI applies it at once and CLI output follows it. `theme` was called `color_scheme`: the old key is still read (`theme` wins if both are in the file), and the next save writes `theme`. An unknown name uses `default`.

| Scheme | For |
|--------|-----|
| `default` | Dark terminals. Violet accent for the prompt arrow, selection marker, focused inputs and logo; blue, amber, green and red stay the state colors |
| `classic` | The previous look (blue accent) |
| `noir` | Deeper, quieter dark |
| `paper` | Light terminals |
| `high-contrast` | Pure black and white with bright state colors |
| `ansi` | Your terminal's own six normal hues (a Solarized or Gruvbox terminal theme gives a matching rex); dim text is the default color drawn faint, since no fixed index is safe (8 is the Solarized Dark background); the selection is reverse video. Also a good fallback on a 16-color terminal |

Every scheme with real colors is tested against its own background: text 7:1, state colors, accent and dim text 4.5:1, focus border 3:1, and the four state colors stay distinct from one another (also after a 256-color terminal quantizes them).

## Border colors

`window_border_color` takes one of ten Pantone-inspired names. The hex values are approximations, not official Pantone values, and rex is not affiliated with Pantone. Each hue sits next to an Okabe-Ito (or IBM color-blind-safe) role, and every value is at least 3:1 against every scheme's background (WCAG non-text contrast); light schemes use the darker variant.

| Name | Reference | Dark / light hex | Okabe-Ito role | `ansi` scheme |
|------|-----------|------------------|----------------|---------------|
| `tangerine` | 17-1463 Tangerine Tango | `#DD4124` | vermillion | 9 |
| `mimosa` | 14-0848 Mimosa | `#F0C05A` / `#B18513` | orange | 3 |
| `illuminating` | 13-0647 Illuminating | `#F5DF4D` / `#A18C00` | yellow | 11 |
| `emerald` | 17-5641 Emerald | `#009473` | bluish green | 2 |
| `cerulean` | 15-4020 Cerulean | `#9BB7D4` / `#758FA9` | sky blue | 14 |
| `classic-blue` | 19-4052 Classic Blue | `#4F83C0` / `#0F4C81` | blue | 12 |
| `very-peri` | 17-3938 Very Peri | `#6667AB` | (IBM indigo) | 13 |
| `orchid` | 18-3224 Radiant Orchid | `#B163A3` | reddish purple | 5 |
| `viva-magenta` | 18-1750 Viva Magenta | `#BE3455` | (IBM magenta) | 1 |
| `ultimate-gray` | 17-5104 Ultimate Gray | `#939597` / `#8A8C8E` | neutral | 8 |

`random` picks from a smaller pool whose colors stay far apart for normal vision and protan, deutan and tritan color vision (OKLab distance, Machado 2009 simulation, tested): seven colors on dark schemes (`tangerine`, `illuminating`, `cerulean`, `very-peri`, `viva-magenta`, `mimosa`, `ultimate-gray`), five on light ones (`mimosa`, `cerulean`, `very-peri`, `viva-magenta`, `classic-blue`). The other colors are only offered by name. The seed is the folder rex was started in (the repository root, inside a repository).

## Keys

### Appearance

| ID | Type | Default | Options |
|----|------|---------|---------|
| `theme` | string | `default` | A theme name: `default` (violet accent), `classic` (the look before the violet accent), `noir`, `paper` (light terminals), `high-contrast`, `ansi` (your terminal's own 16 colors), or a file in `~/.config/rex/themes` ([themes.md](themes.md)). An unknown name uses `default`. Applies to the TUI and to CLI output. Formerly `color_scheme`, still read |
| `spinner` | enum | `braille` | `braille`, `ascii_line`, `moon`, `pulse`, `blocks` |
| `row_density` | enum | `normal` | `compact`, `normal`, `roomy` |
| `prompt_glyph` | string | `λ` | One printable character, 1 or 2 cells wide. The TUI suggests presets and takes any glyph that passes the same check as `rex config set` |
| `reduce_motion` | bool | `false` | |
| `blinking_enabled` | bool | `true` | Completed-row blink |
| `show_help_bar` | bool | `true` | Bottom help line |
| `header_style` | enum | `verbose` | `verbose`, `glyphs`, `numbers` |
| `window_border` | bool | `false` | A thick colored frame around the whole screen (see [tui.md](tui.md#window-border)). It costs 2 columns and 2 rows and gives way on small terminals |
| `window_border_color` | string | `random` | `random` (stable per launch folder: a project keeps its color), a color name (see below), or `#rrggbb` (`#rgb` is expanded). The settings page cycles the names with the arrows and takes a typed hex with enter |
| `column_layout` | enum | `fixed` | `fixed`, `fit`. `fixed` gives board columns set widths that change only at width breakpoints (see [tui.md](tui.md#column-widths)); `fit` sizes them to their content |
| `mobile_layout` | enum | `auto` | `auto`, `always`, `off`. Two-line rows, a compact header and a three-key help line. `auto` uses it at 60 columns or fewer; `always` and `off` force it (see [tui.md](tui.md#mobile-layout)) |

### Behavior

| ID | Type | Default | Range | Notes |
|----|------|---------|-------|-------|
| `keybind_preset` | enum | `default` | `default` only | Hidden on the TUI settings page until a second preset exists; `rex config` still lists it |
| `mouse_enabled` | bool | `true` | | Board and attach-popup mouse: click selects, double-click attaches, wheel moves the selection. Capturing the mouse disables the terminal's native text selection; hold Option (iTerm2/Terminal.app) or Shift (Ghostty/kitty) to select anyway, or turn this off. Applies live |
| `max_concurrent_sessions` | int | `16` | 1–64 | See below |
| `attach_scrollback_lines` | int | `5000` | 0–50000 | Lines the attach popup keeps after they scroll off a primary-screen agent (see [tui.md](tui.md#scrollback)). `0` disables. Also capped at 8 MiB per popup. Read when a popup opens |
| `retire_after_hours` | int | `168` | 1–8760 | How long a session marked done (`cc`, `rex done`) lingers before it is cleared. The TUI and CLI send it with each mark, so changes apply immediately; the daemon reads it at startup as its default |
| `screensaver_enabled` | bool | `true` | | After `screensaver_after_minutes` with no key or click, show a small `∴ rex` with the counts. The key or click that wakes it does nothing else. See [tui.md](tui.md#screensaver) |
| `screensaver_after_minutes` | int | `5` | 1–120 | Idle minutes before the screensaver shows. Overlays, prompts, the wizard and the attach popup time out too; waking returns to them. A change applies within a minute |
| `skip_permissions` | bool | `false` | | Start Claude Code and Antigravity with `--dangerously-skip-permissions` (the tool's `skip_permissions_args`). Agents then edit files and run commands without asking you first. Only applies in a folder that is under `auto_trust_dirs`; anywhere else the flag is left off and the daemon log says `skip_permissions ignored: folder not trusted`. Off by default; read at each spawn (and each resume), so it applies to the next session. A change made through rex (`rex config set` / `reset`, the Settings page, `rex setup`) reaches a running daemon at once; a hand edit of `config.yaml` applies within 5 s, or on `rex reload` with `live_reload` off. See [SECURITY.md](../SECURITY.md) |
| `screensaver_keep_awake` | bool | `true` | | macOS only: keep the display from sleeping while the screensaver shows (`caffeinate -d`, stopped when it goes away or rex exits). Does nothing on other systems. See [tui.md](tui.md#keep-awake-macos) |
| `board_hidden_sections` | string | `""` | | Board sections hidden at startup: comma-separated ids from `needs`, `waiting`, `working`, `done` (for example `done`). Unknown ids are ignored, and a list naming every section is ignored. `zh`, `z1`-`z9` and `:filter` change it for the current run; `:bg` keeps that choice for the next start, which overrides this setting once. Read at startup only. See [tui.md](tui.md#filters) |

`max_concurrent_sessions` in YAML does **not** set the daemon cap at startup. The daemon uses `-max-concurrent-sessions` (default 16). The TUI can apply a new cap live via the settings UI (`SetMaxConcurrent` intent). Restart the daemon to pick up a YAML change for startup behavior.

### Features

One on/off switch per optional feature. A feature that is off is inert: no background work, no file writes, its keys do nothing and its commands refuse with `off: enable in Settings → Features (rex config set <flag> true)`. Existing toggles (`summary_enabled`, `skip_permissions`, …) stay in their own sections. Code reads a switch with `settings.FeatureOn(store, id)`, which panics for an id outside this section.

| ID | Type | Default | Notes |
|----|------|---------|-------|
| `board_filter` | bool | `true` | Tool filter chips, `:filter`, section hiding (`zh`, `z1`-`z9`) and `board_hidden_sections`. Off: no chip row, `t` and the `z` chords do nothing, `:filter` refuses, and hidden sections, `board_hidden_sections` and a saved tool filter are ignored. See [tui.md](tui.md#filter-and-search) |
| `search` | bool | `true` | Board search: `/` and `:search`. Off: `/` does nothing, `:search` refuses, and a saved or stale query is ignored |
| `undo_complete` | bool | `true` | The `uc` key and `:uc` / `:resume`, which take a session out of Done. Off: `uc` does nothing and the commands refuse; `:undone` (unmark only) still works |
| `resume` | bool | `true` | Track each session's agent conversation id (resume id) so an exited session can be resumed: the resume id column, `:rid` and the `Resume` intent. Off: no `--session-id` is preassigned, no id is captured from hooks or `capture_regex`, the column and `:rid` go away, and `Resume` is refused with `bad_intent` `resume is off`. The daemon reads it at each spawn and `Resume`; the TUI at each frame. Ids already stored stay in `meta.json` |
| `timestamps` | bool | `true` | Start and end times: the start→end span column in the detailed board view and `STARTED` / `ENDED` in `rex ls -wide`. Off: no column and no such columns. The end time (`ended_at`) is still recorded. Read at each frame and each `rex ls` |
| `cli_agent_auto` | bool | `true` | Switch rex's CLI to agent mode (one line per record, no color, no prompts) when `CLAUDECODE=1` and stdout is not a terminal. Off: only `--agent`, `-o agent` and `REX_AGENT=1` do. `REX_AGENT=0` turns it off for one run. See [cli.md](cli.md#agent-mode) |
| `summary_preview` | bool | `true` | A line under the board with the selected session's full AI summary and its age. Shown only while some session has a summary. Off: no line and no row reserved |
| `summary_schedule` | bool | `true` | Scheduled description refresh. Each running session (queued, working or needs you) gets a new AI description 5, 10, 15, 20 and 25 minutes after its run started, then every 30 minutes. From the 6th refresh on the description is one `summary_detail` level longer (short→medium, medium→long, long stays at 180 characters) and says what is finished so far plus what is happening now. A state change still refreshes at once and does not move the clock; a Resume restarts the 5-minute phase. Screen activity no longer triggers a refresh, and an unchanged screen makes no call. Applies within about 5 s with `live_reload` on (otherwise on `rex reload`), no daemon restart. Needs `summary_enabled`. A session with no description yet still refreshes on screen activity until its first description lands. Each session's clock is shifted by up to 5% of 5 minutes so sessions started together do not tick together. Off: refresh when the screen goes quiet and on state changes, as before; no timer |
| `turn_snapshots` | bool | `false` | Snapshot a session's folder at the start and end of every agent turn into a private store under `~/.local/share/rex/snapshots`, so `rex undo` can restore it. Only folders inside a git repo are captured, and that repo is never touched. The store holds file contents, including untracked secrets such as `.env` unless they are gitignored. Off: no snapshot, no git call, no file written, and `rex undo` / `rex redo` refuse. Read live by the daemon (within about 5 s). See [snapshots](snapshots.md) |
| `waiting_section` | bool | `true` | A Waiting section below Ready for working sessions blocked on a shell command, subagent or web call for 3 s or more (Claude Code), with its own `◷` marker and count. Off: those sessions stay in Working, the section, its count and its key go away, and `1` `2` `3` (and `z1`-`z3`) are Ready / Working / Done again. The daemon still records `waiting_on` / `waiting_since`; the switch is read by the TUI and `rex ls` |
| `spawn_skills` | bool | `true` | Load skills from `~/.config/rex/skills` into one session: the wizard's skills step and `rex new -skill`. Off: the step is hidden, `-skill` exits 5 (`off: enable in Settings → Features (rex config set spawn_skills true)`), `NewSession` with skills is `bad_intent` `skills are off`, and no `--plugin-dir` is added, also on resume. The daemon reads it at each spawn and `Resume`. See [howto/skills.md](howto/skills.md) |
| `loops` | bool | `false` | Run an agent session in a repeat loop (`rex loop`): the daemon re-sends your prompt each time the agent finishes a turn, until a check command you wrote exits 0 or a cap is reached. Loop files are read only from `~/.config/rex/loops`, and their check commands run on your machine. A permission or question prompt pauses the loop; rex never answers it. Off: no loop runs, no loop file is read, nothing is written, and `rex loop` exits 5. Read at every request and followed within about 5 s. See [loops](loops.md) |
| `usage` | bool | `true` | Track agent usage: the per-session ledger (`~/.local/share/rex/usage.jsonl`, see [paths.md](paths.md)) behind `rex usage`, and the quota Claude Code reports through its status line. Off: no status-line tap is added to Claude, no ledger is written or trimmed, nothing is polled, and `rex usage` exits 5 (`off: enable in Settings → Features (rex config set usage true)`). The daemon reads it at each spawn and each ledger write |
| `usage_statusline_tap` | `auto` \| `on` \| `off` | `auto` | How rex reads Claude Code's plan usage. rex gives Claude a status line (`rex-daemon statusline-tap`) that sends the usage fields to the daemon and then runs **your own** status line command with the same input, so yours looks unchanged. `auto` adds it only when you already have a user-level status line (see below), because Claude hides its footer hints (`esc to interrupt`, `? for shortcuts`) once any status line is set and rex never changes that on its own. `on` always adds it (the footer hints go away). `off` never adds it; Claude rows then use rex's own token estimate (`~`). Ignored while `usage` is off. Read at each spawn and `Resume`, and within about 5 s while the daemon runs (it follows `config.yaml`); a session already running keeps the status line it was started with |
| `usage_meter` | `off` \| `compact` \| `full` | `compact` | The right end of the header's top row: for each agent with a live session, its worst quota window as a 5-cell bar and a percent (`compact`), or every window with its reset time (`full`). It gives way to the counts when the row is short, and a click on it opens `:usage` ([tui.md](tui.md#usage)). Ignored while `usage` is off, and hidden against a daemon that sends no quotas. Live |
| `extra_agents` | bool | `true` | Qwen Code (`qwen`), Cursor CLI (`cursor`, binary `cursor-agent`) and Grok Build (`grok`). Their default modes can edit files or run commands without asking, and rex does not answer their folder-trust dialogs (see [registry.md](registry.md#built-in-tools)). For now the three are hidden in the wizard until enabled in Settings → Onboarding; the wizard does not yet read this switch or `PATH`. Off: the three are gone from the daemon's registry and a spawn is refused with `bad_intent` `off: enable in Settings → Features (`rex config set extra_agents true`)`. The daemon follows `config.yaml` within about 5 s |
| `model_autodetect` | bool | `true` | Ask each installed agent for its own model list (`qwen --list-models`, `grok models`, `cursor-agent models`) and cache it in `~/.local/state/rex/models-cache.json` for 24 h ([paths.md](paths.md)). Detected models show in the wizard and in the quick-spawn model choices. Refreshed at daemon start, by `rex reload`, and when the switch is turned on. Off: no command is run, the cache is neither read nor written, and only the built-in model lists show. See [registry.md](registry.md#models_cmd) |
| `live_reload` | bool | `true` | The daemon polls `config.yaml` every 5 s and applies the settings that can change live (see [below](#which-settings-apply-live)). Off: no polling and no file is stat'ed; `rex reload`, `:reload` and `SIGHUP` still reload by hand. Turning it off takes effect at the next poll; turning it back on needs one manual reload |
| `session_export` | bool | `true` | `rex export` and `:export`: write one session as a json, yaml, md or txt document (the transcript replayed to clean text). Off: both refuse (`off: enable in Settings → Features (rex config set session_export true)`, exit 5 on the CLI). Exports can hold whatever the agent printed. See [export.md](export.md) |
| `workspaces` | bool | `false` | Workspaces: `rex ws init` marks a project folder with a `.rex/` holding an id, a name and a color; `rex audit --ws` and workspace audit files use it. Off: rex never looks for or writes a `.rex/` folder, `rex ws` exits 5 and the daemon does no workspace lookup. See [workspaces.md](workspaces.md) |
| `audit_trail` | bool | `true` | Write one line per finished session (who, tool, state, times, first line of the prompt) to `~/.local/share/rex/audit.jsonl`, or `<workspace>/.rex/audit.jsonl` for a workspace session, for `rex audit`. Off: no file is written, the prompt is not kept in memory and `rex audit` exits 5. The daemon follows `config.yaml` within about 5 s |
| `audit_full_prompt` | bool | `false` | Also store each session's whole first prompt (20,000 characters at most) in the audit trail. Prompts can hold secrets and a workspace `audit.jsonl` may be synced or committed. Needs `audit_trail`. Applies to sessions started after it is turned on |

### Spawn

| ID | Type | Default | Notes |
|----|------|---------|-------|
| `default_spawn_tool` | string | `claude` | Tool id for `i` quick-spawn |
| `default_spawn_model` | string | `opus` | Model id on that tool |
| `default_spawn_effort` | string | `max` | Ignored if tool has no effort axis |
| `auto_trust_dirs` | string | `""` | Folders (separated by `:`, `;` on Windows; `~/` allowed) whose sessions get the agent's "trust this folder" dialog accepted. Empty: always ask. Set with `rex setup` or `rex config set`; both refuse the values listed below (exit 5). A change made through rex reaches a running daemon at once; a hand edit of `config.yaml` applies within 5 s, or on `rex reload` with `live_reload` off |

In the settings page (`S`) these three cycle with `+`/`-` through the tool registry: tool → that tool's models → that model's effort options. Changing the tool resets model and effort; changing the model resets an effort it doesn't offer. The daemon rejects an effort the model doesn't list.

### Auto-trust

Agents ask whether you trust a folder the first time they run in it. For a session started in a folder listed in `auto_trust_dirs`, or anywhere below one, rex handles that question:

- **Antigravity CLI (`agy`):** rex starts it with the folder in its session-only `trustedWorkspaces` (never saved), so the question never shows.
- **Claude Code:** it asks "Is this a project you created or one you trust?" with **No, exit** selected. The daemon answers it: it moves the selection to **Yes, I trust this folder**, checks that it is selected, and confirms. It acts only within the first 60 s of the session and gives up after 4 moves. Anywhere else the dialog waits for you as before.

```sh
rex config set auto_trust_dirs "~/Documents/dev"
```

`rex setup` asks the same question once, with **no** as the default answer. When you run it from the root of a git checkout, it suggests the folder that holds that checkout, if that folder is inside your home folder (never the home folder itself, and not through a link that leads out of it). You can accept the folder, edit it, or say no. Without the wizard, `rex setup -yes -auto-trust "~/Documents/dev"` sets it; if `config.yaml` already exists, add `-force` (your other settings are kept) or use `rex config set`. Setup refuses the values listed below with a message, instead of writing a setting the daemon would ignore.

The setting is read at each spawn; no restart is needed. These are ignored, because they would trust far more than your projects:

- a filesystem root (`/`) or your home folder itself (`~`, `~/`);
- relative entries, and any path containing `..`;
- the whole setting, if splitting it on `:` (`;` on Windows) gives a piece that is not an absolute or `~/` path. A folder name that contains `:` would otherwise split into a broader folder.

Symlinks are resolved on both sides, so a link inside a trusted folder that points elsewhere does not count. If your home folder is set but cannot be resolved (it does not exist), no folder is trusted.

**Only list folders whose code you trust.** Accepting the dialog lets the repository's own agent settings apply without that prompt (for Claude Code, allow rules in its `.claude/settings.json`). A repository you cloned from someone else belongs outside these folders. Which agents have a dialog to answer is set by `trust_prompt` in the tool registry ([registry.md](registry.md#trust_prompt)).

### AI summary

| ID | Type | Default | Options |
|----|------|---------|---------|
| `summary_enabled` | bool | `true` | |
| `summary_model` | string | `gemma2:2b` | Use a small model: `gemma2:2b`, `llama3.2:3b`, `llama3.2:1b`, `gemma3:1b`, `qwen2.5:3b`, `phi3:mini` |
| `summary_detail` | enum | `medium` | Applies within about 5 s, no daemon restart. `short` (about 8 words, 60 characters), `medium` (about 15 words, 110), `long` (about 28 words, 180). Each step up costs roughly 0.5-1.5 s more per call on a small local model |
| `desc_animation` | enum | `typewriter` | `typewriter`, `decode`, `wipe`, `off` |
| `overview_enabled` | bool | `false` | Applies within about 5 s, no daemon restart. Needs `summary_enabled`. The daemon asks the summary model for a one-line headline over the whole board (at most one call every 30 s, only while a rex board is open and something needs you, is ready for review, failed or is running). Off: no call, no goroutine |
| `attach_banner` | enum | `marquee` | `marquee`, `static`, `off`. One row above the agent in the attach popup (what it is doing, where, for how long). Read when a popup opens and on every resize; the agent's terminal is one row shorter while it shows. Hidden below 25 terminal rows. No Ollama call: built from fields the board already has |

Requires a running Ollama instance when enabled. If `summary_model` is not pulled, the daemon substitutes the first pulled model from that small-model list; it never falls back to an arbitrary (large) model. The board shows the session's task (`title`) until the first summary arrives. See `OLLAMA_HOST` in [paths.md](paths.md). If the configured model is not pulled, the substitute is used and logged; run `ollama pull <model>` to use the configured one. Descriptions are written by the model from what the agent printed: they can be wrong, and an `OLLAMA_HOST` that points off this machine receives that screen text. Descriptions start with a verb ("Editing api/webhooks.go"), never "Claude is…", and a finished session gets one final summary ("Done: …").

### Audio

| ID | Type | Default | Options |
|----|------|---------|---------|
| `sound_enabled` | bool | `true` | |
| `soundset` | enum | `factorio` | `factorio`, `evangelion`, `bell`, `chiptune`, `off` |
| `master_volume` | float | `0.80` | `0.0`–`1.0` |
| `notify_on_state_change` | bool | `true` | |
| `boot_voice` | bool | `true` | |

`boot_voice` plays a short spoken "rex" in the lifeOS boot voice (macOS Ralph, flat ~94 Hz robotic monotone, no effects; original audio, generated by `internal/features/audio/voice/gen.sh`) right after the startup tone when the board appears. Independent of `soundset`; requires `sound_enabled` and scales with `master_volume`.

`notify_on_state_change` plays a chime when a session goes `working` → `needs_input` (the agent is waiting on you) or `needs_input` → `working` (it picked back up, e.g. after you answered). Each soundset has its own tone for both. A transition into `done`, including `needs_input` → `done`, plays only the done sound, and so does marking a session done (`cc`, `rex done`); that sound ignores this setting. Rules:

- At most one state-change chime per session every 1.5 s, so a heuristic adapter flickering between `needs_input` and `working` chimes once.
- No sound for states delivered in the initial snapshot or on reconnect, or for a session the TUI hasn't seen yet. Only live transitions chime.
- `sound_enabled`, `soundset: off`, and `master_volume` still apply.
- Changes take effect immediately from the settings page.

### Advanced

| ID | Type | Default | Notes |
|----|------|---------|-------|
| `lua_config_path` | string | `~/.config/rex/init.lua` | Read-only in TUI; path for Lua hooks |
| `cli_error_log` | bool | `true` | Keep a private record of failed `rex` commands in `~/.local/state/rex/cli-errors.log`: structure only (command, flag names, exit code and kind, error type names), never any message, prompt, value or path. 1 MiB plus one backup, 0600. Read with `rex errors`. `REX_ERROR_LOG=0` also turns it off |

## Example

```yaml
theme: noir
sound_enabled: false
default_spawn_tool: echo
default_spawn_model: short
```

## See also

- [howto/customize-appearance.md](howto/customize-appearance.md)
- [howto/lua-hooks.md](howto/lua-hooks.md)
- [registry.md](registry.md) — tool ids for spawn defaults
- [paths.md](paths.md)

### How the Claude status-line tap finds your status line

The **daemon** works out the path once, when it starts a session, from its own environment: `$CLAUDE_CONFIG_DIR/settings.json` when that variable is set to an absolute folder, else `<home>/.claude/settings.json`. It bakes the absolute path into the tap's command (`--user-settings <path>`) and the tap reads only that file, read-only. The tap ignores its own `CLAUDE_CONFIG_DIR` and `HOME`: Claude applies the `env` block of settings files, project ones included, to the processes it starts, so an agent that writes a project settings file could otherwise point the tap at a file of its own. A missing or relative path, a symlink, or anything that is not a regular file means no status line (the tap runs nothing and exits 0). A symlinked `~/.claude/settings.json` (a dotfiles manager, say) is therefore not followed: with `auto` rex then adds no tap, and with `on` your status line is not passed through. The file's `statusLine` is used when it has `"type": "command"` and a non-empty command.

Project files (`<folder>/.claude/settings.json` and `settings.local.json`) are never read, and no folder named in Claude's status-line JSON is used. The tap runs your command on every status-line render, and an agent can write a project settings file from inside its own folder at any moment, while Claude itself reads those files only at startup and only for a trusted folder. Passing through a project-level command would let an agent run a command of its choosing outside Claude's permission system. Consequences:

- A status line that exists only in a project file does not count as "you have a status line": `auto` does not add the tap because of it.
- With `on`, rex's status line replaces a project-level one for that session (rex's `--settings` take precedence), and only the user-level command is passed through.

Not supported: enterprise managed settings, and a `--settings` file you pass to `claude` yourself (rex starts Claude). A missing or invalid file means no status line. A command that already runs `statusline-tap` is ignored. rex never writes any Claude settings file: the tap is injected through the `--settings` argument rex already passes, which also holds the hooks. At run time the tap runs your command through `sh -c` (`bash -c` on Windows) with the identical stdin bytes and passes its stdout, stderr and exit code through. With no user-level status line it prints nothing and exits 0.

## Which settings apply live

The daemon keeps one copy of `config.yaml` and refreshes it about 5 s after an edit (or at once on `rex reload`, `:reload` or `SIGHUP`). With `live_reload` off it refreshes only on those.

The TUI does the same on its own: every 2 s it checks `config.yaml` and the theme files, and when they changed it swaps in the new values and applies the settings marked "applies now" that differ (theme, audio, mouse, hidden sections, `max_concurrent_sessions` and the display settings). `:reload` does it at once, with `live_reload` off too. A `config.yaml` that fails to parse leaves the TUI's values in place and says so on the status line. With `live_reload` off the TUI starts no timer and reads no file.

| Applies | Settings |
|---------|----------|
| To the running feature | `summary_detail`, `summary_schedule`, `overview_enabled`, `turn_snapshots`, `usage` |
| At the next spawn or `Resume` | `skip_permissions`, `auto_trust_dirs`, `resume`, `spawn_skills`, `usage_statusline_tap` |
| At daemon start only | `summary_enabled`, `summary_model`, `retire_after_hours`, `lua_config_path` |

A `config.yaml` that fails to parse is ignored until it is fixed: the previous values stay in force, except `skip_permissions` and `auto_trust_dirs`, which fail closed (off, empty) until the file parses again. The daemon log says so once per failed reload.
