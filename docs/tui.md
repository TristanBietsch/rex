# TUI reference

The terminal UI starts when you run `rex` with no subcommand. It connects to the daemon, shows a kanban board, and accepts keyboard input.

## NAME

rex — terminal board

## SYNOPSIS

```sh
rex
```

## DESCRIPTION

Four sections group sessions, top to bottom: **NEEDS YOU**, **WAITING**, **WORKING**, and **DONE** (includes `done`, `failed`, `crashed`, and every session marked done, whatever its live state). Each title shows its count. The header line reads `N need you · N waiting · N working · N done` (the waiting count is left out while it is 0).

**WAITING** holds working sessions that are blocked on a tool, not on you: a shell command, a subagent or a web fetch or search (Claude Code reports it through its tool hooks). A session moves there once the tool call has run for 3 seconds, so quick calls never make a row jump, and back to WORKING when the call ends. A session that needs your input is never in WAITING, and neither is one you marked done. Waiting rows play no sound and send no notification. Its marker is `◷`, which turns `◴ ◷ ◶ ◵` once a second (still `◷` with `reduce_motion`). Other agents have no tool reports yet, so their sessions stay in WORKING. Known limitation: a command that first needed a permission prompt shows as NEEDS YOU while you decide and as WORKING (not WAITING) after you approve it, because Claude reports the call's start before the prompt; the next tool call or turn corrects it. The Done key shown in the fold hint (`4`) is `3` while the section is off. `waiting_section` in Settings → Features turns the section off (they then show in WORKING). The bottom prompt (`λ` by default) accepts quick-spawn text, and the help bar lists five keys: `enter open · i new · cc done · tab next · ? more`. Everything else is under `?`.

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

- **span** is when the session started and ended (`14:02→15:30`; `14:02→` while it is live; `Jan 2` for an earlier day, the year for an earlier year). The end is when the row entered Done: the earlier of the time you marked it done and the time it reached done, failed or crashed. The simple view has no timestamps.
- **resume id** is the first 8 characters of the agent's conversation id (`1a2b3c4d`), the id `claude --resume` takes. It only appears in the detailed view, only while some row has one, and is the first column to go on a narrow terminal. Rows in Done show it in full colour, live rows dim. `:rid [sel]` puts the full id on the status line so you can select it with Option/Shift-drag. The `resume` feature (Settings → Features) switches the column and `:rid` off.
- **name**, **model · effort**, and **tokens** are as wide as their widest value on the board (name capped at 22, model at 24). Tokens are right-aligned.
- The **waiting** detail: in the detailed view a row in WAITING starts its text with the reason and how long it has waited (`shell 42s · Running the test suite`). The attach header chip reads `◷ waiting (shell)`.
- The **subagent** column (`⋔ 3`: running subagents) only appears while some Claude Code session is running subagents, in both views. Counts above 99 show as 99.
- The **badge** column only appears while some row has a long-running badge. The **tokens** column only appears while some row has a token count.
- **last event** is right-aligned against the edge; `-` means no event yet.
- The description takes the remaining width. Rows always fill the terminal width exactly and never wrap.
- On narrow terminals, columns give way in this order until the description has 20 columns: the resume id is hidden, the span is hidden, tokens are hidden, model shrinks (down to 12), name shrinks (down to 12), model is hidden, subagent count is hidden, long-run badge is hidden. After that the description shrinks, then the name.

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
| Confirm quit | `:q` | `y` / `n` |
| Confirm delete | `dd` | `y` / `n` |

### Help overlay

`?` / `:help` opens a square-bordered panel (width `clamp(w-8, 60, 110)`). At 96+ columns it is two columns: Navigation, Filter & search, Actions, Mouse, Modes on the left; Session commands, App commands, Attached session, Long-running badges on the right. Narrower terminals get a single column with wrapped descriptions. If the box would not fit the height, the badges group and then the mouse group are dropped and a dim `(truncated, enlarge terminal)` note appears. It lists every `:` command, including `:settings` (open settings) and `:undone` (undo done, counterpart of `:done`).

## KEYS (board focus)

| Key | Action |
|-----|--------|
| `j`, `k`, `↓`, `↑` | Move selection |
| `g`, `G` | First / last row |
| `tab` | Next session that needs you (wraps) |
| `1`, `2`, `3` | Jump to Needs you / Waiting / Working |
| `4` | Jump to Done; on a long Done column, show / hide it |
| `v` | Simple / detailed rows |
| `t` | Cycle tool filter (`all`, `claude`, `codex`, `antigravity`, `ollama`) |
| `/` | Search the board (see FILTER AND SEARCH) |
| `esc` | Clear the search |
| `zh` | Hide / show every completed section (Done) |
| `z1`-`z9` | Hide / show section N (`z1` Needs you, `z2` Waiting, `z3` Working, `z4` Done) |
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
| `ctrl+c`, `q` | Quit (confirm with `:q`) |

### Mouse

With `mouse_enabled` on (the default) rex captures the mouse once the board is up:

| Action | Effect |
|--------|--------|
| Click a row | Select it |
| Double-click a row | Attach (same as `enter`) |
| Wheel up / down | Move the selection (same as `k` / `j`); the board scrolls with it |
| Click a filter chip | Set that tool filter |

Overlays (help, settings, stats, wizard, failures) and bottom-bar prompts swallow mouse events, so a click never changes the selection behind them; in settings the wheel moves the cursor.

