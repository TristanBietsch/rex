# cmd

**Path:** `cmd/rex`, `cmd/rex-daemon`
**Depends on:** see per-binary | external: standard library
**Depended on by:** (binaries; not imported)
**Entry points:** `main` in each package

## Purpose

Two binaries: `rex` (CLI + default TUI) and `rex-daemon` (PTY supervisor and UDS server). They communicate over newline-delimited JSON on a Unix socket.

## Public surface

### `cmd/rex`

- `main()` → `run(os.Args[1:])`
- No flags in `main`; routing only.
- Empty args → `cli.RunTUI()`
- First-arg aliases: `--help`/`-h`/`help`, `--version`/`-v`/`version`
- Subcommands: see `interfaces/cli.md` (full list in `cmd/rex/main.go` switch).

Exit codes: `cli.ExitCoder` via `exitCodeFor`; default `1` on error. Stderr prefix `rex:`.

### `cmd/rex-daemon`

- `main()` → `run` with `flag.FlagSet`:
  - `-socket` — default `defaultSocketPath()`: `$XDG_RUNTIME_DIR/rex.sock` else `~/.cache/rex/rex.sock`
  - `-state-dir` — default `~/.local/share/rex`
  - `-tools` — default `~/.config/rex/tools.yaml`
  - `-version` — prints `v1` (const `version` in main)
  - `-max-concurrent-sessions` — default `16`
- Startup: `rexlog.Init("daemon")`, `registry.Load`, `state.LoadAll`, optional `summarizer.Worker`, optional `lua.Runtime`, `server.New` + `Serve`.
- `SIGHUP` reloads registry into server.
- `OLLAMA_HOST` env — normalized to HTTP base URL for summarizer (see `ollamaBaseURL()` in main).

### Tests

- `cmd/rex-daemon/summarizer_integration_test.go` — summarizer → store description patch (not a third binary).

## Internal structure

- `cmd/rex/main.go` — thin router to `internal/surface/cli`.
- `cmd/rex-daemon/main.go` — daemon wiring, signal handling, defaults.
- `cmd/rex-daemon/summarizer_integration_test.go`

## Invariants

Socket and state-dir defaults must stay aligned with `internal/runtime/daemonctl.DefaultSocket` and CLI `defaultStateDir()` helpers.

## Side effects

`rex-daemon`: listens on UDS, spawns PTYs, writes state dir, optional HTTP to Ollama, optional Lua.

## Error handling

`rex-daemon` exits `1` on fatal startup errors. `rex` uses typed exit codes from `internal/surface/cli`.

## Tests

Summarizer integration test only under `cmd/rex-daemon`.

## Gotchas

`version` string in daemon is `"v1"` (not tied to git tag). `rex` has no `flag` package at top level; per-command flags live in `internal/surface/cli`.

## See also

- `interfaces/cli.md`
- `WORKFLOWS.md`
- `ARCHITECTURE.md`
