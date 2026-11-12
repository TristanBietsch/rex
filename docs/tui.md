# TUI reference

The terminal UI starts when you run `rex` with no subcommand. It connects to the daemon, shows a kanban board, and accepts keyboard input.

## NAME

rex — terminal board

## SYNOPSIS

```sh
rex
```

## DESCRIPTION

Four sections group sessions, top to bottom: **READY**, **WAITING**, **WORKING**, and **DONE** (includes `done`, `failed`, `crashed`, and every session marked done, whatever its live state). Each title shows its count. The header line reads `N need you · N waiting · N working · N done` (the waiting count is left out while it is 0).

**READY** holds sessions that wait on you. A Claude Code session is READY only when it asks you something (a permission prompt, an MCP form or link, a usage-limit wait that needs Enter), or when its turn ended (Stop, or an API error such as a rate limit) with no subagent still running and no subagent result still to come. Notifications that need nobody (sign-in done, a form answered, a background session finished, a mod notice, and any type newer than rex) change nothing, and neither does an auto-compaction in the middle of a turn. While subagents run after the main turn ended, the session stays in WAITING (`waiting_on: subagent`); each subagent that finishes wakes the main agent with its result, and only its Stop after the last one makes the session READY, once. A background subagent that finished while the agent was still busy keeps the session in WAITING after that turn's Stop until its result arrives, at most 10 seconds. A subagent whose stop report was lost stops counting a minute after its last tool call once the turn has ended (Claude lists the background tasks still running at the end of a turn), or after 15 minutes without a tool call. A turn you interrupt with Esc or Ctrl+C sends no hook at all; rex sees the key you type into the session (attach popup, `rex attach`, `rex send`) and turns the session READY 5 seconds later, plus the 2-second hold, unless the agent's main thread reports anything in between (the key closed a menu instead). Foreground subagents (Task calls, also several in parallel) interrupted with it no longer count; a background subagent still running keeps the session in WAITING until it ends. Without the key (a turn that ended some other way with no Stop) the fallback is 30 seconds of a still screen, 15 minutes when a tool call is still open, never while a subagent runs. Any move from WORKING into READY must hold for 2 seconds before rex shows it, except a question, which shows at once. Other agents (Codex, Qwen, Cursor, Grok, Antigravity) have no subagent reports: they are READY when their prompt shows or after 15 seconds of a still screen, with the same 2-second hold.

**WAITING** holds working sessions that are blocked on a tool, not on you: a shell command, a subagent or a web fetch or search (Claude Code reports it through its tool hooks). A session moves there once the tool call has run for 3 seconds, so quick calls never make a row jump, and back to WORKING when the call ends. A session that needs your input is never in WAITING, and neither is one you marked done. Waiting rows play no sound and send no notification. Its marker is `◷`, which turns `◴ ◷ ◶ ◵` once a second (still `◷` with `reduce_motion`). `waiting_spinner` (Settings → Appearance, Waiting icon) picks another set: `hourglass` `⧖ ⧑ ⧖ ⧒` (an hourglass turning over), `sand` `⣀ ⣄ ⣤ ⣦ ⣶ ⣷ ⣿`, `orbit` `◜ ◝ ◞ ◟`, `pendulum` `⠂ ⡀ ⢀ ⠐ ⢀ ⡀` or `pulse` `∘ ◦ ⊚ ◦`; the row, the attach chip, the header counts and the legend all follow it. Other agents have no tool reports yet, so their sessions stay in WORKING. Known gaps: an Esc typed outside rex (a Claude session you reach some other way) is not seen, so that turn takes the 15-minute fallback when a tool call was open; an Esc that does not interrupt (it closes a menu) during a long command or subagent with no other hook in 5 seconds shows READY early, and the session turns WORKING again at the next hook, silently. A foreground subagent whose tool call was already in flight when you pressed the key counts as alive, so the session stays in WAITING until Claude reports it stopped or 15 minutes pass. An Esc while the main turn has already ended (the session waits on background subagents) changes nothing. A background shell command (`run_in_background`) or monitor has no completion hook, so a session whose turn ends while one runs shows as READY (rex does not wait on it: a server or a log tail never ends); when the command finishes Claude wakes the agent, which turns WORKING and then READY again. A subagent result that arrives more than 10 seconds after its subagent ended does the same. After you answer a permission prompt, the session stays READY until the tool's next hook (the call ends, or the subagent calls its next tool): a "no" ends the turn with no hook at all, so rex cannot tell it from a long command you allowed. A question from a subagent counts as answered when that subagent works again; when Claude does not say which subagent asks, any agent's next tool call counts. A scheduled prompt (`/loop`, a cron), a message from another session or a subagent's late result starts a new turn like one you typed: READY again after it, with its sound. A command that first needed a permission prompt shows as READY while you decide and, as above, until its call ends. The Done key shown in the fold hint (`4`) is `3` while the section is off. `waiting_section` in Settings → Features turns the section off (they then show in WORKING). The bottom prompt (`λ` by default) accepts quick-spawn text, and the help bar lists five keys: `enter open · i new · cc done · tab next · ? more`. Everything else is under `?`. `show_help_bar` in Settings → Appearance turns these hints off; errors and notices still show on that line.

The filter bar (`all · claude · codex …`, cycled with `t`) has a chip per agent the daemon reports installed, in registry order (not `echo`, not an opt-in built-in such as `gemini`; a `tools.yaml` tool counts; `qwen`, `cursor` and `grok` only while `extra_agents` is on). Against a daemon that sends no tool list it shows `claude`, `codex`, `antigravity` and `ollama`. It only shows while sessions use more than one tool, a filter other than `all` is active, a section is hidden, or a search is active. Hidden sections are named after the chips (`done hidden (zh)`, or `needs, done hidden (z0 shows all)`), and a search reads `/auth · 3 matches · esc clear`. See FILTER AND SEARCH.

A DONE column with more than 5 rows folds into its title line (`DONE  7 hidden`), and its keys join the end of the help line (`… ? more · 4 show · :prune clear`). `4` shows and hides it; `:prune` clears it ([slash.md](slash.md)). On a narrow line `:prune clear` drops first, then `4 show`; the five board keys always stay. When a row folds away (for example you mark the selected session done), the selection moves to the first row shown.

The daemon auto-starts if the socket is unreachable (boot splash).

## ROW COLUMNS

By default (`row_icons` on) a row shows state, name, a strip of fixed icon slots, and age. No row text: the preview line under the board carries it. The strip is right-aligned: it ends next to the age, and the name takes the room on the left.

```
   ◆  dark-mode                 ✦  ? asks                     $0.31           4m
   ◷  api-research                 sub 3m              ⋔ 2    $1.07           2m
 ▸ ⠋  payment-migration-v2                   ⟳ 3/8     ⋔ 3    $4.92  ▴ 45m    1m
     state name                 unseen why     loop  subagents spend long-run  last event
```

The selected row has no background bar: the `▸` arrow marks it, and a marked-done row's name stays at full weight while selected.

The strip's slots, left to right, ending before the age. A slot that exists is always reserved, blank on rows with nothing to show, so nothing shifts when a value appears:

