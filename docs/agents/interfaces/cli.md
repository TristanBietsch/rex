# CLI (agents)

Human reference: **[../../cli.md](../../cli.md)** — commands, flags, selectors, exit codes.

## Entry

`cmd/rex/main.go` → `internal/surface/cli/*`

## Command → handler

| Command | Package | Handler |
|---------|---------|---------|
| *(none)* | `meta` | `RunTUI` |
| `--help`, `-h`, `help` | `core` | `RunHelp` |
| `--version`, `-v`, `version` | `core` | `RunVersion` |
| `status` | `inspect` | `RunStatus` |
| `ls` | `session` | `RunLs` |
| `new` | `session` | `RunNew` |
| `attach` | `session` | `RunAttach` |
| `reply` | `session` | `RunReply` |
| `send` | `session` | `RunSend` |
| `log` | `inspect` | `RunLog` |
| `wait` | `session` | `RunWait` |
| `rm` | `session` | `RunRm` |
| `rename` | `session` | `RunRename` |
| `name` | `session` | `RunName` |
| `done`, `retire` | `session` | `RunDone` |
| `undone`, `unretire` | `session` | `RunUndone` |
| `archive` | `session` | `RunArchive` |
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

## Exit codes

Defined in `internal/surface/cli/core/version.go`: `ExitOK` (0) through `ExitWaitTimedOut` (7). Handlers return `core.ExitError`; any other error exits 1 (`cmd/rex/main.go` `exitCodeFor`). `status` returns `ExitGeneric` (1) with an empty message on `needs_input`, so nothing is printed.

## Selectors

`internal/surface/cli/core/selector.go`, in order: full UUID, exact slug, `@needs` / `@working` / `@done` (every session in that state), hex short-id prefix (4+ digits). A real slug spelled like an alias wins. More than one match is `ExitAmbiguousSelector` (3), none is `ExitSelectorNotFound` (2). The TUI's `resolveLocal` (`tui/command.go`) is separate: id, slug, exact short id — no aliases, no prefixes.

## Gotchas

- `rex reload` → SIGHUP → tools.yaml only (`lifecycle/reload.go`).
- `rex attach` detach: Ctrl+] (`session/attach.go`).
- `config edit` → `init.lua` (`setup/config.go`).
- `ls -short`, `new -no-attach`: flags parsed, no effect.
- Arguments go through the shared `core.ParseArgs`: flags before or after positionals, `--` ends flags, a positional the command does not take exits 5.
- `core.Version` is a `var` (default `dev`) stamped by `-ldflags "-X github.com/tristanbietsch/rex/internal/surface/cli/core.Version=…"` in `Makefile`, `install.sh` and `install.ps1`; unstamped builds fall back to the module build info.
