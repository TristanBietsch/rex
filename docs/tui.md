# TUI reference

The terminal UI starts when you run `rex` with no subcommand. It connects to the daemon, shows a kanban board, and accepts keyboard input.

## NAME

rex — terminal board

## SYNOPSIS

```sh
rex
```

## DESCRIPTION

Three columns group sessions: **Needs input**, **Working**, and **Completed** (includes `done`, `failed`, `crashed`, and every session marked done, whatever its live state). A header shows aggregate counts. A tool filter chip cycles with `t`. The bottom prompt (`λ` by default) accepts quick-spawn text.

The daemon auto-starts if the socket is unreachable (boot splash).

## ROW COLUMNS

Each session row, left to right:

```
 ▸ ⠋  b203  release-notes   draft ready — which feature leads?   gpt-5-codex · medium   123K tk  ▴45m   11m
     state  id  name        description                          model · effort         tokens   badge  last event
```

- **name**, **model · effort**, and **tokens** are as wide as their widest value on the board (name capped at 22, model at 24). Tokens are right-aligned.
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
| Attach | `enter` on selected row | `ctrl+]` or `::q` |
| Wizard | `n` or `:new` | wizard flow |
| Settings | `S` or `:settings` | `esc` |
| Help | `?` or `:help` | `esc`, `?` |
| Stats | `:stats` | `esc`, `:` |
| Confirm quit | `:q` | `y` / `n` |
| Confirm delete | `dd` | `y` / `n` |

## KEYS (board focus)

| Key | Action |
|-----|--------|
| `j`, `k`, `↓`, `↑` | Move selection |
| `g`, `G` | First / last row |
| `1`, `2`, `3` | Jump to Needs input / Working / Completed |
| `t` | Cycle tool filter (`all`, `claude`, `codex`, `gemini`, `ollama`) |
| `enter` | Attach to selected session |
| `n` | New-session wizard |
| `i` | Focus bottom prompt (quick-spawn) |
| `r` | Rename selected (display name; empty resets to slug) |
| `c` | Mark selected done / undo (see MARK DONE) |
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

Detach with **Ctrl+]** (ASCII 0x1d) or `::q`. The session keeps running.

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

Line-oriented agents (ollama, echo) are attached with transcript replay, so their scrollback includes output from before the popup opened. Full-screen tools in rex's list (claude, codex, gemini, …) are attached without replay, so for those only lines that scroll off while the popup is open are kept.

## RENAME

`r` edits the selected session's **name**: free text (64 characters max) shown in the name column instead of the slug. The slug stays the selector for commands and scripts. Enter an empty name to go back to the slug. Control characters, escapes and bidi overrides are stripped; whitespace collapses. Names are stored by the daemon and appear in every connected TUI and in `rex ls`.

## MARK DONE

`c` marks the selected session done **without stopping it**. The row dims, moves to **Completed** whatever its live state, and shows a **done badge** with the time it was marked (`done 14:32` today, `done Oct 21` otherwise; plain `done` for marks made before the daemon recorded the time). The header shows `N marked done`. The notice when marking says when it will clear; the attach footer keeps showing the countdown. The session stays fully usable until the deadline (`retire_after_hours`, default 168 = one week). Then the daemon kills the agent, removes the row, and deletes the session's directory (`meta.json`, `transcript.log`, `hooks.log`). Claude Code's own conversation files are never touched.

- `c` again undoes the mark.
- Typing into the session (attach, `rex reply`, `rex send`) cancels the mark: using it again means it isn't done. Just viewing does not.
- A session still **working** at the deadline is not cleared; it is cleared as soon as it settles (the attach footer reads `clears when idle`).
- `c` again moves the row back to the column its live state belongs to.
- The deadline is stored by the daemon, so it survives TUI exit and daemon restarts. A deadline that passed while the daemon was down is cleared on the next start.
- `C` (or `:complete`) is the old immediate behavior: kill now, keep the row as done.

## SOUNDS

With sound on, each soundset chimes when a session needs you (`working` → `needs_input`), when it resumes (`needs_input` → `working`), and when it finishes (→ `done`). A flickering session chimes at most once every 1.5 s. Turn off the first two with `notify_on_state_change`; see [settings.md](settings.md#audio).

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
