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

**WAITING** holds working sessions that are blocked on a tool, not on you: a shell command, a subagent or a web fetch or search (Claude Code reports it through its tool hooks). A session moves there once the tool call has run for 3 seconds, so quick calls never make a row jump, and back to WORKING when the call ends. A session that needs your input is never in WAITING, and neither is one you marked done. Waiting rows play no sound and send no notification. Its marker is `◷`, which turns `◴ ◷ ◶ ◵` once a second (still `◷` with `reduce_motion`). Other agents have no tool reports yet, so their sessions stay in WORKING. Known limitation: a command that first needed a permission prompt shows as READY while you decide and as WORKING (not WAITING) after you approve it, because Claude reports the call's start before the prompt; the next tool call or turn corrects it. The Done key shown in the fold hint (`4`) is `3` while the section is off. `waiting_section` in Settings → Features turns the section off (they then show in WORKING). The bottom prompt (`λ` by default) accepts quick-spawn text, and the help bar lists five keys: `enter open · i new · cc done · tab next · ? more`. Everything else is under `?`.

The filter bar (`all · claude · codex …`, cycled with `t`) only shows while sessions use more than one tool, a filter other than `all` is active, a section is hidden, or a search is active. Hidden sections are named after the chips (`done hidden (zh)`, or `needs, done hidden (z0 shows all)`), and a search reads `/auth · 3 matches · esc clear`. See FILTER AND SEARCH.

A DONE column with more than 5 rows folds into its title line (`DONE  7 hidden · 4 show · :prune clear`). `4` shows and hides it; `:prune` clears it ([slash.md](slash.md)). When a row folds away (for example you mark the selected session done), the selection moves to the first row shown.

The daemon auto-starts if the socket is unreachable (boot splash).

## ROW COLUMNS

By default a row shows four things: state, name, text and age.

```
 ▸ ◆  release-notes   Which feature should lead: search or export?          11m
     state name       text                                                  last event
```

The **text** is the task you gave the session. A session that needs you shows the last line of its screen instead when that line is a question (ends in `?`); otherwise that line is usually the agent's own status text. A session started without a task shows its AI summary. When the text does not fit, it is cut on a word boundary and ends in `…`.

Under the board, a dim preview line (`↳ …`) shows the selected session's full AI summary and its age (`· 12s`), in both views, while any session has a summary. In the detailed view it appears only when the row cuts the text. It takes one row (two on a terminal 30+ rows tall with `summary_detail: long`, none below 20 rows), reserved for the whole board so the layout does not move as the selection does. `summary_preview` in Settings → Features turns it off. For Antigravity sessions the board's last line skips agy's `Generating...` spinner and the text in its input box.

`v` switches to the detailed view, which adds the short id, model · effort, tokens and the start→end span, shows the AI summary as the text, and adds `· N marked done` to the header:

```
 ▸ ⠋  b203  release-notes   draft ready — which feature leads?   gpt-5-codex · medium   123K tk  ▴ 45m   11m
     state  id  name        description                          model · effort         tokens   badge  last event
```

- **span** is when the session started and ended (`14:02→15:30`; `14:02→` while it is live; `Jan 2` for an earlier day, the year for an earlier year). The end is when the row entered Done: the earlier of the time you marked it done and the time it reached done, failed or crashed. The simple view has no timestamps. The `timestamps` feature (Settings → Features, on by default) switches the span column off in both the fixed and the fit layouts; the end time is still recorded.
- **resume id** is the first 8 characters of the agent's conversation id (`1a2b3c4d`), the id `claude --resume` takes. It only appears in the detailed view on a wide terminal (150+ columns). Rows in Done show it in full colour, live rows dim. `:rid [sel]` puts the full id on the status line so you can select it with Option/Shift-drag. The `resume` feature (Settings → Features) switches the column and `:rid` off.
- **name**, **model · effort**, and **tokens** have fixed widths (see below). Tokens are right-aligned.
- The **waiting** detail: in the detailed view a row in WAITING starts its text with the reason and how long it has waited (`shell 42s · Running the test suite`). The attach header chip reads `◷ waiting (shell)`.
- The **subagent** column (`⋔ 3`: running subagents) shows how many subagents a Claude Code session is running, in both views. Counts above 99 show as 99. The **badge** column shows the long-running badge. Both columns are always reserved, blank on rows that have nothing to show, so nothing shifts when a subagent starts.
- **last event** is right-aligned against the edge; `-` means no event yet.
- The description takes the remaining width. Rows always fill the terminal width exactly and never wrap.

### Column widths

Column widths never depend on what the rows contain: a long name, a first subagent or a token count moves nothing. They change only when the terminal crosses a breakpoint, and each breakpoint drops whole columns:

| Terminal width | Simple view | Detailed view (`v`) adds |
|----------------|-------------|--------------------------|
| 150+ (wide) | name 22 · subagents · badge · age | id · model 24 · tokens · span · resume id |
| 110–149 (normal) | name 20 · subagents · badge · age | id · model 16 · tokens |
| below 110 (narrow) | name 16 · subagents · badge · age | nothing; the text is the AI summary |

