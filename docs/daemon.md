# rex-daemon

PTY supervisor and session store. Listens on a Unix domain socket. One child process per session.

## NAME

rex-daemon — Runtime Executive supervisor

## SYNOPSIS

```sh
rex-daemon [-socket path] [-state-dir dir] [-tools path] [-max-concurrent-sessions N] [-skills-dir dir]
rex-daemon -version
```

Clients normally start the daemon indirectly (`rex`, boot splash, or `rex daemon start`). Manual start is for debugging.

## DESCRIPTION

The daemon loads the tool registry, restores persisted sessions from disk, accepts JSONL clients on a Unix socket, and runs one PTY supervisor per live session. Session metadata and transcripts persist under `-state-dir`.

On a normal (cold) startup, sessions that were `queued`, `working`, or `needs_input` are remapped to `crashed` because child processes do not survive daemon exit. A hot restart (below) keeps them running instead.

A `done`, `failed` or `crashed` session whose tool has a `resume` block and whose `resume_id` is known can be brought back with the `Resume` intent (see [protocol.md](protocol.md#resume)): the daemon reuses the same row, id, slug and transcript and starts the agent with its resume arguments, with folder trust recomputed from the current config. That includes sessions stranded as `crashed` by a daemon restart.

## OPTIONS

| Flag | Default | Description |
|------|---------|-------------|
| `-socket` | See [paths.md](paths.md) | Unix socket path |
| `-state-dir` | `~/.local/share/rex` | Session persistence root |
| `-tools` | `~/.config/rex/tools.yaml` | Registry overlay; missing file is OK |
| `-max-concurrent-sessions` | `16` | Live PTY cap; `0` means uncapped after `SetMaxConcurrent` |
| `-skills-dir` | `~/.config/rex/skills` | Skill library that `NewSession` skill names resolve under (see [skills](howto/skills.md)) |
| `-version` | — | Print `v1` and exit |
| `-handoff` | — | Internal: take over from a hot restart (the handoff file the old image wrote) |
| `-handoff-check` | — | Internal: parse a handoff file and print `ok rex-handoff/1` (the hot-restart preflight) |

`-version` prints the daemon build label. It is not the wire protocol version. Wire version is `1` in every envelope (`v` field). See [protocol.md](protocol.md).

## SIGNALS

| Signal | Effect |
|--------|--------|
| `SIGINT`, `SIGTERM` | Shut down; unlink socket |
| `SIGHUP` | Reload every layer (`tools.yaml`, `config.yaml`, `config.lua`, `init.lua`, the model cache) and log the report; PTYs unchanged |

`rex reload`, the TUI's `:reload` and `rex daemon stop` do not signal the process. They send `Reload` and `Shutdown` requests over the socket, so they reach exactly the daemon on that socket.

`SIGHUP`, `rex reload`, `:reload` and the Settings page's `R` re-read `tools.yaml` (and re-detect model lists, ignoring a fresh cache), then `config.yaml` with `config.lua` on top, then `init.lua`. Every layer is always attempted, so a bad `tools.yaml` does not hold back settings. The `Reload` request answers with a report (`Reloaded` event): the layers read, the setting ids that changed, the changed ones that need a daemon restart, and each failed layer as `file:line: message`. A `config.yaml` that fails to parse keeps the previous values, except `skip_permissions` and `auto_trust_dirs`, which fail closed (off, empty) until the file parses again. An `init.lua` that fails to load keeps the previous handlers; a missing one leaves none. `lua_config_path` itself is read at daemon start.

The daemon holds one copy of `config.yaml` (the settings hub), with `config.lua` on top while `lua_config` is on. It polls `config.yaml`, and `config.lua` plus `~/.config/rex/lua/` while `lua_config` is on, every 5 s and, after an edit or a reload, applies what changed. A `config.lua` that fails keeps its last good values in the daemon, puts `skip_permissions` and `auto_trust_dirs` back to their defaults until it runs again, and logs one flattened line. Settings that apply live (see [settings.md](settings.md#which-settings-apply-live)):

- at once for the running feature: `summary_detail`, `summary_schedule`, `overview_enabled`, `turn_snapshots`, `usage`;
- at the next spawn or `Resume`: `skip_permissions`, `auto_trust_dirs`, `resume`, `spawn_skills`, `usage_statusline_tap`.

`retire_after_hours` (the default for a mark that carries no ttl) also follows the hub. Everything else (`summary_enabled`, `summary_model`, `lua_config_path`) is read once at daemon start; a hot restart applies them without stopping sessions. With the `live_reload` feature off the daemon does not poll at all, and every change above waits for `rex reload`, `:reload` or `SIGHUP`.

## HOT RESTART

`rex daemon restart` (and `rex upgrade`) restart the daemon **in place**: it re-execs its own executable (the path it was started from, so a newly installed binary is picked up) under the same PID, and every running agent keeps its process, its PTY and its output. Sequence:

1. Refused, with nothing changed, on Windows, while another restart runs, or while a session is still starting.
2. Every session is frozen (3 s budget): input stops, the PTY writer finishes what it is writing, the reader parks between reads (it waits for readability 50 ms at a time and never sits in a blocking read), and the screen is captured. Bytes the agent writes meanwhile stay in the kernel's PTY buffer.
3. `meta.json` is written for each session, then a handoff file `<state-dir>/handoff-<pid>.json` (0600, created exclusively, format `rex-handoff/1`) with the listener, lock and PTY fds, child pids, sizes and screens.
4. Preflight: the new binary runs `rex-daemon -handoff-check <file>` (10 s) and must print `ok rex-handoff/1`. A missing binary, an older build without the flag, or one that cannot read the format is refused: the sessions thaw and the old daemon serves on.
5. Only the listener, lock and PTY master fds are made inheritable, client connections are closed, and the daemon calls `execve`. If `execve` fails, the same rollback as 4.

The new image (`-handoff <file>`) accepts the file only when it is a regular file (not a symlink) owned by its uid with no group/other bits, names its own PID and a known format; it deletes it after reading. It adopts the socket and lock without a gap (new connections wait in the listen backlog), restores each session's screen into a fresh emulator, re-reads the Claude hook file from the start, and reaps a child that exited during the exec through the normal exit path. Boards reconnect on their own; an attach popup re-attaches to the same session.

There is no rollback after a successful `execve`: if the new image dies, the PTY masters close and the agents get `SIGHUP`. The preflight is what keeps that unlikely. Only a same-user client on the 0600 socket can ask for a restart, and the daemon only ever execs its own executable. Snapshots carry `daemon_pid`, `daemon_build` (first 12 hex of the executable's SHA-256) and `daemon_started_at`, so clients can tell a new image at the same PID.

A daemon from before hot restart answers `bad_intent`: the first upgrade from it has to be cold (`rex daemon restart -cold`), which kills running agents (those with a resume id can be brought back with `rex resume`).

**When descriptions refresh.** With `summary_enabled` on, the daemon writes each session's one-line description with the local model. With `summary_schedule` on (the default), a live session (queued, working or needs you) is refreshed 5, 10, 15, 20 and 25 minutes after its run started, then every 30 minutes; a Resume (T3) restarts that count. Refreshes from the 6th on are one detail level longer and say what is finished plus what is happening now. A change of state (for example working to needs you) refreshes at once and leaves the clock where it was; finished sessions get one final summary and no scheduled refreshes. Screen activity does not trigger a refresh. A tick that finds the screen unchanged makes no call but still counts; ticks that come due while the model is busy or Ollama is down merge into one refresh per session, run when the model is free. With `summary_schedule` off, a description is refreshed when the screen has been quiet for 500 ms (or every 15 s of continuous output) and on state changes. Logs name the session and tick number, never the screen text.

With `overview_enabled` on, the daemon also writes a one-line overview of the board with the summary model. It runs 3 s after a session is added, removed, changes state, is renamed or is marked, at most once every 30 s, and a changed description alone refreshes it at most every 2 minutes. It skips the call when nothing changed, when only completed sessions are left, and while no `rex` board is connected (other clients such as `rex ls` do not count). The overview lives in memory only: it is not written to disk, and its text is never logged.

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
rex daemon restart    # hot: sessions keep running; -cold to stop and start
rex daemon logs       # tail daemon.log; -f to follow
rex upgrade           # hot-restart the daemon, then swap every board
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
rex reload            # Reload request over the socket: every layer, with a report
```

## SEE ALSO

- [protocol.md](protocol.md)
- [registry.md](registry.md)
- [paths.md](paths.md)
- [cli.md](cli.md) — `rex daemon`

## BUGS

Summarizer and Lua load failures at startup are logged; the daemon still serves sessions unless registry load fails entirely.
