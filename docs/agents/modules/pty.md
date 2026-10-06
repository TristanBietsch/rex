# pty

**Path:** `internal/daemon/pty`
**Depends on:** `internal/daemon/adapter`, `internal/wire/protocol`, `internal/daemon/state` | external: `github.com/creack/pty`, standard library
**Depended on by:** `internal/daemon/server`
**Entry points:** `New`, `(*Supervisor).Run`

## Purpose

Runs one agent child process inside a PTY: read output, classify state, persist transcript, forward input, handle resize and clean shutdown.

## Public surface

### Types

- `SupervisorConfig` — `StateDir`, `Store`, `Command`, `CWD`, `Adapter`, `OutputSink`, `SummaryRequest`, `InputCh`, `CompleteCh`, `IdleTick`, `InitialCols`/`InitialRows`, `InitialPrompt`, `RegisterResize`/`UnregisterResize`.
- `Supervisor` — `New(cfg SupervisorConfig)`, `Run(ctx context.Context, sess *state.Session) error`.

Package also exports sanitize helpers used internally (see `sanitize.go`; not all symbols are exported — check `go doc`).

## Internal structure

- `supervisor.go` — main loop: spawn, read PTY, adapter ticks, persist, exit handling.
- `sanitize.go` — ANSI stripping for classification and summarizer input.
- `supervisor_test.go`, `sanitize_test.go`

## Invariants

`Run` blocks until child exit, context cancel (`StateFailed`), or `CompleteCh` (`StateDone`). `Adapter` nil skips state classification.

`OutputSink` must be non-blocking (server uses buffered fan-out).

## Side effects

Spawns child process with PTY. Appends to transcript via `state.AppendTranscript`. May send on `SummaryRequest` channel. Invokes `RegisterResize` callback.

## Error handling

Spawn and PTY errors fail `Run`. Context cancel marks failed state (distinct from `CompleteCh`).

## Tests

`supervisor_test.go` with echo commands; `sanitize_test.go` for ANSI handling.

## Gotchas

Initial prompt goroutine waits for `Adapter.IsReadyForInput` before pasting `InitialPrompt`. Idle sampling defaults to 200ms if `IdleTick` zero. `ReadySettle` additionally requires that much output silence; `PlainPaste` skips bracketed-paste markers (line REPLs like ollama). Tools that take the prompt on argv (claude/codex/gemini) never use the paste path.

A vt10x screen mirrors the child's output (resized with the PTY). Each tick with new visible output derives `last_line` and the summarizer's screen text (`Store.SetScreen`) from it via `internal/daemon/termtext`. Adapter-reported `done` is sticky. `meta.json` is written at start and on every state transition. `Env` entries are appended to the child environment (`REX_HOOK_FILE`, `REX_SESSION_ID`).

## See also

- `modules/adapter.md`
- `modules/server.md` — constructs `SupervisorConfig` per session.
