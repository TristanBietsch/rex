# Paths and environment

Canonical list of files and directories Rex uses. See [glossary.md](glossary.md) for terms.

In code, every path comes from one package, `internal/runtime/paths`. Its package doc is the source of truth for this layout; change both together.

## Home directory

Every path below is under the home directory (`$HOME`, `%USERPROFILE%` on Windows). When it is unset, empty or not an absolute path, rex does not guess:

- Commands that need a missing path fail and name the flag to pass instead: `-socket`, `-config` or `-state-dir`.
- `rex-daemon` refuses to start unless `-socket`, `-state-dir` and `-tools` are all given.
- No path is ever taken relative to the working directory, and nothing falls back to `/tmp`.

## Directories

| Path | Contents |
|------|----------|
| `~/.config/rex/` | User configuration |
| `~/.config/rex/loops/<name>.yaml` | Loop workflow files for `rex loop` (see [loops](loops.md)). Read only with `loops` on; must be yours, not a symlink, not group- or world-writable (`rex-daemon -loops-dir` overrides) |
| `~/.local/share/rex/loops/<id>.json`, `<id>.log` | A loop's state and log, owner-only (0600) in a 0700 folder. Exists only with `loops` on; finished loops are deleted after 7 days |
| `~/.config/rex/skills/<name>/` | Skill library: one folder per skill, each with a `SKILL.md` (see [skills](howto/skills.md)) |
| `~/.config/rex/themes/<name>.yaml` | The user's own color themes |
| `~/.local/share/rex/` | Session state (default `-state-dir`) |
| `~/.local/share/rex/sessions/<id>/` | Per-session `meta.json` and `transcript.log` |
| `~/.local/share/rex/sessions/<id>/plugin/` | The skills chosen for that session, copied from the library (removed with the session) |
| `~/.local/share/rex/snapshots/<key>/` | Turn-snapshot store for one working folder, `<key>` = first 16 hex of sha256 of its absolute path. Owner-only (0700). `git/` is the shadow repository, `turns.jsonl` the ledger. Exists only with `turn_snapshots` on (see [snapshots](snapshots.md)) |
| `~/.local/share/rex/exports/` | Files `:export` writes, `<slug>-<shortid>.<ext>`, mode 0600 (see [export.md](export.md)) |
| `~/.local/share/rex/daemon.lock` | Held by the running daemon; one daemon per state directory |
| `~/.local/share/rex/usage.jsonl` | Usage ledger (mode 0600, append-only): see below |
| `~/.local/share/rex/audit.jsonl` | Audit trail of sessions outside any workspace (0600, append-only): see below |
| `~/.local/share/rex/workspaces.json` | Workspace registry: id → folder (0600), see [workspaces.md](workspaces.md) |
| `~/.local/share/rex/workspace-trust.json` | Workspaces whose `.rex/config.lua` runs (`rex ws trust`; 0600), see [workspaces.md](workspaces.md#workspace-settings) |
| `~/.local/state/rex/` | Daemon stderr log, slog files, TUI resume state |
| `~/.cache/rex/` | Socket when `XDG_RUNTIME_DIR` is unset |
| `$XDG_RUNTIME_DIR/rex.sock` | Socket when `XDG_RUNTIME_DIR` is set to an absolute path |
| `~/.local/bin/` | Default install location for `rex` and `rex-daemon` |

`~/.local/share/rex` holds durable session data. `~/.local/state/rex` holds logs and ephemeral TUI state. They are not the same tree.

Only `XDG_RUNTIME_DIR` is read. `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME` and `XDG_CACHE_HOME` are not. The locations above are where every release has kept its files, and honoring those variables would strand existing data.

## Files

| Path | Written by | Purpose |
|------|------------|---------|
| `~/.config/rex/config.yaml` | `rex setup`, TUI settings, `rex config` | User settings (flat keys) |
| `~/.config/rex/tools.yaml` | User | Tool registry overlay |
| `~/.config/rex/init.lua` | User | Optional Lua hooks (daemon only) |
| `~/.config/rex/config.lua` | User, `rex config edit -lua` (template, 0600) | Optional settings and themes in Lua, read by every rex process while `lua_config` is on ([howto/lua-config.md](howto/lua-config.md)). Must be owned by you and not group- or world-writable |
| `~/.config/rex/lua/` | User | Modules `config.lua` may `require` (`a.b` is `lua/a/b.lua`); same ownership rule |
| `~/.local/share/rex/usage.jsonl` | `rex-daemon` (when `usage` is on) | Usage ledger: one JSON line per session snapshot (session id, tool, model, tokens, cost, estimate flag, time), written when a session ends and at most every 10 minutes for a live one. The latest line per session wins when totals are computed. Trimmed to the last 90 days when the daemon starts; unreadable lines are skipped with one warning. No prompts or output. Never created while `usage` is off |
| `~/.local/share/rex/audit.jsonl` | `rex-daemon` (when `audit_trail` is on) | Audit trail: one JSON line per finished session (ids, name, tool, model, first line of the prompt, end state, start and end time, duration, resume id, description), written when a session first ends, and before it is cleared if that has not happened. A resumed session adds a line at its next end. The first prompt line only; the whole prompt with `audit_full_prompt`. 0600, append-only, never followed through a symlink. Never created while `audit_trail` is off |
| `<workspace>/.rex/workspace.json` | `rex ws` | A workspace's id, name and color (0600 in a 0700 folder). Only with `workspaces` on |
| `<workspace>/.rex/sessions.jsonl` | `rex-daemon` (`workspaces` on) | Index of sessions started inside the workspace: a line at spawn and one at the first terminal state (0600, append-only). No prompt text |
| `<workspace>/.rex/audit.jsonl` | `rex-daemon` (`audit_trail` and `workspaces` on) | The audit trail of sessions started inside the workspace; same format and rules as the global file |
| `~/.local/share/rex/workspaces.json` | `rex ws` | Registry: workspace id → the folder it was last seen at, name, color. Lets a moved folder keep its id. Only with `workspaces` on |
| `~/.local/share/rex/workspace-trust.json` | `rex ws trust` | Trusted workspaces (real folder + id + SHA-256 of the trusted `.rex/config.lua`). Read as hostile input: a symlink, another user's or a group/world-writable file trusts nothing. Only with `workspaces` on. `workspace-trust.json.lock` next to it (empty, 0600) is the lock `rex ws trust` and `rex ws trust -rm` hold while they rewrite it, so two at once never lose a change |
| `~/.local/state/rex/tui-state.json` | TUI on `:bg` / detach | Selection, tool filter, hidden sections and board search; read once on next launch, then deleted |
| `~/.local/state/rex/models-cache.json` | `rex-daemon` (when `model_autodetect` is on) | Cache of the model ids each agent listed (`qwen --list-models` and the like): per tool the binary path and mtime, the command, the time, and the ids. Trusted for 24 h and dropped when the binary changes. Owner-only (0600), replaced atomically, safe to delete (the next daemon start or `rex reload` detects again). Holds ids only. Never created while `model_autodetect` is off |
| `~/.local/state/rex/daemon.log` | `rex daemon start` | Daemon stderr |
| `~/.local/state/rex/<name>.log` | `rexlog` | Structured logs (`daemon`, `tui`, `setup`) |
| `~/.local/state/rex/cli-errors.log` (+ `.1`) | `rex` | One JSONL record per failed CLI command: structure only (command, flag names, exit code and kind, error type names; no message or value). 0600, capped at 1 MiB plus one backup; read it with `rex errors`; off with `cli_error_log: false` or `REX_ERROR_LOG=0` |

## Socket

Default Unix domain socket path:

1. `$XDG_RUNTIME_DIR/rex.sock` when `XDG_RUNTIME_DIR` is set to an absolute path. A relative value is ignored, as the XDG spec requires.
2. Otherwise `~/.cache/rex/rex.sock`

Override with `-socket` on `rex-daemon` or `-socket` on CLI commands that dial the daemon. The CLI resolves the default when the command runs, so its help shows the rule rather than a path.

## Environment variables

| Variable | Read by | Effect |
|----------|---------|--------|
| `HOME` (`USERPROFILE` on Windows) | `paths` | Root of every default path; required unless every path flag is given |
| `XDG_RUNTIME_DIR` | `paths` | Default socket directory (absolute paths only) |
| `REX_LOG_LEVEL` | `rexlog` | `debug`, `info`, `warn`, `error` (default `info`) |
| `REX_LOG_DIR` | `rexlog` | Directory for the `<name>.log` files (including `cli-errors.log`) instead of `~/.local/state/rex`. `daemon.log` and `tui-state.json` stay put |
| `REX_QUIET_BOOT` | `rex` | Non-empty (not `0`): open the board without boot sounds, same as `rex --quiet-boot`. For scripted relaunches |
| `OLLAMA_HOST` | `rex-daemon` | Ollama base URL for summarizer. A non-local host receives the text of agents' screens |
| `EDITOR` | `rex config edit`, `e` on a locked settings row | Program to edit `init.lua` or `config.lua` |
| `GOPATH` | `rex update` | Module install path resolution |
| `REX_VERSION` | `rex update`, `install.sh`, `install.ps1` | Release to install (`v2.0.0`) instead of the latest |
| `GITHUB_TOKEN` | `rex update` | Sent to the GitHub API only, for its higher rate limit |
| `SHELL` | `rex setup` | Shell profile target for PATH block |

`REX_SOCKET` is not used for production socket resolution.

## Install

| Method | Binaries |
|--------|----------|
| `sh install.sh` (latest GitHub release, `REX_VERSION` to pin) | `$(PREFIX)/bin/rex`, `rex-daemon` (default `PREFIX=$HOME/.local`) |
| `./install.sh --from-source` (build this checkout) | Same |
| `install.ps1` (Windows, latest release; `-FromSource` builds) | `<Prefix>\bin\rex.exe`, `rex-daemon.exe` (default `~\.local`) |
| `rex update` (release installs) | replaces the running `rex` and the `rex-daemon` next to it |
| `make install PREFIX=…` | Same |
| `go install github.com/tristanbietsch/rex/cmd/rex@latest` | `$GOPATH/bin` or `$HOME/go/bin` |

`install.sh` replaces the binaries but leaves a running daemon alone, so sessions keep going on the old binary. It prints how to switch the daemon to the new one.

## See also

- [settings.md](settings.md) — `config.yaml` keys
- [registry.md](registry.md) — `tools.yaml`
- [daemon.md](daemon.md) — daemon flags
- [cli.md](cli.md) — `-socket` on commands
