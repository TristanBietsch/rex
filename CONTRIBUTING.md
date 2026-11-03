# Contributing to rex

Docs start at [docs/index.md](docs/index.md).

## Prerequisites

- Go 1.26.10 or newer (`go.mod` pins the minimum).
- Linux: ALSA headers for the TUI's sound support (oto links ALSA via cgo).
  On Debian or Ubuntu, `sudo apt-get install libasound2-dev`.
- `make` and a POSIX shell. Lint and vulnerability tools run through
  `go run` at pinned versions, so there is nothing else to install.

## Make targets

| Target | What it does |
|--------|--------------|
| `make build` | Builds `rex` and `rex-daemon` into the repo root |
| `make test` | Unit and integration tests, plus `third_party/vt10x` |
| `make test-race` | The same with `-race` |
| `make test-e2e` | End-to-end tests under `e2e/` (skipped if absent) |
| `make lint` | golangci-lint for the host OS |
| `make lint-windows` | golangci-lint for `GOOS=windows` |
| `make cover` | Writes `coverage.out` and prints per-package coverage |
| `make cover-check` | Fails if a package drops below its floor in `tools/coverage/baseline.txt` |
| `make fuzz` | Runs every `Fuzz*` target for `FUZZTIME` (default `30s`) |
| `make vuln` | govulncheck |
| `make check` | The gate: vet, race tests, both lints, coverage floors, govulncheck |
| `make bench` | Unit benchmarks; see `bench/README.md` |

Do not run `make install` or `./install.sh` while working on rex unless
you mean to replace your installed binaries.

## Test tiers

1. **Unit and integration** (`make test-race`): CI runs these on every push
   and PR, with `-race` on Linux and macOS and without it on Windows.
   Integration tests start an in-process daemon on a temp socket through
   `internal/testutil`.
2. **End-to-end** (`make test-e2e`): the suite under `e2e/`.
3. **Stress** (`.github/workflows/stress.yml`): nightly, daemon and wire
   tests 50 times under `-race`.
4. **Fuzz** (`.github/workflows/fuzz.yml`): weekly, every fuzz target for
   two minutes.

Tests must not touch your real rex state. Use `t.TempDir()` and
`t.Setenv("HOME", ...)` for anything that resolves paths under `$HOME`.

## Commits

Use [Conventional Commits](https://www.conventionalcommits.org/) subjects:
`type(scope): summary`, for example `fix(daemon): reap child on read
error` or `chore(ci): add stress workflow`. Common types are `feat`, `fix`,
`refactor`, `test`, `docs`, `chore` and `perf`. Keep the subject under
about 72 characters. Use the body to explain why the change is needed.

Keep commits small and logical. Each one should build and pass tests.

## Pull requests

Before you open a PR:

- [ ] `make check` passes locally.
- [ ] New behavior has tests; bug fixes have a test that fails without the fix.
- [ ] Coverage floors still pass. If coverage rose, raise the floor in
      `tools/coverage/baseline.txt`.
- [ ] Docs under `docs/` match the change (flags, keys, protocol, paths).
- [ ] `CHANGELOG.md` has an entry under `[Unreleased]` for user-visible changes.
- [ ] Changes to the socket, PTY input, transcripts or `init.lua` loading
      were checked against [SECURITY.md](SECURITY.md).

Report security issues privately as described in [SECURITY.md](SECURITY.md),
not in a PR.
