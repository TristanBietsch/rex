# rex-daemon

PTY supervisor and session store. Listens on a Unix domain socket. One child process per session.

## NAME

rex-daemon — Runtime Executive supervisor

## SYNOPSIS

```sh
rex-daemon [-socket path] [-state-dir dir] [-tools path] [-max-concurrent-sessions N]
rex-daemon -version
```

Clients normally start the daemon indirectly (`rex`, boot splash, or `rex daemon start`). Manual start is for debugging.

## DESCRIPTION

The daemon loads the tool registry, restores persisted sessions from disk, accepts JSONL clients on a Unix socket, and runs one PTY supervisor per live session. Session metadata and transcripts persist under `-state-dir`.

On startup, sessions whose `meta.json` still says `queued`, `working`, or `needs_input` are remapped to `crashed` because child processes do not survive daemon exit. After a clean shutdown (below) live sessions were already saved as `failed`, so `crashed` marks a daemon that died or whose sessions did not stop in time.

When an agent exits on its own, the daemon kills whatever is left in its process group (background jobs, MCP servers, tool shells) before reaping it, so nothing is orphaned. On Windows the tree is killed only when rex stops a session; an agent that exits by itself is just reaped.

### Single instance

One daemon runs per state dir. A second `rex-daemon` refuses to start (exit 1, `rex-daemon is already running`) when a daemon already listens on the socket or holds `<state-dir>/daemon.lock`.

### Shutdown

On `SIGINT`, `SIGTERM`, or the `Shutdown` intent the daemon stops accepting clients and closes every client connection, so an open TUI does not hold it up. Each live session's agent and its process tree are killed and the session is saved as `failed`; the daemon waits up to 5 s for that, then exits anyway. It unlinks the socket only if the path is still the one it bound, and releases `daemon.lock`.

### Socket

The socket is created with mode `0600`. Its path must fit the OS limit for a Unix socket path: at most 103 bytes on macOS, 107 on Linux and Windows. A longer `-socket` (or default derived from `XDG_RUNTIME_DIR` / the home directory) fails at startup, before the state dir is touched, with an error that names the limit; `rex daemon start` and the TUI check it before spawning. Choose a shorter path. State and log directories are `0700`, session files and logs `0600`; existing directories are tightened only at the default paths (see [paths.md](paths.md#permissions)).

## OPTIONS

| Flag | Default | Description |
|------|---------|-------------|
| `-socket` | See [paths.md](paths.md) | Unix socket path |
| `-state-dir` | `~/.local/share/rex` | Session persistence root |
| `-tools` | `~/.config/rex/tools.yaml` | Registry overlay; missing file is OK |
| `-max-concurrent-sessions` | `max_concurrent_sessions` from `config.yaml`, else `16` | Live PTY cap. Set explicitly, it wins over `config.yaml`; `0` means uncapped. The `SetMaxConcurrent` intent changes it live, clamped to 1–64 |
| `-version` | — | Print `v1` and exit |

`-version` prints the daemon build label. It is not the wire protocol version. Wire version is `1` in every envelope (`v` field). See [protocol.md](protocol.md).

## SIGNALS

| Signal | Effect |
|--------|--------|
| `SIGINT`, `SIGTERM` | Shut down (see [Shutdown](#shutdown)) |
| `SIGHUP` | Reload `tools.yaml` into the running registry; PTYs unchanged |

`rex reload` and `rex daemon stop` send the `Reload` and `Shutdown` intents over the default socket (see [protocol.md](protocol.md)), so a failed reload is reported back. On macOS and Linux they fall back to `SIGHUP` / `SIGTERM` when nothing answers, the daemon predates the intent, or (for stop) it is still up after 2 s; the signal goes only to this user's process named exactly `rex-daemon` (`pgrep -u <uid> -x`). Windows has no such signals and uses the intents only.

Reload does not re-read `config.yaml` or `init.lua`. Restart the daemon to reload Lua or summarizer settings from config.

## FILES

| Path | Role |
|------|------|
| `<state-dir>/sessions/<id>/meta.json` | Session metadata |
| `<state-dir>/sessions/<id>/transcript.log` | Raw PTY output, capped at 64 MiB: a write that would cross the cap first renames it to `transcript.log.1` (replacing the previous one), so a session keeps at most about 128 MiB |
| `<state-dir>/sessions/<id>/transcript.log.1` | The previous 64 MiB of output; attach replay reads into it after a recent rotation |
| `<state-dir>/daemon.lock` | Single-instance lock |
| Socket path | See [paths.md](paths.md) |
| `~/.local/state/rex/daemon.log` | Stderr when started via `rex daemon start` |

## ENVIRONMENT

| Variable | Effect |
|----------|--------|
| `OLLAMA_HOST` | Summarizer base URL; `http://` added if missing |
| `XDG_RUNTIME_DIR` | Default socket location |

## CLI wrapper

```sh
rex daemon start      # background, -socket <default>; stderr → daemon.log
rex daemon stop
rex daemon status     # exit 4 if not running
rex daemon restart
rex daemon logs       # tail daemon.log; -f to follow
```

## EXIT STATUS

| Code | Meaning |
|------|---------|
| 0 | Clean shutdown |
| 1 | Startup or runtime fatal error |

## EXAMPLES

```sh
rex-daemon -state-dir /tmp/rex-test -socket /tmp/rex-test.sock
```

```sh
rex reload            # Reload intent (SIGHUP fallback) → reload tools.yaml
```

## SEE ALSO

- [protocol.md](protocol.md)
- [registry.md](registry.md)
- [paths.md](paths.md)
- [cli.md](cli.md) — `rex daemon`

## BUGS

Summarizer and Lua load failures at startup are logged; the daemon still serves sessions unless registry load fails entirely.