| Slot | Width | Shows |
|------|-------|-------|
| unseen | 1 | `✦`: a Ready or Done row that finished since you last looked at it (last attach or `cc`). On the first start every row counts as seen. Marked-done rows never show it |
| why | 8 | Ready: `? asks` (the agent asks for an answer or approval, or its last line is a question); a row whose turn simply finished shows nothing. Waiting: what it waits on and for how long (`sh 42s`, `web 9s`, `sub 3m`). Blank otherwise |
| loop | 8 | `⟳ 3/8` while the loop runs; `⟳ paused`, `⟳ capped`, `⟳ done`, `⟳ failed`, `⟳ stop` otherwise. Only while `loops` is on |
| subagents | 4 | `⋔ 3`: subagents the session runs now (Claude Code). Counts above 99 show as 99 |
| spend | 6 | What the session has cost so far, right-aligned: `$0.42`, `$12.3`, `$123`, `$1.2k`. Warmer from $5, red from $20. Only while `usage` is on, and only for sessions the daemon sends a `cost_usd` for (Claude Code); blank otherwise, never `$0.00` |
| long-run | 5 | The long-running badge (`▴ 45m`, see [LONG-RUNNING BADGE](#long-running-badge)) |

`row_icon_set` (Settings → Appearance) removes slots: a comma-separated list of `unseen`, `reason`, `loop`, `subagents`, `spend`, `longrun` (unknown ids are ignored; the order is fixed). `ascii_glyphs` swaps the strip's glyphs for ASCII: `✦→*`, `⟳→@`, `⋔→Y`, `▴→^`; `?` and `$` stay. Every glyph has text next to it, so no meaning is color-only, and nothing in the strip animates.

Under the board, a dim preview line (`↳ …`) shows the selected session's lead, first that exists: the question it waits on, its AI summary (with its age, `· 12s`), or `task: ` and the task you gave it. It takes one row (two on a terminal 30+ rows tall with `summary_detail: long`, none below 20 rows), reserved for the whole board so the layout does not move as the selection does. `summary_preview` in Settings → Features turns it off. For Antigravity sessions the board's last line skips agy's `Generating...` spinner and the text in its input box.

`v` switches to the detailed view, which adds the short id before the name and model · effort, tokens, the start→end span and the resume id between the name and the strip, and adds `· N marked done` to the header:

```
 ▸ ⠋  c4d1  payment-migration-v2                      opus · max         310K tk               ⋔ 3    $4.92  ▴ 45m    1m
     state  id  name                                  model · effort     tokens   strip                            last event
```

- **span** is when the session started and ended (`14:02→15:30`; `14:02→` while it is live; `Jan 2` for an earlier day, the year for an earlier year). The end is when the row entered Done: the earlier of the time you marked it done and the time it reached done, failed or crashed. The simple view has no timestamps. The `timestamps` feature (Settings → Features, on by default) switches the span column off in both the fixed and the fit layouts; the end time is still recorded.
- **resume id** is the first 8 characters of the agent's conversation id (`1a2b3c4d`), the id `claude --resume` takes. It only appears in the detailed view on a wide terminal (150+ columns). Rows in Done show it in full colour, live rows dim. `:rid [sel]` puts the full id on the status line so you can select it with Option/Shift-drag. The `resume` feature (Settings → Features) switches the column and `:rid` off.
- **name**, **model · effort**, and **tokens** have fixed widths (see below). Tokens are right-aligned.
- **last event** is right-aligned against the edge; `-` means no event yet.
- The name's cell takes the remaining width (its class width plus whatever the strip and the detail columns leave), so a long name is cut only when there is no room. Rows always fill the terminal width exactly and never wrap.

### Without the strip (`row_icons` off)

`row_icons` in Settings → Features turns the strip off and brings back the earlier board, a row with a text column:

```
 ▸ ◆  release-notes   Which feature should lead: search or export?          11m
     state name       text                                                  last event
```

The **text** is the task you gave the session. A session that needs you shows the last line of its screen instead when that line is a question (ends in `?`); otherwise that line is usually the agent's own status text. A session started without a task shows its AI summary. When the text does not fit, it is cut on a word boundary and ends in `…`. The detailed view shows the AI summary as the text, and a row in WAITING starts it with the reason and how long it has waited (`shell 42s · Running the test suite`). With `loops` on, a loop's row starts its text with the loop chip in both views. The subagent and long-run columns sit before the age. The preview line shows the selected session's full AI summary, and in the detailed view only when the row cuts it. Nothing is tracked for the unseen mark and no `seen` key is written to `tui-state.json`. The attach header chip reads `◷ waiting (shell)` either way (the glyph follows `waiting_spinner`).

### Column widths

Column widths never depend on what the rows contain: a long name, a first subagent or a cost moves nothing. They change only when the terminal crosses a breakpoint, and each breakpoint drops whole columns:

| Terminal width | Simple view | Detailed view (`v`) adds |
|----------------|-------------|--------------------------|
| 150+ (wide) | name 32+ · strip · age | id · name 24 · model 24 · tokens · span · resume id |
| 110–149 (normal) | name 24+ · strip · age | id · name 18 · model 16 · tokens |
| 61–109 (narrow) | name 20+ · strip · age | nothing |
| 60 or fewer | the mobile layout, see below | |

The name widths are minimums: the name also gets the blank room left of the strip, which sits flush against the age. The strip is 42 columns with its gaps (32 with `loops` off: the loop slot and its gap go). A slot or column that a class shows is always reserved, even when every row's cell is empty. With the `resume` feature off the resume column is gone.

Below a class's minimum (`mobile_layout: off` on a small terminal) the row squeezes in this order: the spacer goes to 0; spend, long-run, loop and subagents go, in that order; the why slot shrinks to its glyph (`?`; a waiting row is blank there, since its waiting marker (`◷` by default) already says it waits); the name shrinks to 12; the unseen slot goes last.

`column_layout` in Settings → Appearance switches the default `fixed` for `fit`, which sizes name, model and tokens to their widest value on the board, and shows a slot or the span, tokens and resume columns only while some row has a value for it. It gives way on narrow terminals in the order above.

With `row_icons` off the widths are those of the text-column board: name 22 / 20 / 16 (wide / normal / narrow) with the subagent and badge columns, and `fit` gives way in this order until the description has 20 columns: the resume id is hidden, the span is hidden, tokens are hidden, model shrinks (down to 12), name shrinks (down to 12), model is hidden, subagent count is hidden, long-run badge is hidden. After that the description shrinks, then the name.

### Mobile layout

At 60 columns or fewer (a phone over SSH) the board stacks each session on two lines. `mobile_layout` in Settings → Appearance sets when: `auto` (the default) uses it at 60 columns or fewer, `always` forces it, `off` never uses it (the narrow columns then squeeze to fit, as above).

```
 ▸ ◆  release-notes   ✦ ?  ⋔ 2  ▴ 42m  11m
      draft the 0.9 release notes
```

- Line 1 is the state, name, the unseen mark and the why glyph (`?` only, each one cell; with `row_icons` on), subagent count, long-run badge and age. The subagent and badge cells go below 36 columns. Loop and spend are not shown.
- Line 2 is the text (simple view) or the AI summary (detailed view), cut on a word boundary. Clicking either line selects the session, and scrolling keeps both lines on screen.
- The header shows the short counts (`◆ 4  ⟳ 2  ● 7`) whatever `header_style` says, without `· detailed (v)`. The backend banner is shortened.
- The help line lists three keys: `enter open · i new · ? help`.
- There is no summary preview line, since line 2 already shows the text.

## FOCUS MODES

| Mode | Enter | Exit |
|------|-------|------|
| Board | default | — |
| Prompt | `i` | `esc` |
| Command | `:` (board) or `::` (inside attach) | `esc` after command |
| Rename | `r` | `enter` saves, `esc` cancels |
| Search | `/` | `enter` keeps the query, `esc` restores the previous one |
| Attach | `enter` on selected row | `qq`, `ctrl+]` or `::q` |
| New-agent page | `n` or `:new` | `esc` (keeps the draft), or a launch |
| Wizard | `n` or `:new` with `new_agent_page` off | wizard flow |
| Settings | `S` or `:settings` | `esc`, `q` (page only) |
| Help | `?` or `:help` | `esc`, `?`, `q` (page only) |
| Stats | `:stats` | `esc`, `:` |
| Overview | `:overview` | `esc`, `q`, `:overview` |
| Loops | `L` or `:loops` (needs `loops`) | `esc`, `q`, `L`, `:loops` |
| Confirm stop loop | `x` or `:loop stop` | `y` / any other key |
| Usage | `:usage`, or a click on the header meter | `esc`, `q`, `:` |
| Undo / redo dialog | `:undo`, `:redo` | `esc`, `n`, or `esc` after the result |
| Confirm quit | `:q` | `y` / `n` |
| Confirm delete | `dd` | `y` / `n` |

### Page look

Settings and help open as full-screen pages that look like the board: the same blank top row and `∴ REX` logo (`∴ REX › SETTINGS`, `∴ REX › HELP`), a divider, the body, a divider and a hint row. The board is not drawn behind them, and the window border frames them as it frames the board. The hint row follows `show_help_bar`; errors, such as a refused edit, show there even when it is off.

- **Panes.** The body has two panes: a list of sections or groups on the left (at most 24 columns wide) and the rows on the right. `h` focuses the left pane and `l` (or `enter`) the rows; `tab` / `shift+tab` switch too. A page opens with the rows focused.
- **Moving (vim-style, the same on every page).** Settings, help and the new-agent page share one list navigator:

  | Key | Does |
  |---|---|
  | `j` `k`, `↓` `↑` | one row; on the rows pane it goes on into the next or previous section (the left pane follows); clamps at the ends, never wraps |
  | `h` `l` | left pane / rows pane |
  | `gg` / `G` (`home` / `end`) | first / last row |
  | `ctrl+d` / `ctrl+u` | half a window down / up |
  | `ctrl+f` / `ctrl+b` (`pgdn` / `pgup`) | a whole window down / up |
  | `[` / `]` | previous / next section (clamps); each section keeps the row you left it on |
  | `1`–`9` | section N |
  | mouse wheel | one row, as `j` / `k`, on the pane under the pointer |

  A list scrolls one row at a time and keeps 2 rows of context above and below the cursor; the `↑ more` / `↓ more` markers take rows of their own, so they never hide a row. Counts (`5j`) are not supported: the digits jump to a section.
- **Narrow terminals.** Under 60 columns the left pane folds into a one-line strip under the header (`‹ Appearance ›`); `j` `k` move rows, `h` / `l` and `[` / `]` change the section, `1`–`9` jump, and `tab` does nothing.
- **Close.** `esc` (it clears a search or filter first) or `q`; `?` also closes help.
- **While attached.** `::settings` and `::help` detach first, then open the page; a page is never drawn over a running agent.

`fullscreen_pages` (Features, on by default) turns the pages off: settings and help then open as the older floating overlays over the dimmed board (a square box with its title set into the top border, `┌─ HELP ──── rex v1 ─┐`), as stats, failures, overview, `:usage` and the wizard always do. In the overlays `tab` / `shift+tab` jump to the next / previous section of settings, and the page keys (`q`, `1`–`9`, `[`, `]`) do nothing; `j` `k` `h` `l` and the wheel keep their overlay meaning (rows within the list, `h` / `l` step a value). The change applies on the next frame, with the cursor kept. The attach popup uses the same square corners but keeps the board's colors around it.

### Help page

`?` / `:help` opens the help page. The left pane lists the groups: Navigation, Filter & search, Actions, Mouse, Modes, Legend, Session commands, App commands and Attached session. The right pane shows the selected group's rows at full height. It lists every `:` command, including `:settings` and `:undone`.

- **Groups.** `1`–`9` pick a group, `[` / `]` step through them (they stop at the ends), and on the left pane `j` `k` move. `enter` or `l` moves to the rows, `h` back. A new group starts at the top.
- **Scroll.** The rows pane reads as one text: `j` `k` (or arrows, or the wheel) scroll one row, and past the end of a group go on into the next one (`k` above the top goes back to the end of the previous one). `ctrl+d` / `ctrl+u` scroll half a window, `ctrl+f` / `ctrl+b`, `pgdn` / `pgup` and `space` a whole one; `gg` and `G` go to the first group's top and the last group's end. A group that does not fit scrolls under `↑ more` / `↓ more` markers. Under a filter the hits are one list and `j` / `k` stop at its ends.
- **Filter.** `/` opens an inline filter: only rows whose key or description contains every word stay (`prune` leaves `:prune`), from every group at once; groups with no hit are dimmed on the left, and no hit at all reads `no match for “…”`. `enter` keeps the filter, `esc` clears it first and a second `esc` closes. The header shows the query.
- **Legend.** Drawn with the board's own styles, so it always matches what you see: `◆` needs you, the spinner for working, the `waiting_spinner` still glyph (`◷` by default) for waiting (only while `waiting_section` is on), `◌` queued, `●` done, `✕` failed, `○` crashed, `▍` fleet member, `⋔ 3` subagents, the row icon strip while `row_icons` is on (`✦` finished since you last looked, `? asks`, `sh 42s` while `waiting_section` is on, `⟳ 3/8` while `loops` is on, `$0.42` while `usage` is on; the ASCII forms with `ascii_glyphs`), and the four long-running badges (`▴ 10m`, `▴ 30m`, `▴ 2h`, `▴ 6h`).
- **Features that are off.** Keys and commands behind a Features switch (`t`, `/`, `esc` for search, `z…`, `uc`, `:filter`, `:search`, `:uc`, `:rid`, `:export`) stay listed, dimmed and tagged `(off)`.

### Settings page

`S` / `:settings` opens the settings page. The left pane lists the sections, each with a dim count of its rows that differ from the default; the right pane lists the selected section's rows; the inspector under them is the detail pane below. The header shows the cursor row (`Appearance › Theme`) and the config file. `rex config` lists the same registry (it also shows the one hidden row, `keybind_preset`, which the page hides until a second preset exists).

- **Sections.** `1`–`9` pick a section, `[` / `]` step through them (they stop at the ends). On the left pane `j` `k` move the section and `enter`, `l` or `→` move to the rows; on the rows pane `j` `k` go on into the next or previous section, and `h` goes back to the sections. Each section keeps the row you left it on.
- **Values.** `←` / `→` (and `+` / `-`) step the row's value, as before: an enum, a number, a picker, a switch (`←` off, `→` on). On a row with no value to step (read only, an Onboarding row) `←` goes to the sections like `h`. `h` and `l` only move focus; they never change a value.
- **Search.** `/` filters live over label, id, section and help text (`spin` leaves Spinner type and Waiting icon), across every section: the right pane lists the hits grouped by section under a match count, or `no setting matches “…”`. `enter` keeps the filter, `esc` clears it, a second `esc` closes. `esc` while you type goes back to the row you were on; after `enter` it keeps the row you picked. While you type, `j` `k` and the rest type; `ctrl+u` clears.
- **Reload.** `R` reloads every layer, like `:reload`: the daemon's `tools.yaml`, `config.yaml`, `config.lua` and `init.lua`, and this board's `config.yaml` and themes. The status line shows `reloaded · changed: theme · needs daemon restart: summary_model`, or the first failed layer as `reload: file:line: message`.
- **Modified marker.** A `•` before a value means it differs from the default; `r` resets the row. Values sit flush right; a switch's `on` is drawn in the primary text color and `off` in the dim one.
- **Set in config.lua.** A row that `config.lua` sets shows its value dimmed with a muted `lua` after it, and the detail pane says where (`set in config.lua:12`). Enter, `←`/`→`, `r` and the inline editor refuse with `<id>: set in config.lua:12 — press e to open` on the bottom line; `e` opens that file at that line in `$EDITOR` (with `+N` for vi, vim, nvim, nano, emacs and micro) and reloads when the editor exits. `:theme` refuses the same way when `config.lua` sets `theme`, a quick-spawn tool or model change that would reset a model or effort `config.lua` sets is refused, and the setup wizard leaves such settings alone and lists them in its summary.
- **Detail pane.** Under the list, for the selected row: its **id** (what `rex config set <id>` takes), its **default**, **when a change applies** (`applies now`, `next attach`, `next new session`, `after a daemon restart`), the **options** with the current one lit (or the numeric range), and up to four lines of help (two on a terminal under 30 rows). `?` shows the full help text.
- **Onboarding.** The last section lists the agents the new-session wizard leaves out because their binary is not on the daemon's `PATH`: dimmed rows reading `not on PATH`, with the command it looks for and an install hint in the detail pane (the tool's `install_hint`, else `install <command> and put it on your PATH`). The rows are read only and not stored settings. It is empty (absent) when every agent is installed.
- **Quick-spawn pickers.** `default_spawn_tool` cycles through the installed tools only, and `default_spawn_model` through the daemon's merged model list; the detail pane adds `detected` when the current model was reported by the tool.
- **Editing text.** `enter` on a free-form string (`auto_trust_dirs`, `prompt_glyph`) opens an inline editor in the pane, prefilled with the value: `enter` saves, `esc` cancels, `ctrl+u` clears. A refused value keeps the editor open and shows why. The check is the one `rex config set` and `rex setup` use. `prompt_glyph` must be one printable character (a single grapheme, 1 or 2 cells wide: no control characters, spaces or invisible marks); the arrow keys still cycle the presets.
- **Accent color.** The `accent_color` row shows a swatch (`██`, `##` with `ascii_glyphs`, none on a terminal without color) in the accent being painted, before the value. `←`/`→` cycle `theme` and the named colors and apply at once. `enter`, `e` or `#` open the editor for your own value (`#ff6f61`, `rgb(255, 111, 97)`, `hsl(...)`, `cmyk(...)`, `oklch(...)`): it starts empty when the value is `theme`, a value that parses repaints the accent as you type, and the line under the draft shows the color (`→ #FF6F61`) or why it is not one yet. `esc` puts the saved accent back. The detail pane's first line says what the row paints and whether it was adjusted for contrast. See [themes.md](themes.md#accent-color).

## KEYS (board focus)

| Key | Action |
|-----|--------|
| `j`, `k`, `↓`, `↑` | Move selection |
| `g`, `G` | First / last row |
| `tab` | Next session that needs you (wraps) |
| `1`, `2`, `3` | Jump to Ready / Waiting / Working |
| `4` | Jump to Done; on a long Done column, show / hide it |
| `v` | Simple / detailed rows |
| `t` | Cycle tool filter (`all`, then each installed agent, e.g. `claude`, `codex`, `qwen`, `cursor`, `grok`) |
| `/` | Search the board (see FILTER AND SEARCH) |
| `esc` | Clear the search |
| `zh` | Hide / show every completed section (Done) |
| `z1`-`z9` | Hide / show section N (`z1` Ready, `z2` Waiting, `z3` Working, `z4` Done) |
| `z0` | Show every section |
| `enter` | Attach to selected session |
| `n` | New-agent page (the wizard with `new_agent_page` off) |
| `i` | Focus bottom prompt (quick-spawn) |
| `r` | Rename selected (display name; empty resets to slug) |
| `cc` | Mark selected done / undo (see MARK DONE) |
| `uc` | Undo done: unmark a running session, or resume an exited one (see MARK DONE) |
| `C` | Complete now: kill the agent and mark it done |
| `dd` | Delete selected (confirm) |
| `:` | Command mode |
| `?` | Help page |
| `S` | Settings |
| `p` | Pause / resume the selected loop (needs `loops`; see [loops.md](loops.md#on-the-board)) |
| `x` | Stop the selected loop, after `y` / `N` (needs `loops`) |
| `L` | List loops: state, turn, time over `max_minutes`, last check (needs `loops`) |
| `ctrl+c`, `q` | Quit (confirm with `:q`) |

### Mouse

With `mouse_enabled` on (the default) rex captures the mouse once the board is up:

| Action | Effect |
|--------|--------|
| Click a row | Select it |
| Double-click a row | Attach (same as `enter`) |
| Wheel up / down | Move the selection (same as `k` / `j`); the board scrolls with it |
| Click a filter chip | Set that tool filter |

Overlays (help, settings, stats, wizard, failures) and bottom-bar prompts swallow mouse events, so a click never changes the selection behind them; in settings the wheel moves the cursor and in help it scrolls.

Capturing the mouse turns off the terminal's own text selection. Hold Option (iTerm2/Terminal.app) or Shift (Ghostty/kitty) while dragging to select text natively, or turn `mouse_enabled` off in settings (`S`). The toggle applies immediately.

## ATTACH

`enter` opens an in-TUI attach view. Output streams from the daemon; keyboard input goes to the session PTY.

Detach with `qq` (two quick `q`s), **Ctrl+]** (ASCII 0x1d) or `::q`. The session keeps running. Like `::`, a lone `q` is held for 400 ms and forwarded to the agent if no second `q` follows (or as soon as another key arrives); pasted text is never intercepted. In scroll mode `q` keeps its own meaning (back to live output).

The popup is never wider than the screen: its agent terminal is 40 columns wide at least while the popup fits, and follows the screen below that. In the mobile layout (60 columns or fewer, see [Mobile layout](#mobile-layout)) the popup becomes a sheet: it fills the screen with only its border, no margin and no padding, so the agent gets `columns - 2` columns. Crossing the breakpoint while attached resizes the agent.

New agents (quick-spawn `i` and the wizard) start at the size the popup will give them, so the first attach needs no resize; agents that lay their screen out once, like Antigravity, stay correct. Until the terminal size is known the daemon default (120x32) is used.

`rex attach <sel>` from a shell uses the same detach key. It has no banner (see [Banner](#banner)): the agent draws straight on your terminal.

### Command bar (`::`)

Typing `:` twice quickly (within 400 ms) inside the popup opens rex's command bar over it instead of sending the colons to the agent. A single `:` is forwarded once the next key arrives or the window passes. Pasted text is never intercepted. `:::` sends a literal `::` (e.g. C++ `std::`).

| Command | Effect |
|---------|--------|
| `q`, `quit`, `detach` | Close the popup (session keeps running) |
| `done` | Mark this session done / undo |
| `kill`, `complete` | Kill the agent now and mark it done |
| `name <text>` | Set this session's display name (`name` alone clears it) |
| `banner` | Hide or show the [banner](#banner) for this run (not saved) |
| `border [arg]` | Same as `:border` ([window border](#window-border)); the popup stays open and the agent is resized by 2 columns and 2 rows |
| anything else | Close the popup and run it as a board `:` command |

### Banner

One row between the title bar and the agent's screen says what the session is doing without you leaving it:

```
↳ Rewriting the invoice rounding tests        code/rex · working 12m · updated 40s ago
```

- **Lead** (after `↳`): the AI description; without one, `task: ` and the task you gave it; without that, the question a waiting session is asking. Nothing otherwise.
- **Meta**, in order: the folder (`~` for your home directory, otherwise the last two path elements); `working 12m` (since the current stretch began), `waiting 3m` (needs you) or `ended 14:02` (when it finished); `updated 40s ago` (age of the description, dim past 2 minutes, left out without one).
- **With `overview_enabled` on:** `elsewhere: 2 to review · 1 ready · 1 failed` (the other sessions, never this one; `quiet` when none needs anything), then the overview headline.

`attach_banner` (Settings → AI summary) picks how long text is shown:

| Value | Behavior |
|-------|----------|
| `marquee` (default) | Text that does not fit scrolls as a ticker: it rests 2 s at the start, moves one cell per 120 ms, and rests again at the start of each pass. The loop restarts when the popup opens and when this session's description changes. Text that fits stays still and costs nothing |
| `static` | Cut at a word boundary (`…`); the right side keeps as many meta items as fit in half the row, none below 60 columns |
| `off` | No banner |

`reduce_motion` turns `marquee` into `static`. Below 25 terminal rows the banner gives way, so the agent never drops to its minimum height because of it. `::banner` hides or shows it for this run; `attach_banner` is the saved switch. The banner is rex's own row above the rule, so the agent cannot draw into it, and clicks or the wheel on it never reach the agent. Everything in it comes from the session (description, task, question, folder, headline) and is stripped of control characters and escape sequences before it is drawn.

The agent's terminal is one row shorter while the banner shows, as for any resize: full-screen agents redraw at the new size. Agents on the primary screen push their top row into rex's scrollback when the banner appears; it is not pulled back when the banner goes (no reflow, as for a window resize). `rex attach` from a shell shows no banner.

### Mouse

The popup shares the board's mouse capture (`mouse_enabled`). Wheel events over the agent's screen go to the agent as mouse reports when it enabled mouse tracking — Claude Code in fullscreen mode scrolls its own history this way. Agents on the alternate screen without mouse tracking get the wheel as arrow keys (what terminals do natively). Hold Option (iTerm2/Terminal.app) or Shift (Ghostty/kitty) to select text natively while attached.

The popup subscribes with `mode_sync`, so it learns which terminal modes the agent switched on before the popup opened.

### Opening a running agent

Full-screen tools in rex's list (claude, codex, gemini, …) are attached without transcript replay. Instead the daemon repaints its own copy of the agent's screen into the popup (`repaint`, see [protocol.md](protocol.md#subscribe-repaint)): colors, attributes, cursor and modes included. The popup shows the agent's current frame at once, even when the agent is busy or does not redraw on `^L`. The popup first resizes the session to its own size, so the repaint fits; when that changes the size, the agent also redraws itself. With a daemon too old to repaint, the popup sends the `^L` redraw nudge instead (run `rex daemon restart` to get repaints). `^L` in the popup still asks the agent to redraw.

### Scrollback

Agents on the primary screen (codex inline, ollama, echo, Claude Code without fullscreen) print history that scrolls off the top of the popup. rex keeps those lines (`attach_scrollback_lines`, default 5000, capped at 8 MiB) and the footer shows `^PgUp scroll` once there are any. Lines on the alternate screen are never kept: full-screen agents own their history (the wheel goes to them, above).

`ctrl+pgup`, or the wheel scrolled up over a primary-screen agent without mouse tracking, enters scroll mode. While scrolling, keys move the view and are **not** sent to the agent:

| Key | Effect |
|-----|--------|
| `k` / `↑`, `j` / `↓` | One line |
| `pgup` / `ctrl+b`, `pgdown` / `ctrl+f` | One page |
| `ctrl+u`, `ctrl+d` | Half a page |
| `g` / `home` | Oldest kept line |
| `G` / `end` / `esc` / `q` | Back to live output |
| wheel | Three lines per notch |

Scrolling back down to the bottom also returns to live output, as does the agent switching to the alternate screen. The title shows `SCROLL ↑N/total`. New output keeps the view where it is and the footer counts it (`↓ N new · G/esc to resume`). `ctrl+]` still detaches and `::` still opens the command bar. Kept lines are shown at their original width: wider lines are cut, narrower ones padded (no reflow after a resize).

Line-oriented agents (ollama, echo) are attached with transcript replay, so their scrollback includes output from before the popup opened. Full-screen tools in rex's list (claude, codex, antigravity, …) are attached without replay, so for those only lines that scroll off while the popup is open are kept. The repaint that opens the popup draws in place and adds none.

## FILTER AND SEARCH

The tool filter and hidden sections belong to the `board_filter` feature (Settings → Features, on by default). Off, the chip row is gone, `t` and the `z` chords do nothing, `:filter` answers `off: enable in Settings → Features (rex config set board_filter true)`, and `board_hidden_sections` and saved filter state are ignored. A search still shows its status on the filter row.

Search belongs to the `search` feature (on by default). Off, `/` does nothing, `:search` answers `off: enable in Settings → Features (rex config set search true)`, and a saved or stale query is ignored.

Three things narrow the board: the **tool filter** (`t` cycles it, `:filter <tool>` sets it, a click on a chip sets it), **hidden sections**, and a **search**. While any of them narrows the rows, section titles read `n/total` (`WORKING  1/4`).

### Search

`/` opens an editor on the bottom line (`/ query█   3 matches`) and narrows the board as you type.

| Key | Effect |
|-----|--------|
| `enter` | Keep the query and go back to the board |
| `esc` | Restore the query you had before `/` |
| `ctrl+u`, `ctrl+w`, `backspace` | Clear, delete a word, delete a character |
| `esc` (on the board) | Clear the search |

`/` again reopens the editor on the current query. `:search <text>` sets the query from the command line (also `::search` from attach), and a bare `:search` clears it ([slash.md](slash.md)).

- A row matches when it contains **every** word of the query, as a case-insensitive substring. There is no regex and no fuzzy matching.
- The words are looked up in what the rows show (name, task or the question a session is waiting on, AI summary, tool, model · effort) plus the slug, short id, fleet, the folder name of the session's directory and its resume id. Raw screen text is not searched.
- At most 16 words and 128 characters; control characters are dropped.
- A search unfolds a long Done column, so hits are never folded away. Empty sections say `no matches`.
- A search combines with the tool filter and hidden sections (all must match). Matches inside hidden sections are not shown, but the filter bar counts them: `+2 in hidden sections (z0)`.
- The selection stays if its row still matches, else moves to the first match.
- The search lasts for the run. `:bg` saves it for the next start (see DETACH WITHOUT QUITTING). `:filter` with no argument clears it along with the other filters.

### Hidden sections

A hidden section is gone from the board: no title, no rows, no navigation, and the others close up. The header counts stay global.

| Key | Effect |
|-----|--------|
| `zh` | Hide every completed section (Done); again shows it |
| `z1`-`z9` | Toggle section N in board order (`z1` Ready, `z2` Waiting, `z3` Working, `z4` Done) |
| `z0` | Show every section |

`z` followed by any other key is just that key. While `z` is pending the help line lists the second keys. rex refuses to hide the last shown section.

`:filter` does the same by name: `:filter hide done`, `:filter show all`, `:filter only needs working` (`wa` is Waiting, `wo` Working; a bare `w` is ambiguous), and a bare `:filter` clears the tool filter and every hidden section ([slash.md](slash.md)).

- The filter bar names what is hidden (`done hidden (zh)`) and shows while any section is hidden.
- When the selection falls into a hidden section (for example `cc` marks it done while Done is hidden), it moves to the first visible row. `1`-`4` and `tab` aimed at a hidden section say how to show it. A session you just started that the filter hides shows the notice `<slug> is hidden by the filter`.
- Hiding is per run. The `board_hidden_sections` setting ([settings.md](settings.md#behavior)) sets what starts hidden, and `:bg` saves the current choice for the next start (see DETACH WITHOUT QUITTING).
- `:prune` ignores filters: it clears every Done session, and its confirmation says so while a filter is active.
- The tool filter survives a daemon reconnect.

## OVERVIEW

What needs you, what is ready for review and what finished, at a glance. Three places show it: a header line, the `:overview` overlay and `rex overview` ([cli.md](cli.md)).

**Header line.** With `overview_enabled` on (Settings → AI summary, off by default), one line follows the chip row (and comes before the summary-backend banner):

```
  » 2 to review  billing-tests and api docs are ready for review; deploy asks a question · 40s
```

The count is computed in the TUI from the sessions, so it is true even when the headline is stale or wrong; it reads `nothing to review` at zero. The headline is the daemon's one-line summary of the whole board, cut on a word boundary, with its age. Until the daemon has written one (or against an older daemon, or with the daemon's setting still switching on, which takes about 5 s) only the count shows. The row is reserved while the setting is on, so the board does not jump. Off, the header is exactly as before.

**Buckets.** Every session is in exactly one, decided in code by these rules, in order:

1. Marked done: **Completed**.
2. `failed` or `crashed`: **Failed**.
3. `done`: **Review**. The agent says it finished and you have not marked it.
4. Waiting for input (`needs_input`): **Review** when the agent finished a turn (Claude's Stop hook, `wait_reason: review`) and the last line is not a question (does not end in `?`, the same rule as the row text); otherwise **Ready**. Claude's idle reminder does not turn a finished turn into a question. Tools without hooks have no `wait_reason`, so a waiting session is always Ready.
5. `working` or `queued`: **Working**.

**`:overview` overlay.** The headline, wrapped, then the sections REVIEW, READY, FAILED, WORKING and COMPLETED (last 24 h) with `marker name status age` rows; empty sections are left out. `j`/`k` (or arrows) select, `g`/`G` jump, `enter` attaches to the selected session, `esc` or `q` closes. It works with the setting off too: you get the lists and a line pointing at the setting.

The headline is written by the local summary model from the sessions' task titles, descriptions and question lines, so it can be wrong and can be steered by what an agent printed. It is one line of display-safe text, never acted on, and the lists next to it never come from it.

---

**Export.** `:export [sel] [json|yaml|md|txt]` writes the session as a readable document to `~/.local/share/rex/exports/` and shows the path ([export.md](export.md)). It belongs to the `session_export` feature.

## USAGE

What each agent used, and how much of its quota is left. Two places show it: the `:usage` page and a meter at the right end of the header. Both belong to the `usage` feature (Settings → Features, on by default); with it off `:usage` answers `off: enable in Settings → Features (rex config set usage true)` and the meter is hidden. The numbers come from the daemon (`rex usage` shows the same, see [cli.md](cli.md)); the TUI never reads the ledger.

**`:usage` page.** A titled panel with the range (`today · 7d · 30d`) and a table: per agent with its models indented, then a `TOTAL` row.

```
AGENT / MODEL     SESSIONS   TOKENS      COST QUOTA          RESETS
claude                  14     1.2M    $18.40 5h ▰▰▱▱▱  42%  2h10m
                                               wk ▰▱▱▱▱  18%  4d
  opus                   3     410k    $11.90
grok                     2     ~88k     $0.70 mo ▰▰▱▱▱  31%  9d
antigravity              1     ~40k         — wk ▰▰▰▰▰  91%  as of 11:20
```

- `←`/`→` (or `h`/`l`) step the range, `1` `2` `3` pick today, 7d, 30d. `r` asks the daemon again (it polls the tools that can be polled). `/` filters by tool or model name (the total follows the filter), `esc` clears it and then closes. `j`/`k`/`g`/`G` scroll a long table; the total stays pinned.
- A `~` before a number marks rex's own token estimate (`output bytes / 4`), not a count the tool reported. A `—` cost means no counted session reported one.
- Quota bars: green below 60%, amber from 60%, red from 85%. A reading older than 15 minutes (or one the daemon marks stale) is dimmed and shows `as of HH:MM` where the reset time would be. A tool that reports one quota for the whole plan shows it on the agent row; model rows show a quota only where the tool reports one per model.
- Below about 66 columns, and in the mobile layout, each agent is a card: totals, models, quota lines last.
- Errors say why there is no table: `update daemon: run 'rex daemon restart'` for a daemon that predates `UsageQuery`, the off message when the daemon has usage off, `not connected to the daemon` while it is down.

**Header meter.** `usage_meter` (`off` | `compact` | `full`, default `compact`; Settings → Features) draws the right end of the header's top row: for each agent with a live session, the worst of its quota windows as a 5-cell bar and a percent (`claude ▰▰▱▱▱ 42% · codex ▰▰▰▰▱ 72%`). `full` lists every window with its reset time. Colors follow the same thresholds as the page, and a stale reading is dim. The counts always win: the meter drops agents from the right, then disappears, before the counts are cut. It is also hidden against a daemon that sends no quotas. A click on it opens `:usage`.

## UNDO AND REDO

`:undo [sel] [-turns N]` and `:redo [sel]` put a session's folder back from the turn snapshots ([snapshots.md](snapshots.md)); they are the TUI side of `rex undo` / `rex redo` ([cli.md](cli.md#rex-undo--rex-redo)). `sel` defaults to the selected row. They belong to the `turn_snapshots` feature (Settings → Features, **off by default**): off, both answer `off: enable in Settings → Features (rex config set turn_snapshots true)` and send nothing.

The dialog never changes a file until you confirm:

1. The TUI asks the daemon for a **dry run** (`Undo` / `Redo` with `dry_run`), showing `checking what would change…` meanwhile (`esc` lets go of it).
2. It shows `restore N files, delete M files` with the first few paths, and the line `shell side effects (installs, databases, files outside the folder) are not undone`. `y` or `enter` runs it, `n` or `esc` cancels.
3. After the run it shows what was restored and deleted, then any `UndoResult.Warnings` (the folder changed while the restore ran, the session started working), then a reminder that the agent was not told and `:redo` brings the changes back. `esc` closes. If the run itself fails, the message stays in the dialog, since the folder may be part way, and `:redo` recovers it.

**Another live session in the same folder.** The daemon refuses an undo that would revert a second live session's files. Only in that case the dialog opens on the refusal (it names the other sessions) with `f force`; `f` asks for a new dry run with force and the confirmation then says `forced: the other live session in this folder is reverted too (...)`. Nothing else offers a force option.

**Refusals** (the session is working, the folder is not inside a repository, nothing to undo or redo, blocked paths, an old daemon) close the dialog and show the daemon's message on the status line, prefixed with `undo:` or `redo:`. `-turns` takes 1 to 1000 and belongs to `:undo` only.

## NEW-AGENT PAGE

`n` or `:new` opens one full-screen page (`new_agent_page`, Features, on by default) with everything a new agent needs: the agent, a multi-line task, context and folder trust. It lands typing in the task editor with the agent you launched last (else `default_spawn_tool` / `_model` / `_effort`), so the common path stays `n`, type, `enter`.

```
  ∴ REX › NEW AGENT                 ~/dev/rex                       draft
──────────────────────────────────────────────────────────────────────────
  STEPS          │ AGENT
  ✓ Agent        │ ◆ Claude Code  ·  opus  ·  effort max   ctrl+g change
  ▸ Task         │ TASK
  · Context      │ ┌──────────────────────────────────────────────────┐
  ? Trust   ask  │ │ fix the flaky auth test█                         │
  › Launch       │ └──────────────────────────────────────────────────┘
                 │ CONTEXT
                 │   none yet: type @path, paste, or ctrl+o to import
                 │ TRUST
                 │   folder not trusted: the agent will ask
                 │
                 │   launch  ·  prompt 23 B of 96 KB · slug cc.opus.fix-the-flaky
──────────────────────────────────────────────────────────────────────────
  enter launch · ctrl+j newline · @ file · ctrl+o import · esc navigate · tab next
```

The page has two modes, as in vim:

- **Editing** (where it opens): keys type into the task. `enter` launches, `ctrl+j` / `alt+enter` add a line, `esc` leaves the editor (an open `@` list closes first), `tab` / `shift+tab` leave it for the next or previous step. `ctrl+s`, `ctrl+g`, `ctrl+o`, `ctrl+t` and `ctrl+a` work here too.
- **Navigating**: the form is one list, Agent, Task, each context item, Trust, Launch. `j` / `k` walk it (clamped), `gg` / `G` go to the agent row and the launch row, `h` focuses the step rail (`j` / `k` there move a step at a time, `l` or `enter` come back), `[` / `]` and `1`–`5` jump a step. `i`, `I` (start of line), `a`, `A` (end of line) or `enter` on the task type again. `enter` acts on the step: the Agent screen, the item's preview, the next trust choice (`space` too: ask, this session, always), or the launch. `esc` or `q` closes the page and keeps the draft.

| Key | Editing | Navigating |
|---|---|---|
| letters, digits | type | page keys (`j` `k` `h` `l` `g` `G` `i` `a` `x` `d` `r` `e` `q` `1`–`5` …) |
| `enter` | launch | act on the step |
| `esc` | leave the editor | close the page |
| `tab` / `shift+tab` | leave the editor: next / previous step | next / previous step (wraps) |
| `ctrl+s` | launch | launch |

- **Steps.** The left rail lists Agent, Task, Context, Trust and Launch, each with a mark and a word (`✓`, `·` empty, the item count, `ask` / `sess` / `always` / `trusted`); `tab` / `shift+tab` move between steps and `1`–`5` jump (outside the editor). Under 60 columns the rail becomes a strip, `‹ Agent · [Task] · Context 2 · Trust · Launch ›`, `h` / `l` step through it, the header keeps `draft` and drops the folder, and the form is one column.
- **Agent.** `ctrl+g` (or `enter` on the agent row) opens the Agent screen: agents on the left with their brand glyph (missing ones dimmed under `not installed`; `enter` there opens Settings → Onboarding), and on the right models with a `/` filter, effort as `low  [medium]  high` and skills. `h` / `l` move between the agents and the right column; `j` / `k` move within: on the right they go from the last model on to effort, then to skills, and back. `gg` / `G` and `ctrl+d` / `ctrl+u` work on the agents, models and skills lists (the effort row is one row: `gg` / `G` do nothing there) (a tool with 200 models scrolls one row at a time). On the effort row `←` / `→` change it (`+` / `-` change it from any pane); `space` toggles a skill; `tab` / `shift+tab` and `1`–`4` pick agents, models, effort or skills. While you type the filter, keys type and `↑` / `↓` move through the matches; `enter` keeps the filter and `j` / `k` then move through the matches; `esc` clears it. `enter` is done and `esc` (or `q`) comes back. Until the daemon has said which agents are installed, the AGENT section reads `⠋ checking which agents are installed…`; against a daemon that predates the tool list every tool counts as installed.
- **Task.** `enter` launches; `ctrl+j` or `alt+enter` adds a line; `ctrl+w` / `alt+backspace` delete a word; `ctrl+u` clears the line, and on an empty editor drops the whole draft; `ctrl+e` edits the task in `$EDITOR`. A paste never launches: one of more than 8 lines or 1 KiB becomes a `paste N` context item with a `[paste N · L lines]` placeholder in the task.
- **Context.** Pastes, workspace files and folders. `@` at a word start opens a completion list of workspace paths; `ctrl+o` opens the import picker (type to match, `space` marks, `enter` adds, `→` / `←` browse folders). In the list `j` / `k` move (on past the last item to Trust), `enter` previews, `x`, `d` or `delete` removes, `r` renames and `e` edits a paste in `$EDITOR`. The preview scrolls with `j` / `k`, `gg` / `G` and `ctrl+d` / `ctrl+u`; `esc`, `q` or `h` go back. A path must resolve inside the session folder and be a regular file or folder; a name that looks like secrets (`.env`, `*.pem`, `id_rsa*`, …) is added only after `y`. Limits: 32 KiB per paste, 64 KiB of pasted text, 32 items, a 96 KiB prompt (16 KiB for agents that take a pasted prompt). Ollama takes its prompt as typed lines, so it only launches a one-line prompt with no context. A draft whose files were added in another folder shows `! files from <folder>, not this folder` and takes no new paths until `x` removes them. The last row shows the prompt's size against that limit and the slug.
- **Trust.** `ctrl+t` trusts the folder for this session only, `ctrl+a` saves it to `auto_trust_dirs` at launch; `enter` or `space` on the Trust step cycles ask, this session, always. The focused section names the exact folder each key trusts (as in the wizard below).
- **Launch.** `enter` in the editor, `enter` on the Launch row (the last row: `▸ launch · prompt 2.0 KB of 96 KB · slug …`), or `ctrl+s` anywhere. The page checks every cap and path first, then stays open with `⠋ starting…` until the daemon answers: success closes it and selects the new card; a failure shows `✗ launch failed: …` and keeps the task and context.
- **Draft.** `esc` closes the innermost thing (completion, editor, picker, preview, Agent screen, then the page). Leaving the page keeps the draft in memory for the next `n`; nothing of it is written to disk. A board swap (`:swap`, `rex upgrade`) never carries it: the swap waits while the page is open, `auto_swap` waits while a draft is kept, and an explicit swap over a kept draft drops it and the new board says so.
- **Help.** `?` outside the editor opens help on the page's keys alone; `esc` comes back.
- **Mouse.** The wheel moves one row, as `j` / `k`, on the pane under the pointer (the rail or the form; on the Agent screen the agents or the right column); it leaves the editor.

Every state has a glyph or a word besides its color: `▸` on the cursor, `[x]` marks, the bracketed effort, `✓` / `·` / `?` rail marks, `✗` errors and `!` warnings.

The first prompt is the task, then `Context from rex (read before starting):` with one `- file:` or `- dir:` line per reference (workspace-relative paths; the agent reads them itself), then each paste in a `<pasted name="paste 1" lines="38">` block. No file is copied and no shell runs.

## NEW-SESSION WIZARD (with `new_agent_page` off)

With `new_agent_page` off, `n` or `:new` opens a short flow. Steps that do not apply are skipped, in both directions, so the common path is a few Enter presses:

1. **Provider**: the tool (`j`/`k`, `enter`). Only installed tools are listed: the daemon reports which binaries are on its `PATH` (`Snapshot.tools`) and says so again when it reloads (`ToolsChanged`), and the open wizard updates in place, keeping your cursor on the same tool. Qwen Code, Cursor CLI and Grok Build are listed after the others (Claude Code, OpenAI Codex, Antigravity CLI, Ollama) when their binaries (`qwen`, `cursor-agent`, `grok`) are on the daemon's `PATH`; a missing one is under Settings → Onboarding with its install hint. With `extra_agents` off, qwen, cursor and grok never show. Against a daemon that predates the tool list every tool counts as installed. A long list scrolls.
2. **Model**: the tool's model, from the daemon's merged list: the models the tool itself reported first, each tagged `detected`, then the built-in ones it did not list. A long list scrolls. Skipped when the tool has one model. It starts on `default_spawn_model` when the tool is `default_spawn_tool`, else on the first model.
3. **Effort**: the reasoning effort of the chosen model. Skipped when it has none.
4. **Skills**: a checklist of the skills in `~/.config/rex/skills` (`j`/`k` move, `space` toggles, `enter` continues; long lists scroll). Loaded for this session only. Skipped when the library is empty, when the tool cannot load skills per session (Antigravity cannot), and when `spawn_skills` is off in Settings → Features. See [howto/skills.md](howto/skills.md).
5. **Describe**: the task. `enter` launches. The slug comes from the task. Under the input a trust line says whether the session's folder is trusted:
   - `folder not trusted` with two choices, each followed by the exact folder it trusts: `ctrl+t` trusts the folder the session starts in for this session only (nothing is saved), `ctrl+a` saves the repo folder (the root of the git checkout, or the folder itself; never its parent) to `auto_trust_dirs` when you launch, and trusts the session. Press the key again to take the choice back. Nothing is trusted unless you press one.
   - `trusted (auto_trust_dirs)` when a listed folder already covers it.
   - `home folder: start rex in a project folder to trust it` when rex was started in your home folder (or a folder that holds it): neither key works there.

The wizard does not ask for a name; press `r` on the board to give a session a display name, or use `rex new -name`.

`:trust [folder]` on the board does the same as `ctrl+a` without starting a session: it saves the repo folder of the selected session's folder (or of the folder rex runs in, or the folder you name) to `auto_trust_dirs` and says which folder it trusted. Sessions already running keep asking; new ones skip the dialog. A refused folder shows why on the status line. `rex trust` does the same from the shell.

`b` goes back (on the describe step only while the field is empty), `esc` cancels. If the `rex-daemon` is older than the skills field, the status line says `this rex-daemon ignored skills; run \`rex daemon restart\``. Quick-spawn (`i`) is unchanged.

---

## RENAME

`r` edits the selected session's **name**: free text (64 characters max) shown in the name column instead of the slug. The slug stays the selector for commands and scripts. Enter an empty name to go back to the slug. Control characters, escapes and bidi overrides are stripped; whitespace collapses. Names are stored by the daemon and appear in every connected TUI and in `rex ls`.

## MARK DONE

`cc` marks the selected session done **without stopping it**. The row dims, moves to **DONE** whatever its live state. The detailed view's span column shows when it was marked, and its header shows `N marked done`. The notice when marking says when it will clear; the attach footer keeps showing the countdown. The session stays fully usable until the deadline (`retire_after_hours`, default 168 = one week). Then the daemon kills the agent, removes the row, and deletes the session's directory (`meta.json`, `transcript.log`, `hooks.log`). Claude Code's own conversation files are never touched.

- `cc` again undoes the mark. `uc` (or `:uc [sel]`) does the same without ever marking: it unmarks a session that is still running and says `<name> isn't done` for one that is not in Done.
- `uc` and `:uc` belong to the `undo_complete` feature (Settings → Features, on by default). Off, `u` is unbound and `:uc` answers `off: enable in Settings → Features (rex config set undo_complete true)`; `:undone` still unmarks.
- `uc` on a session that has exited (done, failed or crashed, including after `C`) resumes it in place when it has a resume id (the `resume id` column): the status line says `resuming <name> (resume id 1a2b3c4d…)`, the row keeps its slug, start time and transcript, leaves Done for Working, and the agent is started again with its own resume arguments (`claude --resume <id>`). The daemon clears a done mark when it respawns. A session whose tool can't be resumed (no id) gets `uc: <name> has exited and <tool> can't be resumed` and stays in Done; with the `resume` feature off it says `resume is off`. A row already being cleared answers `too late — session is already being cleared`. `:resume [sel]` is the same as `:uc [sel]`.
- Typing into the session (attach, `rex reply`, `rex send`) cancels the mark: using it again means it isn't done. Just viewing does not.
- A session still **working** at the deadline is not cleared; it is cleared as soon as it settles (the attach footer reads `clears when idle`).
- `cc` again moves the row back to the column its live state belongs to.
- The deadline is stored by the daemon, so it survives TUI exit and daemon restarts. A deadline that passed while the daemon was down is cleared on the next start.
- `C` (or `:complete`) is the old immediate behavior: kill now, keep the row as done.

## SOUNDS

With sound on, each soundset chimes when a session needs you (`working` → `needs_input`), when it resumes (`needs_input` → `working`), and when it finishes (→ `done`) or you mark it done (`cc`, `:done`, `rex done`). A flickering session chimes at most once every 1.5 s. The needs-you chime plays once per turn (per prompt you send a Claude Code session); a new question in the same turn chimes again. Other agents report no turns, so only the 1.5 s limit applies to them. Turn off the first two with `notify_on_state_change`; see [settings.md](settings.md#audio).

## WORKSPACE BOARD

`rex here` (or `rex ws open [path]`) opens the board fixed to one workspace ([workspaces.md](workspaces.md#sessions-and-the-workspace-board), needs `workspaces`). The filter is not a user filter: no key or command changes it, and the chip row does not mention it. Rows, header counts, the screensaver, the overview, `:prune`, `:fail`, the loops overlay, session selectors (`:rm fix-auth`, `:undo 7d4f`, …) and sounds and notifications only see that workspace's sessions. The daemon-wide overview headline is hidden, since it speaks for every workspace.

- The header starts with `[<id> <name>]` in the workspace color (`[<id>]` in the mobile layout). A color that is not `#RRGGBB` is ignored and the chip uses the default style.
- The window border is on by default and uses the workspace color while `window_border` was never set and `window_border_color` is `random`. `window_border`, an explicit `window_border_color`, and `:border` all win. `:border` says `workspace color` instead of `random` in its notice.
- The wizard and the `i` quick spawn start in the workspace root.
- A daemon that does not tag sessions shows every session, with the one notice ``daemon doesn't tag workspaces — `rex daemon restart` ``.
- With `workspaces` off the scope is inert: the board shows everything, with no chip.

## WINDOW BORDER

`window_border` (Settings → Appearance; off by default) draws a thick colored frame around the whole screen, so several rex windows are easy to tell apart. It is built from block glyphs: `█` on the sides and corners, `▀` on top and `▄` at the bottom, so all four edges look equally thick.

- The frame costs 2 columns and 2 rows. Every view lays out inside it, so the board, overlays and the attach popup need no frame code; the attached agent gets a terminal 2 columns and 2 rows smaller (a normal resize when the frame turns on or off).
- It is hidden on the boot splash and on the screensaver (both fill the whole screen; the screensaver keeps the layout size, so an attached agent is not resized when it starts), and when the terminal is smaller than 50 columns by 16 rows (the layout area would be under 48 by 14).
- `window_border_color` is `random` (default: one color per launch folder, so a project keeps its color), a color name or `#rrggbb`; see [settings.md](settings.md#border-colors). The `ansi` scheme uses the terminal's own palette index for the color.
- `:border` toggles the frame for this run. `:border on|off` sets it, `:border next` moves to the next color of the random pool, `:border random` goes back to the folder's color, and `:border <name>` or `:border #rrggbb` picks one. It answers with a notice such as `border: tangerine (random · :border next)`, and says so when the terminal is too small to show it. `::border` does the same inside the attach popup. The choice lasts until rex exits and is never saved; use the settings for that. It works with `window_border` off.
- With `NO_COLOR` the frame is still drawn, without color (`:border` says so).
- Mouse clicks on the frame are ignored; other clicks are translated to the layout area, so the board, chips and attach popup behave as without a frame.

## SCREENSAVER

After 5 minutes (`screensaver_after_minutes`) with no key or mouse event, the screen gives way to a small screen: `∴ rex` in the center and the header's counts under it.

```
             ∴ rex

1 ready · 3 working · 7 done
```

- `screensaver_counts: vertical` stacks the counts under the logo, one per line, with the numbers in one column, so your eyes don't travel along a line. When the terminal is too short for that, they go back to one line. `off` shows the logo alone.

```
   ∴ rex

 1 ready
 3 working
 7 done
```

- `screensaver_clock` (`24h` or `12h`) shows the time under the logo. It changes on the minute. On a short terminal the blank lines go first, then the clock, then the logo; the counts stay.
- The counts stay live: daemon events keep updating them. `ready` is highlighted while it is above 0, and `· N failed` shows in red while a session has failed or crashed. While the daemon is unreachable, the reconnect status shows under the counts.
- Any key or mouse event (click, wheel), or a terminal resize, brings the screen back. That key or click does nothing else, so an `enter`, `q` or `ctrl+c` that wakes the board never opens, quits or deletes anything. A half-typed `dd` or `cc` from before is dropped.
- It starts whatever is open: an overlay, prompt, confirmation, the new-session wizard, the settings page or the attach popup left open still times out. Waking returns to it as you left it (typed text, wizard step, attached session). An attached session's output keeps reaching its terminal meanwhile. Only the boot splash never times out.
- Once a minute it moves a little (at most 6 columns and 2 rows from the center), so a screen left on for hours does not keep one image in the same place. With `screensaver_drift` off or `reduce_motion` on it stays centered.
- It uses no CPU while idle: the spinner stops, and one timer fires at most once a minute (with the clock on, on the minute). It plays no sound.

Turn it off with `screensaver_enabled` (settings page `S`, or `rex config set screensaver_enabled false`). `screensaver_counts`, `screensaver_clock` and `screensaver_drift` change how it looks; all apply live and can be set from a trusted workspace's `.rex/config.lua`. See [settings.md](settings.md#behavior).

### Keep awake (macOS)

With `screensaver_keep_awake` on (the default), rex runs `/usr/bin/caffeinate -d -w <rex pid>` while the screensaver shows, so the display does not sleep. It needs no root. rex stops it when the screensaver goes away and when the TUI exits; `-w` makes it exit by itself if rex dies. On other systems the setting does nothing.

- rex holds the display on only while the screensaver shows. If macOS turns the display off sooner (System Settings, Lock Screen, "Turn display off ... when inactive"), the display is already off when the screensaver starts, and rex does not turn it back on. Set `screensaver_after_minutes` below that time.
- rex cannot tell whether you are working in another app. While the screensaver shows in a background window, macOS does not turn the display off for inactivity. Turn `screensaver_keep_awake` off if you don't want that.

## LONG-RUNNING BADGE

A small column before the time column flags sessions that have been **working** without a break. The column is hidden while no row has a badge:

| Badge | Working for |
|-------|-------------|
| (blank) | under 10 min |
| `▴ 12m` dim | 10–30 min |
| `▴ 45m` amber | 30 min – 2 h |
| `▴ 3h` red | 2–6 h |
| `▴ 7h` red, inverse | 6 h + |

## DETACH WITHOUT QUITTING

`:bg` or `:detach` saves selection, tool filter, hidden sections and the board search to `tui-state.json` and exits the TUI. The daemon and all sessions continue. The next `rex` restores them once (the saved hidden sections replace `board_hidden_sections` for that run), then deletes the state file.

## LOADING A NEW BUILD (`:swap`)

The board checks its own executable every 10 s. When a new build is installed (`make install`, `rex update`) the status line says `new rex build installed · :swap to load it`. `:swap` re-execs the board into the new binary at once; with `auto_swap` on (the default) it also does so by itself once it has sat on the board for 60 s with no key, no overlay, wizard, dialog or editor open. `rex upgrade` asks every board to swap right away.

A swap keeps the view: selection, scroll, tool filter, search, hidden sections, the open page (board, help, settings, usage, stats, overview, loops) and the attach popup when its session is still running. The state goes through `tui-swap-<pid>.json` next to `tui-state.json` (0600, read once and removed, pid-checked). The new board boots quietly (no boot sounds, as `--quiet-boot`). An open wizard, new-agent page, confirm dialog, rename or inline editor is not carried: an automatic or `rex upgrade` swap waits until none is open. If the exec fails the board keeps running and says why. Not on Windows (the status line says so).

When the daemon hot-restarts, boards reconnect by themselves and an open attach popup re-attaches to the same session (backoff 250 ms to 2 s, 10 tries) instead of showing it as ended.

## PROMPT

`i` focuses the bottom prompt. Submitting spawns a session using `default_spawn_tool`, `default_spawn_model`, and `default_spawn_effort` from [settings.md](settings.md). The text becomes the agent's first prompt and the session's title (shown in the description column until an AI summary arrives). The new session is selected when it appears. If the daemon rejects the spawn, the error shows in the status line.

## SEE ALSO

- [slash.md](slash.md) — `:` commands
- [cli.md](cli.md) — non-interactive equivalents
- [settings.md](settings.md)
- [paths.md](paths.md) — `tui-state.json`

## BUGS

Help text in `rex --help` may mention selector forms the TUI does not implement. TUI local resolution accepts session id, `slug`, or `short_id` only; a duplicate slug is reported as ambiguous.
