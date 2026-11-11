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

On startup, sessions that were `queued`, `working`, or `needs_input` are remapped to `crashed` because child processes do not survive daemon exit.

A `done`, `failed` or `crashed` session whose tool has a `resume` block and whose `resume_id` is known can be brought back with the `Resume` intent (see [protocol.md](protocol.md#resume)): the daemon reuses the same row, id, slug and transcript and starts the agent with its resume arguments, with folder trust recomputed from the current config. That includes sessions stranded as `crashed` by a daemon restart.

## OPTIONS

| Flag | Default | Description |
|------|---------|-------------|
| `-socket` | See [paths.md](paths.md) | Unix socket path |
| `-state-dir` | `~/.local/share/rex` | Session persistence root |
| `-tools` | `~/.config/rex/tools.yaml` | Registry overlay; missing file is OK |
| `-max-concurrent-sessions` | `16` | Live PTY cap; `0` means uncapped after `SetMaxConcurrent` |
| `-version` | — | Print `v1` and exit |

`-version` prints the daemon build label. It is not the wire protocol version. Wire version is `1` in every envelope (`v` field). See [protocol.md](protocol.md).

## SIGNALS

| Signal | Effect |
|--------|--------|
| `SIGINT`, `SIGTERM` | Shut down; unlink socket |
| `SIGHUP` | Reload `tools.yaml` into the running registry; PTYs unchanged |

`rex reload`, the TUI's `:reload` and `rex daemon stop` do not signal the process. They send `Reload` and `Shutdown` requests over the socket, so they reach exactly the daemon on that socket.

Reload does not re-read `config.yaml` or `init.lua`. Restart the daemon to reload Lua or summarizer settings from config, with one exception: the daemon polls `config.yaml` every 5 s and applies `summary_detail` live.

## FILES

| Path | Role |
|------|------|
| `<state-dir>/sessions/<id>/meta.json` | Session metadata |
| `<state-dir>/sessions/<id>/transcript.log` | Raw PTY output |
| Socket path | See [paths.md](paths.md) |
| `~/.local/state/rex/daemon.log` | Stderr when started via `rex daemon start` |

## ENVIRONMENT

| Variable | Effect |
|----------|--------|
| `OLLAMA_HOST` | Summarizer base URL; `http://` added if missing |
| `XDG_RUNTIME_DIR` | Default socket location |

## CLI wrapper

```sh
rex daemon start      # background; stderr → daemon.log
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
rex reload            # Reload request over the socket: re-read tools.yaml
```

## SEE ALSO

- [protocol.md](protocol.md)
- [registry.md](registry.md)
- [paths.md](paths.md)
- [cli.md](cli.md) — `rex daemon`

## BUGS

Summarizer and Lua load failures at startup are logged; the daemon still serves sessions unless registry load fails entirely.
