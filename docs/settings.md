# Settings reference

User settings live in `~/.config/rex/config.yaml`. Keys are flat top-level YAML entries. Unknown keys are ignored on load. Invalid values for known keys are skipped silently.

Saving is sparse: rex re-reads the file and writes only the keys it changed (`rex config reset` deletes the key), so the TUI and `rex config set` do not overwrite each other. There is no file lock; two saves in the same instant can still lose one.

The TUI settings page (`S`), `rex config`, and the daemon read the same registry defined in code.

## Files

| Path | Role |
|------|------|
| `~/.config/rex/config.yaml` | Persisted values |

## Commands

```sh
rex config list
rex config get <id>...
rex config set <id> <value>...
rex config reset <id>...
rex config edit          # opens init.lua in $EDITOR, not config.yaml
```

## Keys

### Appearance

| ID | Type | Default | Options |
|----|------|---------|---------|
| `color_scheme` | enum | `default` | `default`, `noir`, `paper` |
| `spinner` | enum | `braille` | `braille`, `ascii_line`, `moon`, `pulse`, `blocks` |
| `row_density` | enum | `normal` | `compact`, `normal`, `roomy` |
| `prompt_glyph` | string | `λ` | Suggested graphemes in TUI |
| `reduce_motion` | bool | `false` | |
| `blinking_enabled` | bool | `true` | Completed-row blink |
| `show_help_bar` | bool | `true` | Bottom help line |
| `header_style` | enum | `verbose` | `verbose`, `glyphs`, `numbers` |
| `column_layout` | enum | `fixed` | `fixed`, `fit`. `fixed` gives board columns set widths that change only at width breakpoints (see [tui.md](tui.md#column-widths)); `fit` sizes them to their content |
| `mobile_layout` | enum | `auto` | `auto`, `always`, `off`. Two-line rows, a compact header and a three-key help line. `auto` uses it at 60 columns or fewer; `always` and `off` force it (see [tui.md](tui.md#mobile-layout)) |

### Behavior

| ID | Type | Default | Range | Notes |
|----|------|---------|-------|-------|
| `keybind_preset` | enum | `default` | `default` only | |
| `mouse_enabled` | bool | `true` | | Board and attach-popup mouse: click selects, double-click attaches, wheel moves the selection. Capturing the mouse disables the terminal's native text selection; hold Option (iTerm2/Terminal.app) or Shift (Ghostty/kitty) to select anyway, or turn this off. Applies live |
| `max_concurrent_sessions` | int | `16` | 1–64 | See below |
| `attach_scrollback_lines` | int | `5000` | 0–50000 | Lines the attach popup keeps after they scroll off a primary-screen agent (see [tui.md](tui.md#scrollback)). `0` disables. Also capped at 8 MiB per popup. Read when a popup opens |
| `retire_after_hours` | int | `168` | 1–8760 | How long a session marked done (`cc`, `rex done`) lingers before it is cleared. The TUI and CLI send it with each mark, so changes apply immediately; the daemon reads it at startup as its default |
| `screensaver_enabled` | bool | `true` | | After `screensaver_after_minutes` with no key or click on the board, show a small `∴ rex` with the counts. The key or click that wakes it does nothing else. See [tui.md](tui.md#screensaver) |
| `screensaver_after_minutes` | int | `5` | 1–120 | Idle minutes before the screensaver shows. Only time on the bare board counts: overlays, prompts and the attach popup never time out. A change applies within a minute |
| `skip_permissions` | bool | `false` | | Start Claude Code and Antigravity with `--dangerously-skip-permissions` (the tool's `skip_permissions_args`). Agents then edit files and run commands without asking you first. Only applies in a folder that is under `auto_trust_dirs`; anywhere else the flag is left off and the daemon log says `skip_permissions ignored: folder not trusted`. Off by default; read at each spawn (and each resume), so it applies to the next session. See [SECURITY.md](../SECURITY.md) |
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
| `turn_snapshots` | bool | `false` | Snapshot a session's folder at the start and end of every agent turn into a private store under `~/.local/share/rex/snapshots`, so `rex undo` can restore it. Only folders inside a git repo are captured, and that repo is never touched. The store holds file contents, including untracked secrets such as `.env` unless they are gitignored. Off: no snapshot, no git call, no file written, and `rex undo` / `rex redo` refuse. Read live by the daemon (within about 5 s). See [snapshots](snapshots.md) |
| `waiting_section` | bool | `true` | A Waiting section below Needs you for working sessions blocked on a shell command, subagent or web call for 3 s or more (Claude Code), with its own `◷` marker and count. Off: those sessions stay in Working, the section, its count and its key go away, and `1` `2` `3` (and `z1`-`z3`) are Needs you / Working / Done again. The daemon still records `waiting_on` / `waiting_since`; the switch is read by the TUI and `rex ls` |
| `spawn_skills` | bool | `true` | Load skills from `~/.config/rex/skills` into one session: the wizard's skills step and `rex new -skill`. Off: the step is hidden, `-skill` exits 5 (`off: enable in Settings → Features (rex config set spawn_skills true)`), `NewSession` with skills is `bad_intent` `skills are off`, and no `--plugin-dir` is added, also on resume. The daemon reads it at each spawn and `Resume`. See [howto/skills.md](howto/skills.md) |
| `loops` | bool | `false` | Run an agent session in a repeat loop (`rex loop`): the daemon re-sends your prompt each time the agent finishes a turn, until a check command you wrote exits 0 or a cap is reached. Loop files are read only from `~/.config/rex/loops`, and their check commands run on your machine. A permission or question prompt pauses the loop; rex never answers it. Off: no loop runs, no loop file is read, nothing is written, and `rex loop` exits 5. Read at every request and followed within about 5 s. See [loops](loops.md) |
| `usage` | bool | `true` | Track agent usage: the per-session ledger (`~/.local/share/rex/usage.jsonl`, see [paths.md](paths.md)) behind `rex usage`, and the quota Claude Code reports through its status line. Off: no status-line tap is added to Claude, no ledger is written or trimmed, nothing is polled, and `rex usage` exits 5 (`off: enable in Settings → Features (rex config set usage true)`). The daemon reads it at each spawn and each ledger write |
| `usage_statusline_tap` | `auto` \| `on` \| `off` | `auto` | How rex reads Claude Code's plan usage. rex gives Claude a status line (`rex-daemon statusline-tap`) that sends the usage fields to the daemon and then runs **your own** status line command with the same input, so yours looks unchanged. `auto` adds it only when you already have a user-level status line (see below), because Claude hides its footer hints (`esc to interrupt`, `? for shortcuts`) once any status line is set and rex never changes that on its own. `on` always adds it (the footer hints go away). `off` never adds it; Claude rows then use rex's own token estimate (`~`). Ignored while `usage` is off. Read at each spawn and `Resume`, and within about 5 s while the daemon runs (it follows `config.yaml`); a session already running keeps the status line it was started with |
| `session_export` | bool | `true` | `rex export` and `:export`: write one session as a json, yaml, md or txt document (the transcript replayed to clean text). Off: both refuse (`off: enable in Settings → Features (rex config set session_export true)`, exit 5 on the CLI). Exports can hold whatever the agent printed. See [export.md](export.md) |

### Spawn

| ID | Type | Default | Notes |
|----|------|---------|-------|
| `default_spawn_tool` | string | `claude` | Tool id for `i` quick-spawn |
| `default_spawn_model` | string | `opus` | Model id on that tool |
| `default_spawn_effort` | string | `max` | Ignored if tool has no effort axis |
| `auto_trust_dirs` | string | `""` | Folders (separated by `:`, `;` on Windows; `~/` allowed) whose sessions get the agent's "trust this folder" dialog accepted. Empty: always ask. Set with `rex setup` or `rex config set`; both refuse the values listed below (exit 5) |

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
color_scheme: noir
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
