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
- `ExitError` — `NewExitError(code int, msg string)`

### Functions (commands)

`RunTUI`, `RunHelp`, `RunVersion`, `RunStatus`, `RunLs`, `RunNew`, `RunAttach`, `RunReply`, `RunSend`, `RunLog`, `RunWait`, `RunRm`, `RunRename`, `RunArchive`, `RunComplete`, `RunReload`, `RunDaemon`, `RunCompletion`, `RunRender`, `RunConfig`, `RunSetup`, `RunDoctor`, `RunUpdate`, `RunUninstall`, `RunDigest`, `RunStats`, `RunFleet`

### Utilities (`core`)

- `ParseArgs(fs, args)` — parses flags anywhere among the positionals (`--` ends flags) and returns the positionals; flag errors are `ExitInvalidArgs`.
- `CheckArgs(pos, min, max, usage)` — enforces positional arity (`max < 0`: unbounded); extra or missing positionals are `ExitInvalidArgs`.
- `Dial(socket)` — dial plus `Hello`; returns the client and snapshot. Failures are `ExitDaemonUnreachable`.
- `Connect(socket, sel)` — `Dial` plus `ResolveInSnapshot`; selector errors keep their own codes.
- `AwaitPatch(c, sessionID, reqID, patchKey)` — waits up to 3 s for a `SessionUpdated` carrying `patchKey` (success) or an `Error` echoing `reqID` (`bad_intent` → `ExitInvalidArgs`, else `ExitGeneric`). Used by `rename`, `name`, `done`/`undone`, `archive`, `fleet set`/`unset`.
- `DefaultSocket()`, `ResolveInSnapshot`, `WriteSessionsTable`, `WriteSessionsJSONL`, `WriteAggregateLine`, `WriteAggregateJSON`, `Version`.

## Internal structure

Subpackages of `internal/surface/cli`: `core` (exit codes, `ParseArgs`/`CheckArgs`, `Dial`/`Connect`/`AwaitPatch`, selectors, output, help, version), `session` (`ls`, `new`, `attach`, `reply`, `send`, `wait`, `rm`, `rename`, `name`, `done`, `archive`), `inspect` (`status`, `log`, `render`, `digest`, `stats`, `fleet`), `lifecycle` (`daemon`, `reload`), `meta` (TUI entry, `complete`, `completion`), `setup` (`config`, `setup`, `doctor`, `update`, `uninstall`). Commands parse with `core.ParseArgs`, check arity with `core.CheckArgs` (exceptions: `new` joins any number of prompt words; `config get`/`set`/`reset` check `len(args)` themselves), and reach the daemon through `core.Dial` or `core.Connect`.

## Invariants

Most commands dial `DefaultSocket()`. Selectors: UUID, slug, `@needs` / `@working` / `@done`, short-id prefix (`core/selector.go`). Human reference: [../../cli.md](../../cli.md).

`RunStatus` exits `1` when any session is `needs_input` (for shell prompts).

## Side effects

Network (UDS), filesystem (log, config, uninstall), subprocess (`rex-daemon`, `go install` in update), TUI (bubbletea) via `RunTUI`/`RunSetup`.

## Error handling

Returns `ExitError` with specific codes where documented. `cmd/rex/main.go` finds it with `errors.As`, so wrapping keeps the code; other errors → exit `1`.

## Tests

See inventory in Pass 1: attach indexing, complete validation, digest ranges, doctor JSON, fleet routing, output tables, selector snapshot, stats aggregation, uninstall shell block.

## Gotchas

`RunNew` spawns and exits; it never attaches. `DefaultSocket` in `socket.go` delegates to `daemonctl`. Per-command flags are local `flag.FlagSet`s — see `interfaces/cli.md`. `RunRender` builds headless `tui.Model` for screenshots.

## See also

- `interfaces/cli.md` — flags per command.
- `modules/tui.md`
- `modules/client.md`
- Human reference: `docs/cli.md`
