# Rex optimizations log

Measured on darwin/arm64, Apple M4, Go default toolchain (May 2026). Baseline in [BASELINE.md](BASELINE.md).

## Summary (baseline → final)

| Scenario | Metric | Baseline | Final | Change |
|----------|--------|----------|-------|--------|
| S1 proxy `AppendTranscript_1MiB` | ns/op | ~15.0M | ~0.33M (`TranscriptFile`) | **~45× faster** |
| S1 proxy `AppendTranscript_4KiB` | ns/op | ~69k | ~1.2k (`TranscriptFile`) | **~48× faster** |
| S1 proxy 4KiB | allocs/op | 7–8 | 0 | **−100%** |
| S1 integration `PTYFlood` | wall ns/op | ~447M | ~444M | ~1% (noise) |
| S3 `RenderBoard_200` | ns/op | ~2.89M | ~2.78M | **~4%** |
| S3 `RenderBoard_200` | B/op | ~642k | ~525k | **~18%** |
| S4 `SessionUpdatedRoundTrip` | ns/op | ~9.6k | ~8.6k | **~10%** |
| S4 | allocs/op | 25 | 24 | −1 |

## Pass 2 — Algorithmic

### Transcript file handle (`OpenTranscript` / `TranscriptFile`)

- **Why:** `AppendTranscript` opened and closed `transcript.log` on every PTY read (~4 KiB).
- **Change:** [persist.go](../internal/daemon/state/persist.go) keeps a `bufio.Writer` for the session lifetime; [supervisor.go](../internal/daemon/pty/supervisor.go) writes through it.
- **Proof:** `BenchmarkTranscriptFile_1MiB` ~330µs vs `BenchmarkAppendTranscript_1MiB` ~15ms.

### Single-pass board partition

- **Why:** `renderBoard` called `filterByGroup` three times (O(3n) scans).
- **Change:** `partitionSessions` in [board.go](../internal/surface/tui/board.go) buckets in one pass.
- **Proof:** `RenderBoard_200` bytes/op −18%, ns/op −4%.

### Single JSON encode for wire envelopes

- **Why:** Payload marshaled, then envelope marshaled again.
- **Change:** One `json.Marshal` over a struct with `Data any` in [codec.go](../internal/wire/protocol/codec.go).
- **Proof:** `SessionUpdatedRoundTrip` ~10% faster, one fewer alloc.

### Spawn short-ID map

- **Why:** `spawn` scanned `Store.All()` to build `taken` on every new session.
- **Change:** `Store.TakenShortIDs()` reads `byShortID` directly.
- **Proof:** Correctness via existing spawn tests; negligible at small N, avoids O(n) spawn.

### Spinner tick only when needed

- **Why:** 100ms `SpinnerTickMsg` forced full `View()` even with zero working sessions.
- **Change:** `hasWorkingSessions()` gates `tickSpinner()` in [update.go](../internal/surface/tui/update.go).
- **Proof:** Idle board no longer schedules spinner work (behavior change: spinner stops when nothing working).

## Pass 3 — Memory discipline

- **PTY read path:** Avoid chunk copy for persist; copy only for `OutputSink` subscribers ([supervisor.go](../internal/daemon/pty/supervisor.go)).
- **Heuristic adapter:** `cleanTail` helper deduplicates tail processing ([heuristic.go](../internal/daemon/adapter/heuristic.go)).

## Pass 4 — I/O and concurrency

- **`Store.Add`:** Release `mu` before `broadcast` so slow subscribers do not hold the session table lock ([store.go](../internal/daemon/state/store.go)).
- **Transcript buffering:** 64 KiB `bufio.Writer` reduces write syscalls without async complexity.

## Pass 5 — Micro

- **Claude adapter:** `bytes.IndexByte` instead of manual newline scan ([claude.go](../internal/daemon/adapter/claude.go)).

## Rejected / out of scope

| Idea | Reason |
|------|--------|
| Real Claude/Codex flood benches | Non-reproducible, network/credential dependent |
| Async transcript queue | Added complexity; buffered file handle sufficient for measured S1 proxy |
| Partial TUI row redraw | High complexity; partition + spinner gate gave acceptable S3 wins |
| `unsafe` string from `[]byte` in heuristic | Marginal; regexp API requires string today |

## Running regression checks

```sh
make bench
make bench-integration
# Compare to budgets.txt with benchstat
```
