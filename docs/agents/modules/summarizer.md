# summarizer

**Path:** `internal/features/summarizer`
**Depends on:** `internal/wire/protocol`, `internal/daemon/state` | external: standard library (HTTP client)
**Depended on by:** `cmd/rex-daemon`
**Entry points:** `New`, `(*Worker).Start`, `Client`, `Defaults`

## Purpose

Background worker calls local Ollama HTTP API to generate one-line `description` fields for active sessions; patches `state.Store` and emits health events.

## Public surface

### Types

- `Config` — `BaseURL`, `Model`, `RequestTimeout`, `MinInterval`, `MaxBytes`.
- `Defaults() Config`
- `Client` — `NewClient`, `Generate`, `Tags`, `SetModel`
- `TranscriptReader func(sessionID string, max int) []byte`
- `Worker` — `New(cfg, store, transcript)`, `Start(ctx)`, `Channel() chan<- string`, `BackendAvailable`, `MarkAvailable`, `MarkUnavailable`, `SetHealthCallback`, `SetModel`
- `ResolveModel(configured string, pulled []string) (string, bool)`

## Internal structure

- `config.go` — defaults.
- `client.go` — Ollama HTTP (`/api/generate`, `/api/tags`).
- `worker.go` — single goroutine consumer of session ID channel.
- `prompt.go` — prompt template and response cleanup.
- `*_test.go`, integration test in `cmd/rex-daemon/summarizer_integration_test.go`

## Invariants

One worker goroutine; per-session rate floor `MinInterval`. Disabled when daemon passes `SummaryRequest == nil` in `server.Config`.

## Side effects

HTTP to Ollama (`OLLAMA_HOST` / config `BaseURL`). Updates session descriptions in store (broadcasts `SessionUpdated`).

## Error handling

Generate failures logged; worker continues. Health callback drives `SummarizerHealth` events on availability flips.

## Tests

HTTP fakes in `client_test.go`, `worker_test.go`, `prompt_test.go`; e2e in `cmd/rex-daemon`.

## Gotchas

Daemon probes Ollama at startup and may substitute fallback model via `ResolveModel`. Transcript passed to model is sanitized tail, max bytes from config. Settings `summary_enabled` / `summary_model` read in daemon main, not inside this package.

## See also

- `interfaces/config.md` — `OLLAMA_HOST`, summary settings.
- `modules/state.md`
- `modules/cmd.md`
