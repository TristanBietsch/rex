# cli

**Path:** `internal/surface/cli`
**Depends on:** `internal/wire/client`, `internal/wire/protocol`, `internal/runtime/daemonctl`, `internal/catalog/settings`, `internal/daemon/state`, `internal/runtime/rexlog`, `internal/surface/tui` | external: `github.com/charmbracelet/lipgloss`, `golang.org/x/term`
**Depended on by:** `cmd/rex`
**Entry points:** all `Run*` functions, `DefaultSocket`, selector helpers, exit constants

## Purpose

Implements every `rex` subcommand: daemon control, session CRUD, scripting output, setup/doctor/update, and TUI entry.

## Output kit (`core`)

Every command renders through one kit; see [../../cli.md](../../cli.md#output-formats) for what users see. The contract for a command:

1. Parse with `core.ParseArgs` (or the `out.Parse` / `out.ParseText` of `core.AddOutputFlags(fs, core.AllFormats)`, plus `core.AddSocketFlag`). Pick the format set for the class: `AllFormats` (query, action), `ContentFormats` (log, render, completion), `TextFormats` or `SetOf(FormatText)` with `core.WithHint(...)` (interactive).
2. Never call `fmt.Print*` or touch `os.Stdout`/`os.Stderr`, and never write a hex color: `TestNoDirectStdout` and `TestNoHardcodedHex` (core/lint_test.go) fail the build. Write through `core.Out()`.
3. Return `core.Out().Emit(core.Result{Cmd, Data, View})`. `Data` is a typed struct with snake_case JSON tags (sessions are `protocol.SessionSummary`); `View` uses only kit blocks (Header, Section, KV, Table, Checks, Note, Hint, Line, Para, Defs). The agent line comes from `Table.Kind`/`KV.Kind` and the column keys. Actions use `core.ActionView(line, fields...)`; `Line.Silent` keeps a command quiet in `txt`; `Result.Legacy` keeps a pre-versioned `-json` shape; `Result.Failed` makes the agent trailer `fail`; `Result.Content` is a verbatim body.
4. Errors: `core.Errorf(code, hint, format, ...)`. `main` renders them per format and records them (`core.RecordError`). Never print an error yourself; warnings go through `Printer.Warn`, progress through `Printer.Progress`.
5. Questions go through `Printer.Confirm(question, core.Refusal{...})` (stderr, text only, exit 6 otherwise).
6. Add a golden archive `testdata/golden/<cmd>.txtar` covering every format (`internal/testutil/golden`, `-update`), a row in `core/commands.go` (it feeds `rex --help`, `rex <cmd> -h`, the agent card and the completion scripts), and a case in `cmd/rex/main.go`.
7. Doctor rows keep returning `checkResult`; `doctorView` turns them into one Checks block.

Files: `format.go` (formats, precedence), `printer.go` (the per-stream Printer, flags, `Confirm`/`Warn`), `view.go` (blocks), `render_*.go` (one renderer per format), `agentline.go` (`EncodeLine`), `styles.go` (palette from `catalog/theme`), `errors.go` (`Errorf`, `Describe`, `RenderError`), `errlog.go` (the error log), `help.go`, `commands.go`, `agentcard.go`.

## Public surface

### Constants (exit codes)

`ExitOK`, `ExitGeneric`, `ExitSelectorNotFound`, `ExitAmbiguousSelector`, `ExitDaemonUnreachable`, `ExitInvalidArgs`, `ExitOperationRefused`, `ExitWaitTimedOut`

### Types

- `ExitCoder` interface — `ExitCode() int`
- `ExitError` — `NewExitError(code int, err error)`

### Functions (commands)

`RunTUI`, `RunHelp`, `RunAgentHelp`, `RunErrors`, `RunResume`, `RunVersion`, `RunStatus`, `RunLs`, `RunNew`, `RunAttach`, `RunReply`, `RunSend`, `RunLog`, `RunWait`, `RunRm`, `RunRename`, `RunArchive`, `RunComplete`, `RunReload`, `RunDaemon`, `RunCompletion`, `RunRender`, `RunConfig`, `RunSetup`, `RunDoctor`, `RunUpdate`, `RunUninstall`, `RunDigest`, `RunStats`, `RunFleet`

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
