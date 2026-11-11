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
| `filter` | `[what]` | No argument: clear every filter (tool, hidden sections and search). `<tool>`: set the tool filter (`all`, a filter chip tool, or a tool id on the board; anything else is an error). `hide <sec…>`, `show <sec…\|all>`, `only <sec…>`: hide or show board sections |
| `search` | `[text…]` | Search the board: a row must contain every word (case-insensitive). No text clears the search. Same as `/`; `:filter` with no argument clears it too |
| `new` | — | New-session wizard |
| `rm` | `<sel>` | Delete session |
| `prune` | — | Delete every DONE session except those still working (confirm `y`; sessions that changed while the prompt was open are kept) |
| `rename` | `<sel> <slug>` | Rename slug |
| `name` | `[text…]` | Set selected session's display name; no text opens the editor |
| `done` | `[sel]` | Mark done, or undo the mark (default: selected) |
| `undone` | `[sel]` | Undo the mark only; an unmarked session gets the notice "is not marked done" |
| `rid` | `[sel]` | Show the full resume id (the agent's conversation id) of the session on the status line. Needs the `resume` feature (Settings → Features); an error when the session has no id |
| `uc`, `resume` | `[sel]` | Undo done (same as the `uc` key): unmark a running session marked done, or resume an exited one in place with its resume id (`resuming <name> (resume id 1a2b3c4d…)`). A session that is not in Done gets the notice "isn't done"; an exited one with no resume id gets an error (`<name> has exited and <tool> can't be resumed`), and so does any exited one while the `resume` feature is off |
| `complete` | `[sel]` | Kill now and mark done |
| `fail`, `fails`, `failed` | — | Failed-session inspector |
| `stats` | — | Toggle stats overlay |
| `overview` | — | Toggle the overview overlay: the headline and the sessions that need you, are ready for review, failed, work or finished (works with `overview_enabled` off; then lists only) |

`<sel>` resolves against the current board: full session id, then `slug`, then `short_id`. A slug shared by several sessions (restored from an older daemon) is reported as `ambiguous: … use short id`. `:rename` to a slug another session holds is rejected by the daemon; the error shows on the status line.

`:filter` needs the `board_filter` feature (Settings → Features); while it is off it answers `off: enable in Settings → Features (rex config set board_filter true)`.

`:search` needs the `search` feature (Settings → Features); while it is off it answers `off: enable in Settings → Features (rex config set search true)`.

A `<sec>` for `:filter` is a section id (`needs`, `working`, `done`), a unique prefix of one, its title (`Needs`), or a 1-based number. `:filter` refuses a change that would hide every section and changes nothing when a section is not recognised. A hidden section is gone from the board (no rows, no navigation); the header counts stay global.

Unknown verbs set the error `unknown command: x (try :help)` on the command line.

## EXAMPLES

```text
:bg
:filter claude
:filter hide done
:filter only needs working
:filter show all
:filter
:search auth callback
:search
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