Capturing the mouse turns off the terminal's own text selection. Hold Option (iTerm2/Terminal.app) or Shift (Ghostty/kitty) while dragging to select text natively, or turn `mouse_enabled` off in settings (`S`). The toggle applies immediately.

## ATTACH

`enter` opens an in-TUI attach view. Output streams from the daemon; keyboard input goes to the session PTY.

Detach with `qq` (two quick `q`s), **Ctrl+]** (ASCII 0x1d) or `::q`. The session keeps running. Like `::`, a lone `q` is held for 400 ms and forwarded to the agent if no second `q` follows (or as soon as another key arrives); pasted text is never intercepted. In scroll mode `q` keeps its own meaning (back to live output).

`rex attach <sel>` from a shell uses the same detach key.

### Command bar (`::`)

Typing `:` twice quickly (within 400 ms) inside the popup opens rex's command bar over it instead of sending the colons to the agent. A single `:` is forwarded once the next key arrives or the window passes. Pasted text is never intercepted. `:::` sends a literal `::` (e.g. C++ `std::`).

| Command | Effect |
|---------|--------|
| `q`, `quit`, `detach` | Close the popup (session keeps running) |
| `done` | Mark this session done / undo |
| `kill`, `complete` | Kill the agent now and mark it done |
| `name <text>` | Set this session's display name (`name` alone clears it) |
| anything else | Close the popup and run it as a board `:` command |

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
| `z1`-`z9` | Toggle section N in board order (`z1` Needs you, `z2` Waiting, `z3` Working, `z4` Done) |
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
  » 2 ready for review  billing-tests and api docs are ready for review; deploy asks a question · 40s
```

The count is computed in the TUI from the sessions, so it is true even when the headline is stale or wrong; it reads `nothing to review` at zero. The headline is the daemon's one-line summary of the whole board, cut on a word boundary, with its age. Until the daemon has written one (or against an older daemon, or with the daemon's setting still switching on, which takes about 5 s) only the count shows. The row is reserved while the setting is on, so the board does not jump. Off, the header is exactly as before.

**Buckets.** Every session is in exactly one, decided in code by these rules, in order:

1. Marked done: **Completed**.
2. `failed` or `crashed`: **Failed**.
3. `done`: **Ready for review**. The agent says it finished and you have not marked it.
4. Waiting for input (`needs_input`): **Ready for review** when the agent finished a turn (Claude's Stop hook, `wait_reason: review`) and the last line is not a question (does not end in `?`, the same rule as the row text); otherwise **Needs you**. Claude's idle reminder does not turn a finished turn into a question. Tools without hooks have no `wait_reason`, so a waiting session is always Needs you.
5. `working` or `queued`: **Working**.

**`:overview` overlay.** The headline, wrapped, then the sections READY FOR REVIEW, NEEDS YOU, FAILED, WORKING and COMPLETED (last 24 h) with `marker name status age` rows; empty sections are left out. `j`/`k` (or arrows) select, `g`/`G` jump, `enter` attaches to the selected session, `esc` or `q` closes. It works with the setting off too: you get the lists and a line pointing at the setting.

The headline is written by the local summary model from the sessions' task titles, descriptions and question lines, so it can be wrong and can be steered by what an agent printed. It is one line of display-safe text, never acted on, and the lists next to it never come from it.

---

## NEW-SESSION WIZARD

`n` or `:new` opens a short flow. Steps that do not apply are skipped, in both directions, so the common path is a few Enter presses:

1. **Provider**: the tool (`j`/`k`, `enter`).
2. **Model**: the tool's model. Skipped when the tool has one model. It starts on `default_spawn_model` when the tool is `default_spawn_tool`, else on the first model.
3. **Effort**: the reasoning effort of the chosen model. Skipped when it has none.
4. **Name**: an optional display name (the board shows it instead of the slug; at most 64 characters, checked as you type). `enter` on an empty field skips it; an invalid name shows the reason and stays. With no task, the name also gives the slug.
5. **Skills**: a checklist of the skills in `~/.config/rex/skills` (`j`/`k` move, `space` toggles, `enter` continues; long lists scroll). Loaded for this session only. Skipped when the library is empty, when the tool cannot load skills per session (Antigravity cannot), and when `spawn_skills` is off in Settings → Features. See [howto/skills.md](howto/skills.md).
6. **Describe**: the task. `enter` launches.

`b` goes back (on the text steps only while the field is empty), `esc` cancels. If the `rex-daemon` is older than the name or skills fields, the status line says `this rex-daemon ignored skills; run \`rex daemon restart\``. Quick-spawn (`i`) is unchanged.

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

## SCREENSAVER

After 5 minutes (`screensaver_after_minutes`) with no key or mouse event while the board itself has focus, the board gives way to a small screen: `∴ rex` in the center and the header's counts under it.

```
             ∴ rex

1 need you · 3 working · 7 done
```

- The counts stay live: daemon events keep updating them. `need you` is highlighted while it is above 0, and `· N failed` shows in red while a session has failed or crashed. While the daemon is unreachable, the reconnect status shows under the counts.
- Any key or mouse event (click, wheel) brings the board back. That key or click does nothing else, so an `enter`, `q` or `ctrl+c` that wakes the board never opens, quits or deletes anything. A half-typed `dd` or `cc` from before is dropped.
- It only starts over the bare board. Overlays, prompts, confirmations, the wizard, the settings page and the attach popup never time out, because there you are in the middle of something.
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
