# Slash commands

Command mode opens with `:` on the board. Type a verb and arguments, then press Enter.

## NAME

rex TUI — command palette

## SYNOPSIS

```text
:<verb> [arguments...]
```

## DESCRIPTION

Commands run in the TUI process. Some call the daemon; `:reload` sends a `Reload` request over the TUI's socket to reload `tools.yaml` only; a failed request shows in the status line.

## COMMANDS

| Command | Arguments | Action |
|---------|-----------|--------|
| `q`, `quit` | — | Confirm quit |
| `q!` | — | Quit without confirm |
| `bg`, `detach` | — | Save TUI state and exit |
| `help` | — | Help overlay |
| `settings` | — | Settings page |
| `reload` | — | Ask the daemon to reload tools.yaml (over the socket) |
| `filter` | `<tool>` | Set filter chip (`all` or tool id) |
| `new` | — | New-session wizard |
| `rm` | `<sel>` | Delete session |
| `prune` | — | Delete every Completed session except those still working (confirm `y`) |
| `rename` | `<sel> <slug>` | Rename slug |
| `name` | `[text…]` | Set selected session's display name; no text opens the editor |
| `done`, `undone` | `[sel]` | Mark done / undo (default: selected) |
| `complete` | `[sel]` | Kill now and mark done |
| `fail`, `fails`, `failed` | — | Failed-session inspector |
| `stats` | — | Toggle stats overlay |

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

`::` opens a command bar over the attach popup: `q` detaches, `done` toggles mark-done, `kill` completes, `name <text>` renames; other commands close the popup and run as board commands. `:::` types a literal `::`. See [tui.md](tui.md#attach).
