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
| `rename` | `session` | `RunRename` |
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

Defined in `internal/surface/cli/core/version.go`: `ExitOK` (0) through `ExitWaitTimedOut` (7). `status` exits 1 on `needs_input` without always using `ExitCoder`.

## Selectors

`internal/surface/cli/core/selector.go`: UUID, slug, hex short-id prefix. **`@needs` / `@working` / `@done` in `RunHelp` are not implemented.**

## Gotchas

- `rex reload` → SIGHUP → tools.yaml only (`lifecycle/reload.go`).
- `rex attach` detach: Ctrl+] (`session/attach.go`).
- `config edit` → `init.lua` (`setup/config.go`).
- `ls -short`, `new -no-attach`: flags parsed, no effect.
