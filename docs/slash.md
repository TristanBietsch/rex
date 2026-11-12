# Slash commands

Command mode opens with `:` on the board. Type a verb and arguments, then press Enter.

## NAME

rex TUI — command palette

## SYNOPSIS

```text
:<verb> [arguments...]
```

## DESCRIPTION

Commands run in the TUI process. Some call the daemon over the TUI's socket; a failed request shows in the status line. The help overlay (`?` or `:help`) lists the same commands. A command gated by a feature switch (Settings → Features) answers `off: enable in Settings → Features (rex config set <feature> true)` while the switch is off.

## COMMANDS

| Command | Arguments | Action |
|---------|-----------|--------|
| `q`, `quit` | — | Confirm quit |
| `q!` | — | Quit without confirm |
| `bg`, `detach` | — | Save TUI state and exit |
| `help` | — | Help overlay |
| `settings` | — | Settings page |
| `reload` | — | Reload now, whatever `live_reload` says: sends a `Reload` request to the daemon (it re-reads `tools.yaml`, then `config.yaml`), and the TUI re-reads `config.yaml` and the theme files itself and applies what changed. A `config.yaml` that fails to parse keeps the previous values and says so on the status line |
| `theme` | `[name]` | Set the theme: a built-in, `auto`, or one of your theme files. It applies at once and is saved to `config.yaml`. No name: the next theme in the list. An unknown name is an error that lists the names. See [themes.md](themes.md) |
| `border` | `[on\|off\|next\|random\|<color>\|#rrggbb]` | The window border for this run only (never saved; `window_border` and `window_border_color` are the lasting choice). No argument toggles it; `next` steps to the next color; `random` picks one per project folder; a color name or `#rrggbb` sets it. The status line says what is shown, and why it is hidden on a terminal too small for it |
| `trust` | `[folder]` | Save a folder to `auto_trust_dirs`, so new sessions there skip the agent's "trust this folder" dialog (sessions already running keep asking). No folder: the repo folder of the selected session's folder, or of the folder rex runs in. The home folder, a folder that holds it, the root, relative paths and paths with `..` are refused with the reason. The status line shows the exact folder saved, or `already trusted`. See [settings.md](settings.md) |
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
| `uc`, `resume` | `[sel]` | Needs the `undo_complete` feature. Undo done (same as the `uc` key): unmark a running session marked done, or resume an exited one in place with its resume id (`resuming <name> (resume id 1a2b3c4d…)`). A session that is not in Done gets the notice "isn't done"; an exited one with no resume id gets an error (`<name> has exited and <tool> can't be resumed`), and so does any exited one while the `resume` feature is off |
| `export` | `[sel] [json\|yaml\|md\|txt]` | Write the session (default: selected) as a readable document to `~/.local/share/rex/exports/<slug>-<shortid>.<ext>` (mode 0600) and show the path. One argument that names a format is the format. Needs the `session_export` feature (Settings → Features). See [export.md](export.md) |
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
:theme paper
:theme auto
:theme
:border next
:border off
:trust
:trust ~/dev/project
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
- [themes.md](themes.md) — `:theme`
- [settings.md](settings.md) — `auto_trust_dirs`, `window_border`

## BUGS

`:reload` does not reload `init.lua`. Restart the daemon for that.

## Inside an attached session

`::` opens a command bar over the attach popup: `q` (or `q!`, `quit`, `detach`, `close`) detaches, `done` toggles mark-done, `undone` undoes the mark only, `complete` (or `kill`) completes, `name <text>` (or `rename`) renames, `banner` hides or shows the banner row for this run, `border [...]` works as `:border`; other commands close the popup and run as board commands. `:::` types a literal `::`. See [tui.md](tui.md#attach).
