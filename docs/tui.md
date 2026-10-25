# TUI reference

The terminal UI starts when you run `rex` with no subcommand. It connects to the daemon, shows a kanban board, and accepts keyboard input.

## NAME

rex — terminal board

## SYNOPSIS

```sh
rex
```

## DESCRIPTION

Three columns group sessions: **NEEDS YOU**, **WORKING**, and **DONE** (includes `done`, `failed`, `crashed`, and every session marked done, whatever its live state). Each title shows its count. The header line reads `N need you · N working · N done`. The bottom prompt (`λ` by default) accepts quick-spawn text, and the help bar lists five keys: `enter open · i new · cc done · tab next · ? more`. Everything else is under `?`.

The tool filter row (`all · claude · codex …`, cycled with `t`) only shows while sessions use more than one tool or a filter other than `all` is active.

A DONE column with more than 5 rows folds into its title line (`DONE  7 hidden · 3 show · :prune clear`). `3` shows and hides it; `:prune` clears it ([slash.md](slash.md)). When a row folds away (for example you mark the selected session done), the selection moves to the first row shown.

The daemon auto-starts if the socket is unreachable (boot splash).

## ROW COLUMNS

By default a row shows four things: state, name, text and age.

```
 ▸ ◆  release-notes   Which feature should lead: search or export?          11m
     state name       text                                                  last event
```

The **text** is the task you gave the session. A session that needs you shows the last line of its screen instead when that line is a question (ends in `?`); otherwise that line is usually the agent's own status text. A session started without a task shows its AI summary.

`v` switches to the detailed view, which adds the short id, model · effort and tokens, shows the AI summary as the text, and adds `· N marked done` to the header:

```
 ▸ ⠋  b203  release-notes   draft ready — which feature leads?   gpt-5-codex · medium   123K tk  ▴45m   11m
     state  id  name        description                          model · effort         tokens   badge  last event
```

- **name**, **model · effort**, and **tokens** are as wide as their widest value on the board (name capped at 22, model at 24). Tokens are right-aligned.
- The **subagent** column (`⋔3`: running subagents) only appears while some Claude Code session is running subagents, in both views. Counts above 99 show as 99.
- The **badge** column only appears while some row has a long-running badge. The **tokens** column only appears while some row has a token count.
- **last event** is right-aligned against the edge; `-` means no event yet.
- The description takes the remaining width. Rows always fill the terminal width exactly and never wrap.
- On narrow terminals, columns give way in this order until the description has 20 columns: tokens are hidden, model shrinks (down to 12), name shrinks (down to 12), done badge is hidden, model is hidden, long-run badge is hidden. After that the description shrinks, then the name.

## FOCUS MODES

| Mode | Enter | Exit |
|------|-------|------|
| Board | default | — |
| Prompt | `i` | `esc` |
| Command | `:` (board) or `::` (inside attach) | `esc` after command |
| Rename | `r` | `enter` saves, `esc` cancels |
| Attach | `enter` on selected row | `qq`, `ctrl+]` or `::q` |
| Wizard | `n` or `:new` | wizard flow |
| Settings | `S` or `:settings` | `esc` |
| Help | `?` or `:help` | `esc`, `?` |
| Stats | `:stats` | `esc`, `:` |
| Confirm quit | `:q` | `y` / `n` |
| Confirm delete | `dd` | `y` / `n` |

### Help overlay

`?` / `:help` opens a square-bordered panel (width `clamp(w-8, 60, 110)`). At 96+ columns it is two columns: Navigation, Actions, Mouse, Modes on the left; Session commands, App commands, Attached session, Long-running badges on the right. Narrower terminals get a single column with wrapped descriptions. If the box would not fit the height, the badges group and then the mouse group are dropped and a dim `(truncated, enlarge terminal)` note appears. It lists every `:` command, including `:settings` (open settings) and `:undone` (undo done, counterpart of `:done`).

## KEYS (board focus)

| Key | Action |
|-----|--------|
| `j`, `k`, `↓`, `↑` | Move selection |
| `g`, `G` | First / last row |
| `tab` | Next session that needs you (wraps) |
| `1`, `2` | Jump to Needs you / Working |
| `3` | Jump to Done; on a long Done column, show / hide it |
| `v` | Simple / detailed rows |
| `t` | Cycle tool filter (`all`, `claude`, `codex`, `antigravity`, `ollama`) |
| `enter` | Attach to selected session |
| `n` | New-session wizard |
| `i` | Focus bottom prompt (quick-spawn) |
| `r` | Rename selected (display name; empty resets to slug) |
| `cc` | Mark selected done / undo (see MARK DONE) |
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

## RENAME

`r` edits the selected session's **name**: free text (64 characters max) shown in the name column instead of the slug. The slug stays the selector for commands and scripts. Enter an empty name to go back to the slug. Control characters, escapes and bidi overrides are stripped; whitespace collapses. Names are stored by the daemon and appear in every connected TUI and in `rex ls`.

## MARK DONE

`cc` marks the selected session done **without stopping it**. The row dims, moves to **DONE** whatever its live state, and shows a **done badge** with the time it was marked (`done 14:32` today, `done Oct 21` otherwise; plain `done` for marks made before the daemon recorded the time). The detailed view's header shows `N marked done`. The notice when marking says when it will clear; the attach footer keeps showing the countdown. The session stays fully usable until the deadline (`retire_after_hours`, default 168 = one week). Then the daemon kills the agent, removes the row, and deletes the session's directory (`meta.json`, `transcript.log`, `hooks.log`). Claude Code's own conversation files are never touched.

- `cc` again undoes the mark.
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
| `▴12m` dim | 10–30 min |
| `▴45m` amber | 30 min – 2 h |
| `▴3h` red | 2–6 h |
| `▴7h` red, inverse | 6 h + |

## DETACH WITHOUT QUITTING

`:bg` or `:detach` saves selection and filter to `tui-state.json` and exits the TUI. The daemon and all sessions continue. The next `rex` restores selection and filter once, then deletes the state file.

## PROMPT

`i` focuses the bottom prompt. Submitting spawns a session using `default_spawn_tool`, `default_spawn_model`, and `default_spawn_effort` from [settings.md](settings.md). The text becomes the agent's first prompt and the session's title (shown in the description column until an AI summary arrives). The new session is selected when it appears. If the daemon rejects the spawn, the error shows in the status line.

## SEE ALSO

- [slash.md](slash.md) — `:` commands
- [cli.md](cli.md) — non-interactive equivalents
- [settings.md](settings.md)
- [paths.md](paths.md) — `tui-state.json`

## BUGS

Help text in `rex --help` may mention selector forms the TUI does not implement. TUI local resolution accepts session id, `slug`, or `short_id` only; a duplicate slug is reported as ambiguous.