A column that a class shows is always reserved, even when every row's cell is empty. With the `resume` feature off the resume column is gone.

`column_layout` in Settings → Appearance switches the default `fixed` for `fit`, which sizes name, model and tokens to their widest value on the board (name capped at 22, model at 24), shows the subagent, badge, tokens, span and resume columns only while some row has a value, and gives way on narrow terminals in this order until the description has 20 columns: the resume id is hidden, the span is hidden, tokens are hidden, model shrinks (down to 12), name shrinks (down to 12), model is hidden, subagent count is hidden, long-run badge is hidden. After that the description shrinks, then the name.

### Mobile layout

At 60 columns or fewer (a phone over SSH) the board stacks each session on two lines. `mobile_layout` in Settings → Appearance sets when: `auto` (the default) uses it at 60 columns or fewer, `always` forces it, `off` never uses it (the narrow columns then squeeze to fit).

```
 ▸ ◆  release-notes     ⋔ 2  ▴ 42m  11m
      Which feature should lead: sea…
```

- Line 1 is the state, name, subagent count, long-run badge and age. The subagent and badge cells go below 36 columns.
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
| Wizard | `n` or `:new` | wizard flow |
| Settings | `S` or `:settings` | `esc` |
| Help | `?` or `:help` | `esc`, `?` |
| Stats | `:stats` | `esc`, `:` |
| Overview | `:overview` | `esc`, `q`, `:overview` |
| Loops | `L` or `:loops` (needs `loops`) | `esc`, `q`, `L`, `:loops` |
| Confirm stop loop | `x` or `:loop stop` | `y` / any other key |
| Confirm quit | `:q` | `y` / `n` |
| Confirm delete | `dd` | `y` / `n` |

### Overlay look

Help, settings, stats, failures, overview and the wizard share one chrome: a square box with its title set into the top border (`┌─ HELP ──── rex v1 ─┐`). The board behind them is dimmed to one muted color so the overlay reads first. The attach popup uses the same square corners but keeps the board's colors around it.

### Help overlay

`?` / `:help` opens the panel (width `clamp(w-8, 60, 110)`, at most `w-2` on a narrow terminal). At 96+ columns it is two columns: Navigation, Filter & search, Actions, Mouse, Modes and the Legend on the left; Session commands, App commands and Attached session on the right. Narrower terminals get a single column with wrapped descriptions. It lists every `:` command, including `:settings` and `:undone`.

- **Scroll.** The body scrolls when it does not fit, under `↑ more` / `↓ more` markers, so no group is ever dropped: `j` `k` (or arrows), `pgup` `pgdn` (or `ctrl+u` `ctrl+d`, `space`), `g` `G`, and the mouse wheel.
- **Filter.** `/` opens an inline filter: only rows whose key or description contains every word stay (`prune` leaves `:prune`). `enter` keeps the filter, `esc` clears it first and a second `esc` closes. The panel shows the query in its first line.
- **Legend.** Drawn with the board's own styles, so it always matches what you see: `◆` needs you, the spinner for working, `◷` waiting (only while `waiting_section` is on), `◌` queued, `●` done, `✕` failed, `○` crashed, `▍` fleet member, `⋔ 3` subagents, and the four long-running badges (`▴ 10m`, `▴ 30m`, `▴ 2h`, `▴ 6h`).
- **Features that are off.** Keys and commands behind a Features switch (`t`, `/`, `esc` for search, `z…`, `uc`, `:filter`, `:search`, `:uc`, `:rid`, `:export`) stay listed, dimmed and tagged `(off)`.

### Settings overlay

`S` / `:settings` lists every setting by section; `rex config` lists the same registry (it also shows the one hidden row, `keybind_preset`, which the page hides until a second preset exists).

- **Search.** `/` filters live over label, id, section and help text (`spin` leaves Spinner type). `enter` keeps the filter, `esc` clears it, a second `esc` closes. While you type, `j` `k` and the rest type; `ctrl+u` clears.
- **Sections.** `tab` / `shift+tab` jump to the next / previous section (they wrap).
- **Modified marker.** A `•` before a value means it differs from the default; `r` resets the row. `on` shows in the accent color.
- **Detail pane.** Under the list, for the selected row: its **id** (what `rex config set <id>` takes), its **default**, **when a change applies** (`applies now`, `next attach`, `next new session`, `after a daemon restart`), the **options** with the current one lit (or the numeric range), and up to four lines of help (two on a terminal under 30 rows). `?` shows the full help text.
- **Editing text.** `enter` on a free-form string (`auto_trust_dirs`, `prompt_glyph`) opens an inline editor in the pane, prefilled with the value: `enter` saves, `esc` cancels, `ctrl+u` clears. A refused value keeps the editor open and shows why. The check is the one `rex config set` and `rex setup` use. `prompt_glyph` must be one printable character (a single grapheme, 1 or 2 cells wide: no control characters, spaces or invisible marks); the arrow keys still cycle the presets.

