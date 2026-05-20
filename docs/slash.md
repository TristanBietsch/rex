# Slash commands

Command mode opens with `:` on the board. Type a verb and arguments, then press Enter.

## NAME

rex TUI — command palette

## SYNOPSIS

```text
:<verb> [arguments...]
```

## DESCRIPTION

Commands run in the TUI process. Some call the daemon; `:reload` sends SIGHUP to reload `tools.yaml` only.

## COMMANDS

| Command | Arguments | Action |
|---------|-----------|--------|
| `q`, `quit` | — | Confirm quit |
| `q!` | — | Quit without confirm |
| `bg`, `detach` | — | Save TUI state and exit |
| `help` | — | Help overlay |
| `settings` | — | Settings page |
| `reload` | — | SIGHUP daemon (reload tools.yaml) |
| `filter` | `<tool>` | Set filter chip (`all` or tool id) |
| `new` | — | New-session wizard |
| `rm` | `<sel>` | Delete session |
| `rename` | `<sel> <slug>` | Rename slug |
| `fail`, `fails`, `failed` | — | Failed-session inspector |
| `stats` | — | Toggle stats overlay |

`<sel>` resolves against the current board: full session id, `short_id`, or `slug`.

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
