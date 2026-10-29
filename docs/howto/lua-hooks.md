# How to use Lua hooks

Prerequisite: [quickstart.md](../quickstart.md).

## Goal

Run a script when sessions change and send text to a session PTY.

## Steps

1. Create `~/.config/rex/init.lua`:

```lua
rex.on("session_added", function(s)
  rex.log("info", "added " .. s.slug)
end)

rex.on("session_updated", function(s)
  if s.state == "needs_input" then
    rex.log("info", s.slug .. " needs input")
  end
end)
```

2. Restart the daemon so it loads the file:

```sh
rex daemon restart
```

`rex reload` reloads `tools.yaml` only. It does not reload Lua.

3. Spawn a session and watch the daemon log:

```sh
rex daemon logs -f
```

## API

| Function | Description |
|----------|-------------|
| `rex.on(event, fn)` | Register handler |
| `rex.send(session_id, text)` | Write to session PTY |
| `rex.list()` | Table of current sessions |
| `rex.log(level, msg)` | Daemon slog |

Events: `session_added`, `session_updated`, `session_removed`.

Handlers run on a goroutine of their own, never inside the daemon's event broadcast, so a slow script cannot stall sessions or clients. Events wait in a queue of 256; while it is full, new events are dropped and logged. Loading the file and each handler call are cut off after 5 s. `rex.send` gives up after 2 s if the session's input buffer stays full, and never lands between a reply's text and its Enter.

If the file fails to load, the error is logged and handlers registered before the error stay active; fix the script and run `rex daemon restart`.

The script runs in-process as the daemon user. Load only trusted code.

## See also

- [internal/features/lua/README.md](../../internal/features/lua/README.md) — threading and security
- [settings.md](../settings.md) — `lua_config_path`