## KEYS (board focus)

| Key | Action |
|-----|--------|
| `j`, `k`, `↓`, `↑` | Move selection |
| `g`, `G` | First / last row |
| `tab` | Next session that needs you (wraps) |
| `1`, `2`, `3` | Jump to Ready / Waiting / Working |
| `4` | Jump to Done; on a long Done column, show / hide it |
| `v` | Simple / detailed rows |
| `t` | Cycle tool filter (`all`, `claude`, `codex`, `antigravity`, `ollama`) |
| `/` | Search the board (see FILTER AND SEARCH) |
| `esc` | Clear the search |
| `zh` | Hide / show every completed section (Done) |
| `z1`-`z9` | Hide / show section N (`z1` Ready, `z2` Waiting, `z3` Working, `z4` Done) |
| `z0` | Show every section |
| `enter` | Attach to selected session |
| `n` | New-session wizard |
| `i` | Focus bottom prompt (quick-spawn) |
| `r` | Rename selected (display name; empty resets to slug) |
| `cc` | Mark selected done / undo (see MARK DONE) |
| `uc` | Undo done: unmark a running session, or resume an exited one (see MARK DONE) |
| `C` | Complete now: kill the agent and mark it done |
| `dd` | Delete selected (confirm) |
| `:` | Command mode |
| `?` | Help overlay |
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

## NEW-SESSION WIZARD

`n` or `:new` opens a short flow. Steps that do not apply are skipped, in both directions, so the common path is a few Enter presses:

1. **Provider**: the tool (`j`/`k`, `enter`).
2. **Model**: the tool's model. Skipped when the tool has one model. It starts on `default_spawn_model` when the tool is `default_spawn_tool`, else on the first model.
3. **Effort**: the reasoning effort of the chosen model. Skipped when it has none.
4. **Skills**: a checklist of the skills in `~/.config/rex/skills` (`j`/`k` move, `space` toggles, `enter` continues; long lists scroll). Loaded for this session only. Skipped when the library is empty, when the tool cannot load skills per session (Antigravity cannot), and when `spawn_skills` is off in Settings → Features. See [howto/skills.md](howto/skills.md).
5. **Describe**: the task. `enter` launches. The slug comes from the task.

The wizard does not ask for a name; press `r` on the board to give a session a display name, or use `rex new -name`.

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

With sound on, each soundset chimes when a session needs you (`working` → `needs_input`), when it resumes (`needs_input` → `working`), and when it finishes (→ `done`) or you mark it done (`cc`, `:done`, `rex done`). A flickering session chimes at most once every 1.5 s. Turn off the first two with `notify_on_state_change`; see [settings.md](settings.md#audio).

## WORKSPACE BOARD

`rex here` (or `rex ws open [path]`) opens the board fixed to one workspace ([workspaces.md](workspaces.md#sessions-and-the-workspace-board), needs `workspaces`). The filter is not a user filter: no key or command changes it, and the chip row does not mention it. Rows, header counts, the screensaver, the overview and `:prune` only see that workspace's sessions.

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

- The counts stay live: daemon events keep updating them. `ready` is highlighted while it is above 0, and `· N failed` shows in red while a session has failed or crashed. While the daemon is unreachable, the reconnect status shows under the counts.
- Any key or mouse event (click, wheel), or a terminal resize, brings the screen back. That key or click does nothing else, so an `enter`, `q` or `ctrl+c` that wakes the board never opens, quits or deletes anything. A half-typed `dd` or `cc` from before is dropped.
- It starts whatever is open: an overlay, prompt, confirmation, the new-session wizard, the settings page or the attach popup left open still times out. Waking returns to it as you left it (typed text, wizard step, attached session). An attached session's output keeps reaching its terminal meanwhile. Only the boot splash never times out.
- Once a minute it moves a little (at most 6 columns and 2 rows from the center), so a screen left on for hours does not keep one image in the same place. With `reduce_motion` it stays centered.
- It uses no CPU while idle: the spinner stops, and one timer fires at most once a minute. It plays no sound.

Turn it off with `screensaver_enabled` (settings page `S`, or `rex config set screensaver_enabled false`). See [settings.md](settings.md#behavior).

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

## PROMPT

`i` focuses the bottom prompt. Submitting spawns a session using `default_spawn_tool`, `default_spawn_model`, and `default_spawn_effort` from [settings.md](settings.md). The text becomes the agent's first prompt and the session's title (shown in the description column until an AI summary arrives). The new session is selected when it appears. If the daemon rejects the spawn, the error shows in the status line.

## SEE ALSO

- [slash.md](slash.md) — `:` commands
- [cli.md](cli.md) — non-interactive equivalents
- [settings.md](settings.md)
- [paths.md](paths.md) — `tui-state.json`

## BUGS

Help text in `rex --help` may mention selector forms the TUI does not implement. TUI local resolution accepts session id, `slug`, or `short_id` only; a duplicate slug is reported as ambiguous.
