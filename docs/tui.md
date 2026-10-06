# TUI reference

The terminal UI starts when you run `rex` with no subcommand. It connects to the daemon, shows a kanban board, and accepts keyboard input.

## NAME

rex — terminal board

## SYNOPSIS

```sh
rex
```

## DESCRIPTION

Three columns group sessions: **Needs input**, **Working**, and **Completed** (includes `done`, `failed`, `crashed`). A header shows aggregate counts. A tool filter chip cycles with `t`. The bottom prompt (`λ` by default) accepts quick-spawn text.

The daemon auto-starts if the socket is unreachable (boot splash).

## FOCUS MODES

| Mode | Enter | Exit |
|------|-------|------|
| Board | default | — |
| Prompt | `i` | `esc` |
| Command | `:` | `esc` after command |
| Attach | `enter` on selected row | `ctrl+]` |
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
| `c` | Complete selected session |
| `dd` | Delete selected (confirm) |
| `:` | Command mode |
| `?` | Help overlay |
| `S` | Settings |
| `ctrl+c`, `q` | Quit (confirm with `:q`) |

Mouse clicks select rows when `mouse_enabled` is true in settings.

## ATTACH

`enter` opens an in-TUI attach view. Output streams from the daemon; keyboard input goes to the session PTY.

Detach with **Ctrl+]** (ASCII 0x1d). The session keeps running.

`rex attach <sel>` from a shell uses the same detach key.

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

Help text in `rex --help` may mention selector forms the TUI does not implement. TUI local resolution accepts session id, `short_id`, or `slug` only.
