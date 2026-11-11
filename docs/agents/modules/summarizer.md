# summarizer

**Path:** `internal/features/summarizer`
**Depends on:** `internal/wire/protocol`, `internal/daemon/state` | external: standard library (HTTP client)
**Depended on by:** `cmd/rex-daemon`
**Entry points:** `New`, `(*Worker).Start`, `Client`, `Defaults`

## Purpose

Background worker calls local Ollama HTTP API to generate one-line `description` fields for active sessions; patches `state.Store` and emits health events.

## Public surface

### Types

- `Config` — `BaseURL`, `Model`, `RequestTimeout`, `MinInterval`, `MaxBytes`, `Detail`.
- `Defaults() Config`
- `Client` — `NewClient`, `Generate` (the health probe's bare call), `Complete(ctx, CompleteRequest{System, Prompt, NumPredict, Stop, Temperature})`, `Tags`, `SetModel`
- `TranscriptReader func(sessionID string, max int) []byte`
- `Worker` — `New(cfg, store, transcript)`, `Start(ctx)`, `Channel() chan<- string`, `BackendAvailable`, `MarkAvailable`, `MarkUnavailable`, `SetHealthCallback`, `SetModel`, `Detail`, `SetDetail`, `Summarize`
- `Kind` (`KindActivity`, `KindFleet`), `Input{Tool, Task, State, Text}`, `Prompt{System, User, MaxRunes, NumPredict}`, `BuildPrompt(kind, in, detail)`, `ErrUnavailable`
- `Detail` (`DetailMedium` is the zero value, `DetailShort`, `DetailLong`), `ParseDetail(string)`, `Detail.MaxRunes()`, `Detail.NumPredict()`
- `Clean(s, maxRunes, tool) string` — the guard every model reply passes through
- `ResolveModel(configured string, pulled []string) (string, bool)`

### Summarize and how to add a Kind

`(*Worker).Summarize(ctx, kind, in)` builds the prompt, takes the shared Ollama slot, calls the model (one retry for transient errors only) and returns `Clean(reply, p.MaxRunes, in.Tool)`; `""` with a nil error means the reply cleaned to nothing. It stores nothing and is safe from any goroutine. It returns `ErrUnavailable` while the backend is down.

`KindFleet` (`prompt_fleet.go`) is the board overview: `Input.Text` is the digest from `internal/features/overview` and the reply is a headline of at most `protocol.MaxOverviewRunes` (200) runes, `NumPredict` 90, whatever the detail level. Its name is internal; users see "overview", and it is unrelated to the `fleet` label. The digest sits in a `<sessions>` block whose delimiter is defused. See [overview.md](overview.md).

To add a Kind (the marquee banner is next): add a `Kind` constant and one `case` in `BuildPrompt`, and put the template in `prompt_<kind>.go`. Do not edit `prompt_activity.go`. The tool name never goes into a prompt. Input text is untrusted: wrap it in a delimiter and defuse the delimiter (`defuse`).

## Internal structure

- `config.go` — defaults.
- `client.go` — Ollama HTTP (`/api/generate`, `/api/tags`), `statusError`, `transient`.
- `worker.go` — `Worker`, `handle` (one request: build, call, clean, store), model fallbacks.
- `schedule.go` — the coalescing scheduler (`pending`, `next`, `loop`) and the Ollama slot.
- `summarize.go` — `Kind`, `Input`, `Prompt`, `BuildPrompt`, `Summarize`.
- `prompt_activity.go` — the activity prompt (system, user, examples per detail).
- `prompt_fleet.go` — the overview (`KindFleet`) prompt.
- `detail.go` — `Detail` and its caps table.
- `clean.go` — `Clean`: sanitize, labels, quotes, agent-subject openers, sentence case, word-boundary clamp.
- `testdata/screens` (synthetic) and `testdata/prompts` (goldens; `-update`), `eval_test.go` (`ollama_eval` tag, live).
- `*_test.go`, integration test in `cmd/rex-daemon/summarizer_integration_test.go`

## Invariants

- One worker goroutine. Requests for a session coalesce into one pending entry; the oldest request runs first among eligible ones (per-session floor `MinInterval`), and a state change (`working`, `needs_input`, a terminal state) jumps ahead and bypasses the unchanged-prompt skip once. Requests are deferred, never dropped: they survive an outage and a failed call is queued again.
- A session gets exactly one final summary per run of done/failed/crashed (`meta.final`); any move back to a live state (a resume, `uc`) re-arms it.
- Every Ollama call, from the loop or from `Summarize`, holds the one slot: never two in flight.
- Each attempt has its own `RequestTimeout`. Only an unreachable server or a 5xx is retried, once, and never a deadline or a 4xx.
- Per-session state is dropped when the session is removed.
- Model output is untrusted: `Clean` sanitizes first, is idempotent, and caps at the detail level (never above `protocol.MaxDescriptionRunes`). Screen text and prompts are never logged.
- Disabled when daemon passes `SummaryRequest == nil` in `server.Config`.

## Side effects

HTTP to Ollama (`OLLAMA_HOST` / config `BaseURL`); with `OLLAMA_HOST` pointing off this machine, screen text leaves the machine. Updates session descriptions in store (broadcasts `SessionUpdated`).

## Error handling

Call failures are logged (never the text), the request is queued again, and three in a row mark the backend unavailable until the health probe restores it. Health callback drives `SummarizerHealth` events on availability flips.

## Tests

HTTP fakes in `client_test.go`, `worker_test.go`, `prompt_test.go`; e2e in `cmd/rex-daemon`.

## Gotchas

Daemon probes Ollama at startup and every 30s; it may substitute a small fallback model via `ResolveModel` (never an arbitrary pulled model). The probe only marks the backend available after a warm-up `/api/generate` succeeds, which also loads the model (`keep_alive` 30m). Input is the supervisor's emulated-screen text (`state.Store.Screen`, chrome filtered by `internal/daemon/termtext`), falling back to the cleaned transcript tail; empty input skips the call. Prompt includes the session `Title` (the task). Settings `summary_enabled` / `summary_model` are read in daemon main at startup; `summary_detail` is also applied live by `boot.WatchSettings` (a 5 s poll of `config.yaml`; features add an applier there). Replies come from a model reading agent output, so they can be wrong or manipulated; they are display-safe and capped, nothing more.

## See also

- `interfaces/config.md` — `OLLAMA_HOST`, summary settings.
- `modules/state.md`
- `modules/cmd.md`
