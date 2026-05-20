# Rex performance benchmarks

Measure-first workflow for rex daemon and TUI hot paths. The orchestrator runs benchmarks **sequentially** on a quiet machine; do not run multiple `make bench` invocations in parallel.

## Quick start

```sh
# Unit benchmarks (S3–S5 micro + persist)
make bench

# Integration benchmarks (S1 PTY flood, S2 concurrent sessions)
make bench-integration

# CPU + heap profiles for a scenario
make bench-profile SCENARIO=s3
```

Results land in `bench/results/`. Profiles land in `bench/profiles/` (gitignored).

## Scenarios

| ID | What | Package |
|----|------|---------|
| S1 | PTY output flood → transcript persist | `bench/integration` |
| S2 | N concurrent echo sessions + snapshot | `bench/integration` |
| S3 | TUI `renderBoard` at 10/50/200 sessions | `internal/surface/tui` |
| S4 | Wire JSONL encode/decode round-trip | `internal/wire/protocol` |
| S5 | `state.LoadAll` cold restore | `internal/daemon/state` |

## Comparing runs

```sh
go test -bench=. -benchmem -count=5 ./internal/daemon/state/... > bench/results/new.txt
benchstat bench/results/baseline-s5.txt bench/results/new.txt
```

Install benchstat: `go install golang.org/x/perf/cmd/benchstat@latest`

## Environment

Record in `bench/BASELINE.md` when capturing baselines:

- `go version`
- `uname -a` / `GOOS` / `GOARCH`
- `GOMAXPROCS` (default unless noted)

## Budgets

See [budgets.txt](budgets.txt). CI can fail when a benchmark exceeds its budget by more than 10% (see `make bench-check`).
