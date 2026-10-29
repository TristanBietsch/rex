# Slash commands

Command mode opens with `:` on the board. Type a verb and arguments, then press Enter.

## NAME

rex TUI — command palette

## SYNOPSIS

```text
:<verb> [arguments...]
```

## DESCRIPTION

Commands run in the TUI process. Some call the daemon; `:reload` asks it to re-read `tools.yaml` only.

## COMMANDS

| Command | Arguments | Action |
|---------|-----------|--------|
| `q`, `quit` | — | Confirm quit |
| `q!` | — | Quit without confirm |
| `bg`, `detach` | — | Save TUI state and exit |
| `help` | — | Help overlay |
| `settings` | — | Settings page |
| `reload` | — | Daemon re-reads tools.yaml; a failure (no daemon, invalid tools.yaml) shows on the status line |
| `filter` | `<tool>` | Set filter chip: `all`, `claude`, `codex`, `gemini`, or `ollama` (anything else is an error) |
| `new` | — | New-session wizard |
| `rm` | `<sel>` | Delete session |
| `rename` | `<sel> <slug>` | Rename slug |
| `name` | `[text…]` | Set selected session's display name; no text opens the editor |
| `done`, `undone` | `[sel]` | Mark done / undo (default: selected) |
| `complete` | `[sel]` | Kill now and mark done |
| `fail`, `fails`, `failed` | — | Failed-session inspector |
| `stats` | — | Open stats overlay (`esc` closes it) |

`<sel>` resolves against the current board: full session id, then `slug`, then `short_id`. A slug shared by several sessions (restored from an older daemon) is reported as `ambiguous: … use short id`. `:rename` to a slug another session holds is rejected by the daemon; the error shows on the status line.

Unknown verbs set an error on the command line.

## EXAMPLES

```text
:bg
:filter claude
:rm 7d4f
:rename dark-mode dark-mode-v2
:reload
```

## SEE ALSO

- [tui.md](tui.md)
- [cli.md](cli.md)
- [registry.md](registry.md) — tools.yaml reload

## BUGS

`:reload` does not reload `config.yaml` or `init.lua`. Restart the daemon for those.

## Inside an attached session

`::` opens a command bar over the attach popup. These act on the attached session and keep the popup open (except `q`):

| Command | Action |
|---------|--------|
| `q`, `q!`, `quit`, `detach`, `close` | Close the popup (session keeps running) |
| `done`, `undone` | Mark done / undo |
| `kill`, `complete` | Kill now and mark done |
| `name [text…]` | Set the display name; no text clears it |
| `rename <slug>` | Change the slug, like the board's `:rename` |

Any other command closes the popup and runs as a board command, including `::rename <sel> <slug>`. `:::` types a literal `::`. See [tui.md](tui.md#attach).
