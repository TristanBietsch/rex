# CLI (agents)

Human reference: **[../../cli.md](../../cli.md)** — commands, flags, selectors, exit codes.

## Entry

`cmd/rex/main.go` → `internal/surface/cli/*`

## Command → handler

| Command | Package | Handler |
|---------|---------|---------|
| *(none)* | `meta` | `RunTUI` |
| `status` | `inspect` | `RunStatus` |
| `ls` | `session` | `RunLs` |
| `new` | `session` | `RunNew` |
| `attach` | `session` | `RunAttach` |
| `reply` | `session` | `RunReply` |
| `send` | `session` | `RunSend` |
| `log` | `inspect` | `RunLog` |
| `wait` | `session` | `RunWait` |
| `rm` | `session` | `RunRm` |
| `prune` | `session` | `RunPrune` (selection: `tui.PruneTargets`) |
| `rename` | `session` | `RunRename` |
| `archive` | `session` | `RunArchive` |
| `resume` | `session` | `RunResume` |
| `help`, `--help` | `core` | `RunHelp` (table: `core/commands.go`; per-command: `core/help.go` `showCommandHelp`) |
| `agent-help` | `core` | `RunAgentHelp` (card: `core/agentcard.go`) |
| `errors` | `inspect` | `RunErrors` (log: `core/errlog.go`, written by `core.RecordError` from `main`) |
| `complete` | `meta` | `RunComplete` |
| `reload` | `lifecycle` | `RunReload` |
| `daemon` | `lifecycle` | `RunDaemon` |
| `completion` | `meta` | `RunCompletion` |
| `render` | `inspect` | `RunRender` |
| `config` | `setup` | `RunConfig` |
| `setup` | `setup` | `RunSetup` |
| `doctor` | `setup` | `RunDoctor` |
| `update` | `setup` | `RunUpdate` |
| `uninstall` | `setup` | `RunUninstall` |
| `digest` | `inspect` | `RunDigest` |
| `stats` | `inspect` | `RunStats` |
| `fleet` | `inspect` | `RunFleet` |

## Output formats and agent mode

Every command except `attach` and the bare TUI takes `-o text|txt|md|json|agent`, `--agent` and `--no-color`; `-json` is the legacy shape where a command had one. `txt` is the default in a pipe and keeps each command's historical output. Agent mode (`--agent`, `REX_AGENT=1`, or `CLAUDECODE=1` with a piped stdout) prints `kind key=value` lines and an `ok cmd=<c> …` trailer on stdout and one `error code=N kind=K …` line on stderr. `rex agent-help` prints the card; `rex errors` reads the error log. See [../../cli.md](../../cli.md#output-formats) and [../modules/cli.md](../modules/cli.md).

## Exit codes

Defined in `internal/surface/cli/core/version.go`: `ExitOK` (0) through `ExitWaitTimedOut` (7). `status` exits 1 on `needs_input` without always using `ExitCoder`.

## Selectors

`internal/surface/cli/core/selector.go`: UUID, slug, hex short-id prefix (case-insensitive). A slug that is also another session's short id exits 3.

## Gotchas

- `rex reload` → `Reload` intent over the socket → tools.yaml only (`lifecycle/reload.go`). `rex daemon stop|status|restart` gate on the socket, never on `daemonctl.FindPID` (display only) (`lifecycle/daemon.go`).
- `rex attach` detach: Ctrl+] (`session/attach.go`).
- `config edit` → `init.lua` (`setup/config.go`).
- `ls -short`, `new -no-attach`: flags parsed, no effect.
