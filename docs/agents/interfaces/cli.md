# CLI

`rex` binary: `cmd/rex/main.go` routes to `internal/surface/cli`. No global flags except when first argument is help/version.

## rex

### NAME

rex — Runtime Executive for Agents

### SYNOPSIS

```sh
rex
rex [--help|-h|help]
rex [--version|-v|version]
rex <command> [flags] [args...]
```

### DESCRIPTION

With no arguments, starts the Bubble Tea TUI (`cli.RunTUI`). With a command, runs the matching `Run*` handler.

### OPTIONS (top-level)

| Option | Description |
|--------|-------------|
| `-h`, `--help`, `help` | Print help (`RunHelp`) |
| `-v`, `--version`, `version` | Print version (`RunVersion`) |

### COMMANDS

| Command | Handler | Summary |
|---------|---------|---------|
| `status` | `RunStatus` | Aggregate session counts; exit 1 if any `needs_input` |
| `ls` | `RunLs` | List sessions |
| `new` | `RunNew` | Spawn session |
| `attach` | `RunAttach` | Attach terminal to session PTY |
| `reply` | `RunReply` | Send newline-terminated reply |
| `send` | `RunSend` | Send raw bytes |
| `log` | `RunLog` | Read transcript file |
| `wait` | `RunWait` | Block until state change |
| `rm` | `RunRm` | Delete session |
| `rename` | `RunRename` | Change slug |
| `archive` | `RunArchive` | Archive completed session |
| `complete` | `RunComplete` | Terminate and mark done |
| `reload` | `RunReload` | SIGHUP daemon (reload tools.yaml) |
| `daemon` | `RunDaemon` | Daemon lifecycle |
| `completion` | `RunCompletion` | Shell completion script |
| `render` | `RunRender` | Print static board view |
| `config` | `RunConfig` | User settings YAML |
| `setup` | `RunSetup` | First-run wizard |
| `doctor` | `RunDoctor` | Diagnostics |
| `update` | `RunUpdate` | Upgrade binaries |
| `uninstall` | `RunUninstall` | Remove install |
| `digest` | `RunDigest` | Daily summary |
| `stats` | `RunStats` | Lifetime usage stats |
| `fleet` | `RunFleet` | Fleet labels |

### SELECTORS

Session arguments accept short id, slug, or group aliases `@needs`, `@working`, `@done` (see `RunHelp`).

### EXIT STATUS

| Code | Constant | Meaning |
|------|----------|---------|
| 0 | `ExitOK` | Success |
| 1 | `ExitGeneric` | General error |
| 2 | `ExitSelectorNotFound` | No matching session |
| 3 | `ExitAmbiguousSelector` | Multiple matches |
| 4 | `ExitDaemonUnreachable` | Socket/daemon unavailable |
| 5 | `ExitInvalidArgs` | Usage error |
| 6 | `ExitOperationRefused` | Daemon refused intent |
| 7 | `ExitWaitTimedOut` | `wait` timed out |

`status` also exits 1 when sessions need input (without `ExitCoder`).

---

## rex ls

**Flags:** `-socket`, `-state`, `-tool`, `-model`, `-show-archived`, `-short`, `-json`

## rex new

**Flags:** `-socket`, `-tool` (default `echo`), `-model` (default `short`), `-effort`, `-slug`, `-cwd`, `-fleet`, `-no-attach` (default `true`)

**Args:** `[prompt]` positional initial prompt.

## rex attach

**Flags:** `-socket`, `-read-only`, `-no-replay`

**Args:** `<selector>`

Detach: Ctrl+].

## rex reply

**Flags:** `-socket`, `-raw` (no trailing newline)

**Args:** `<selector> <text>`

## rex send

**Flags:** `-socket`

**Args:** `<selector>` — stdin bytes forwarded.

## rex log

**Flags:** `-socket`, `-state-dir`, `-f`, `-n`, `-bytes`

## rex wait

**Flags:** `-socket`, `-until` (default `done`; values: `working`, `needs_input`, `done`, `failed`, `any`)

## rex rm

**Flags:** `-socket`, `-force`

## rex rename

**Flags:** `-socket`

**Args:** `<selector> <slug>`

## rex archive / complete

**Flags:** `-socket`

**Args:** `<selector>`

## rex status

**Flags:** `-socket`, `-json`

## rex render

**Flags:** `-socket`, `-w` (default `120`), `-h` (default `30`)

## rex config

Subcommands: `list`, `get`, `set`, `reset`, `edit`

- `list`: `-json`
- `get` / `set` / `reset`: setting id arguments (see `settings.Registry`)
- `edit`: uses `$EDITOR`

## rex setup

**Flags:** `-preview`, `-yes`/`-y`, `-tool`, `-model`, `-effort`, `-no-shell`, `-config`, `-socket`, `-force`

## rex doctor

**Flags:** `-json`, `-socket`, `-verbose`

## rex update

**Flags:** `-check`, `-yes`/`-y`, `-source`, `-verbose`

## rex digest

**Flags:** `-socket`, `-date`, `-since`, `-json`

## rex stats

**Flags:** `-socket`, `-json`, `-all`

## rex fleet

**Flags:** `-socket`

**Subcommands:** `ls`, `set <selector> <fleet>`, `unset <selector>`, `show <fleet>`

## rex daemon

**Subcommands:** `start`, `stop`, `status`, `restart`, `logs`

- `logs`: `-f` follow
- Log file path: `daemonLogPath()` in `daemon.go` (under state dir — see source)

## rex reload

Sends SIGHUP to running `rex-daemon` process.

## rex-daemon

Separate binary. See `modules/cmd.md`.

**Flags:** `-socket`, `-state-dir`, `-tools`, `-max-concurrent-sessions`, `-version`

### EXAMPLES

```sh
rex
rex ls --json
rex new --tool claude --model opus --slug my-task "fix the bug"
rex attach 7d4f
rex status
rex config set sound_enabled false
```

### EXIT STATUS

`rex-daemon` exits 1 on startup failure; 0 on clean shutdown.
