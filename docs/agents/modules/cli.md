# cli

**Path:** `internal/surface/cli`
**Depends on:** `internal/wire/client`, `internal/wire/protocol`, `internal/runtime/daemonctl`, `internal/catalog/settings`, `internal/daemon/state`, `internal/runtime/rexlog`, `internal/surface/tui` | external: `github.com/charmbracelet/lipgloss`, `golang.org/x/term`
**Depended on by:** `cmd/rex`
**Entry points:** all `Run*` functions, `DefaultSocket`, selector helpers, exit constants

## Purpose

Implements every `rex` subcommand: daemon control, session CRUD, scripting output, setup/doctor/update, and TUI entry.

## Public surface

### Constants (exit codes)

`ExitOK`, `ExitGeneric`, `ExitSelectorNotFound`, `ExitAmbiguousSelector`, `ExitDaemonUnreachable`, `ExitInvalidArgs`, `ExitOperationRefused`, `ExitWaitTimedOut`

### Types

- `ExitCoder` interface — `ExitCode() int`
- `ExitError` — `NewExitError(code int, err error)`

### Functions (commands)

`RunTUI`, `RunHelp`, `RunVersion`, `RunStatus`, `RunLs`, `RunNew`, `RunAttach`, `RunReply`, `RunSend`, `RunLog`, `RunWait`, `RunRm`, `RunRename`, `RunArchive`, `RunComplete`, `RunReload`, `RunDaemon`, `RunCompletion`, `RunRender`, `RunConfig`, `RunSetup`, `RunDoctor`, `RunUpdate`, `RunUninstall`, `RunDigest`, `RunStats`, `RunFleet`

### Utilities

`DefaultSocket()`, `ResolveSelector`, `ResolveInSnapshot`, `WriteSessionsTable`, `WriteSessionsJSONL`, `WriteAggregateLine`, `WriteAggregateJSON`

## Internal structure

One file per command area: `new.go`, `attach.go`, `ls.go`, `status.go`, `daemon.go`, `config.go`, `setup.go`, `doctor.go`, `fleet.go`, `digest.go`, `stats.go`, `selector.go`, `output.go`, `socket.go` (forwards to `daemonctl`), `help.go`, `tui.go`, etc. `*_test.go` for digest, doctor, fleet, output, selector, stats, attach, complete, uninstall.

## Invariants

Most commands dial `DefaultSocket()`. Selectors: UUID, slug, short-id prefix only. Human reference: [../../cli.md](../../cli.md).

`RunStatus` exits `1` when any session is `needs_input` (for shell prompts).

## Side effects

Network (UDS), filesystem (log, config, uninstall), subprocess (`rex-daemon`, `go install` in update), TUI (bubbletea) via `RunTUI`/`RunSetup`.

## Error handling

Returns `ExitError` with specific codes where documented. Other errors → exit `1` from `cmd/rex/main.go`.

## Tests

See inventory in Pass 1: attach indexing, complete validation, digest ranges, doctor JSON, fleet routing, output tables, selector snapshot, stats aggregation, uninstall shell block.

## Gotchas

`RunNew` default `--no-attach true` (spawn and exit). `DefaultSocket` in `socket.go` delegates to `daemonctl`. Per-command flags are local `flag.FlagSet`s — see `interfaces/cli.md`. `RunRender` builds headless `tui.Model` for screenshots.

## See also

- `interfaces/cli.md` — flags per command.
- `modules/tui.md`
- `modules/client.md`
- Human reference: `docs/cli.md`
