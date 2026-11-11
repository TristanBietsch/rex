# overview

**Path:** `internal/features/overview`
**Depends on:** `internal/wire/protocol` | standard library only
**Depended on by:** `internal/daemon/boot`, `internal/surface/tui`, `internal/surface/cli/inspect`
**Entry points:** `Classify`, `Group`, `Digest`, `NewRunner`

## Purpose

The board overview: which sessions need the user, which are ready for review, which failed, work or finished. The buckets come from rules over session fields. The package also builds the digest the summarizer turns into one headline, and runs the loop that keeps that headline current. It is pure: no store, server or model import. The daemon passes closures.

## Public surface

### Buckets

- `Bucket` (`NeedsYou`, `Review`, `Failed`, `Working`, `Completed`), `Buckets`, `Bucket.String()`, `Bucket.Key()` (`needs_you`, ...)
- `Classify(s) Bucket` — rules in order: marked done, failed/crashed, done, `needs_input` (review when `wait_reason == review` and the last line is not a question), working/queued.
- `Group(ss) map[Bucket][]SessionSummary` — most recent first (a completed session by when it was marked).
- `IsQuestion(lastLine)` — the board's question rule (flattened last line ends in `?`).
- `CompletedWindow` (24 h), `Listed(bucket, s, now)`, `At(s)` — what overviews list and the age they show.

### Digest

- `Digest(ss, now) string` — one line per session in bucket order. Every field is flattened and cut (name 40, task 80, status 120 runes); a literal `</sessions>` is defused; at most 5 completed, 40 lines and 4096 bytes, ending `- …and N more` when cut.
- `Fingerprint(ss) Prints{Structural, Content}` — Structural: ids, bucket, state, mark, name, slug, title, wait reason. Content adds descriptions. Tokens, last lines and times change neither.
- `Relevance` (`Ignored`, `ContentChange`, `StructuralChange`), `PatchRelevance(patch)`.

### Runner

- `Config{Debounce, MinInterval, Refresh, Timeout}`, `DefaultConfig()` (3 s, 30 s, 2 min, 45 s)
- `NewRunner(cfg, snapshot, generate, publish, active) *Runner`, `Notify(Relevance)`, `SetEnabled(bool)`, `Run(ctx)`
- `ErrUnavailable` — what `generate` returns while the backend is down.

## Internal structure

- `bucket.go` — buckets, `Classify`, `Group`.
- `digest.go` — `Digest`, fingerprints, relevance, `At`, `Listed`.
- `runner.go` — the runner.
- `*_test.go` — unit tests; `main_test.go` fails the package on a leaked goroutine.

## Invariants

- A structural change runs after `Debounce`; a description-only change at most once per `Refresh`; at least `MinInterval` between calls. An unchanged content fingerprint, a board with nothing live (only completed sessions) and `active() == false` make no call.
- `ErrUnavailable` parks the request until the next `Notify` (the daemon sends one when the backend returns and when a TUI says Hello). Any other error retries after `MinInterval`.
- Disabled is inert: `Notify` drops events, `Run` only waits. Turning it off publishes `{Enabled:false}` and discards a call in flight.
- The runner only displays. Its output never drives marking, sending or retiring. Logs carry counts and durations, never the digest or the headline.

## Wiring

`daemon/boot.Overview` builds the runner over `store.Snapshot`, `summarizer.Worker.Summarize(ctx, KindFleet, …)`, `server.SetOverview` and `server.BoardClients() > 0` (only `rex-tui` connections count). The setting `overview_enabled` follows the single settings watcher through `Overview.Applier()`; the first switch on starts the runner goroutine.

## Gotchas

`KindFleet` is the summarizer's internal name for the digest prompt; users see "overview". The fleet label on sessions is unrelated.
