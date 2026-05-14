# Slash commands

Slash commands (`/foo`) are the **user-facing action palette** — distinct from `:` system commands. They share the same data-driven, registry-backed pattern as settings: every entry lives in one Go registry, the TUI renders a fuzzy palette from it, and Lua can register new entries at runtime.

This is the documented extension point. v0 ships a small starter set; new commands are added over time without TUI code changes.

## `:` vs `/`

| | `:` | `/` |
| --- | --- | --- |
| Purpose | System / management verbs | User-facing actions, extensible |
| Source | Built-in, fixed | Built-in + Lua-registered |
| UI | Type a literal command at the prompt | Opens a fuzzy palette |
| Examples | `:q`, `:bg`, `:settings`, `:reload`, `:filter` | `/find`, `/new`, `/attach`, `/help`, plus anything you register |

There's deliberate overlap between the two for some actions (e.g. `:new` and `/new` both open the wizard). Use whichever muscle memory you prefer.

## Invocation

- **Keybind: `/`** — opens the slash palette from board focus.
- **Mouse: click the `/` glyph** in the help bar (when present).

The palette is a centered modal:

```
┌──────────────── /                ─────────────────┐
│  /                                                │
│                                                    │
│  ▸ /find                  search slug + description │
│    /new                   open the new-agent wizard  │
│    /help                  open the help overlay      │
│    /attach <sel>          open modal on selector     │
│    /reply <sel> <text>    reply to a session         │
│                                                    │
│  j/k select · enter run · esc close · type to filter │
└────────────────────────────────────────────────────┘
```

Typing fuzzy-filters the list (substring match on `id` and `label`). `enter` runs the highlighted command — if it has positional args, focus moves to an inline argument prompt.

**Fallback behavior.** When the typed text doesn't match any registered command, `enter` runs `/find <typed>` with the input as the search query. That preserves the muscle-memory of "press `/`, type, search" for users coming from vim, while still surfacing the palette.

## Registry shape

```go
// internal/slash/registry.go
type Slash struct {
    ID      string                            // "find", "new", "attach"
    Label   string                            // human label shown in the palette
    Help    string                            // one-line description
    Args    []ArgSpec                         // positional args (name, optional, completion source)
    Action  func(ctx Context, args []string) Result
}

type ArgSpec struct {
    Name        string
    Optional    bool
    Completer   func(prefix string) []string  // tab-completion source
}
```

Built-in entries are listed in `internal/slash/builtins.go`. The registry merges built-ins with Lua-registered commands at startup and on `:reload`.

## v0 starter set

| Command | Args | Effect |
| --- | --- | --- |
| `/find` | `<query>` | Incremental search over slug + description (this replaces the old `/` search keybind). |
| `/new` | — | Open the new-agent wizard (mirrors `n` and `:new`). |
| `/help` | — | Open the help overlay (mirrors `?` and `:help`). |
| `/attach` | `<selector>` | Open the modal on a selector (mirrors `:attach`). |
| `/reply` | `<selector> <text>` | Reply to a session (mirrors `:reply` and `rex reply`). |

These are deliberately a small set. The intent for v0 is to establish the surface; the bulk of `/` commands will accumulate over time, often via user Lua.

## Registering a command from Lua

```lua
-- ~/.config/rex/init.lua

-- Simple no-arg command
rex.slash.register("standup", {
    label = "Print standup digest",
    help  = "List sessions worked on in the last 24h",
    action = function()
        local sessions = rex.sessions.list({ since = "24h" })
        for _, s in ipairs(sessions) do
            rex.notify(string.format("%s · %s", s.slug, s.last_line))
        end
    end,
})

-- Command with positional args
rex.slash.register("blame", {
    label = "Show which agent last touched a file",
    args  = {
        { name = "path", completer = "files" },
    },
    action = function(args)
        local path = args[1]
        local s = rex.sessions.find({ touched = path })
        if s then rex.notify(s.slug .. " · " .. s.short_id)
        else rex.notify("no agent has touched " .. path) end
    end,
})

-- Unregister a built-in (rare, but allowed)
rex.slash.unregister("find")
```

The full Lua surface is documented in `settings.md` under *Lua config*.

## Adding a built-in slash command (recipe)

1. Add a `Slash` entry to `internal/slash/builtins.go`.
2. Implement `Action` — usually a few lines that dispatch to the daemon via an `Intent`.
3. **Done.** The palette picks it up, fuzzy search works, tab-completion works (if `Completer` is set), `rex` CLI gets a matching `rex slash <id>` invocation for free.

## CLI

```sh
rex slash list              # print every registered slash command (table or --json)
rex slash <id> [args...]    # run a slash command non-interactively
```

`rex slash new` is equivalent to opening the TUI wizard from the shell. `rex slash find dark-mode` runs the find command and prints matches. Custom Lua commands work over the CLI too — they execute in a one-shot Lua runtime that connects to the daemon, runs the action, and exits.
