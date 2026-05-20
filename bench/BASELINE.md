# Rex performance baseline (Pass 1)

Captured on the orchestrator machine before optimization passes.

## Environment

| Key | Value |
|-----|-------|
| Date | 2026-05-19 |
| OS | darwin arm64 |
| CPU | Apple M4 |
| Go | `go1.24.x` (run `go version` when comparing) |
| GOMAXPROCS | 10 (default) |

## Unit benchmarks (`go test -bench=. -benchmem -count=5`)

### S3 — TUI `renderBoard`

| Benchmark | ns/op | B/op | allocs/op |
|-----------|-------|------|-----------|
| RenderBoard_10 | ~150,000 | ~33,700 | 801 |
| RenderBoard_50 | ~720,000 | ~161,000 | 3,882 |
| RenderBoard_200 | ~2,890,000 | ~642,000 | 15,436 |

### S4 — Wire codec round-trip

| Benchmark | ns/op | B/op | allocs/op |
|-----------|-------|------|-----------|
| SessionUpdatedRoundTrip | ~9,600 | 67,165 | 25 |
| SnapshotRoundTrip (50 sessions) | ~256,000 | 129,100 | 116 |

### S5 — `state.LoadAll`

| Benchmark | ns/op | B/op | allocs/op |
|-----------|-------|------|-----------|
| LoadAll_50 | ~1,380,000 | 116,840 | 1,115 |
| LoadAll_200 | ~5,480,000 | 463,788 | 4,417 |

### S1 proxy — `AppendTranscript` (4 KiB chunks, open/close per write)

| Benchmark | ns/op | throughput | B/op | allocs/op |
|-----------|-------|------------|------|-----------|
| AppendTranscript_4KiB | ~69,000 | ~60 MB/s | 820 | 7 |
| AppendTranscript_1MiB (256 chunks) | ~15,000,000 | ~70 MB/s | 217,000 | 1,813 |

## Integration benchmarks (`-tags=integration -count=3`)

### S1 — PTY flood (~1 MiB echo/flood model)

| Benchmark | ns/op | B/op | allocs/op |
|-----------|-------|------|-----------|
| S1_PTYFlood | ~447,000,000 | 47,225,653 | 573,662 |

Wall time ~0.45s per 1 MiB session end-to-end.

### S2 — 20 concurrent echo/short sessions

Run with `make bench-integration` after harness fix; expect multi-second wall time dominated by process spawn.

## Profile artifacts

| Scenario | CPU | Heap (alloc_space) |
|----------|-----|-------------------|
| S1 | `bench/profiles/s1-cpu.prof` | `bench/profiles/s1-mem.prof` |
| S3 | `bench/profiles/s3-cpu.prof` | `bench/profiles/s3-mem.prof` |
| S4 | `bench/profiles/s4-cpu.prof` | `bench/profiles/s4-mem.prof` |
| S5 | `bench/profiles/s5-cpu.prof` | `bench/profiles/s5-mem.prof` |

Generate: `make bench-profile SCENARIO=s3`

## Top hypotheses (pre-fix)

1. **`AppendTranscript`** — syscall open/close per 4 KiB PTY read (S1 proxy shows 7 allocs/write).
2. **`renderBoard`** — triple `filterByGroup` scan + lipgloss per row (S3 scales ~linearly with session count).
3. **Codec** — payload marshal + envelope marshal (S4 ~25 allocs/event).
4. **`LoadAll`** — per-session `ReadFile` + `json.Unmarshal` (S5 ~22 allocs/session at N=200).
5. **PTY reader** — chunk copy + synchronous disk write on read goroutine (S1 integration alloc count).

## Next passes

See [OPTIMIZATIONS.md](OPTIMIZATIONS.md) for accepted changes and measured deltas after Pass 2–6.
