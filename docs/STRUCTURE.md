# Repository structure

Rex is a **two-binary Go application**: `rex` (CLI + Bubble Tea TUI) and `rex-daemon` (PTY supervisor). Clients talk to the daemon over newline-delimited JSON on a Unix domain socket.

This document is the canonical map for where code lives. **Go source wins** on any conflict with older human specs.

## Project kind

| Aspect | Detail |
|--------|--------|
| Binaries | `cmd/rex`, `cmd/rex-daemon` |
| Module | `github.com/tristanbietsch/rex` |
| Scale | ~160 Go files, medium |
| Public API | None exported outside the module (`internal/` only) |

## Top-level layout

```
rex/
├── cmd/                    # entrypoints (package main)
├── internal/               # all library code
├── testdata/               # repo-root fixtures (Go convention)
├── docs/
│   ├── STRUCTURE.md        # this file
│   └── agents/             # agent-oriented module docs
├── Makefile
├── go.mod
└── install.sh
```

## Domain tree (`internal/`)

```
internal/
├── surface/                # user-facing CLI + TUI
│   ├── cli/
│   │   ├── core/           # shared: selectors, output, help, exit codes
│   │   ├── session/        # attach, new, send, reply, ls, rm, …
│   │   ├── inspect/        # status, log, stats, digest, fleet, render
│   │   ├── lifecycle/      # rex daemon / reload commands
│   │   ├── setup/          # setup, doctor, config, update, uninstall
│   │   └── meta/           # completion, complete, TUI entry
│   └── tui/                # Bubble Tea board (single flat package)
├── daemon/                 # rex-daemon supervision
│   ├── server/             # UDS server, intent handlers
│   ├── state/              # in-memory store + disk persistence
│   ├── pty/                # per-session PTY supervisor
│   ├── adapter/            # PTY output → session state
│   └── boot/               # daemon wiring (Lua, Ollama, SIGHUP reload)
├── wire/                   # client ↔ daemon contract
│   ├── protocol/           # JSONL types, codec
│   └── client/             # dial, intents, event stream
├── catalog/                # tools + user settings
│   ├── registry/           # builtin.yaml + tools.yaml
│   └── settings/           # config.yaml registry + store
├── runtime/                # paths, process control, logging
│   ├── daemonctl/          # spawn/check daemon, socket paths
│   ├── ids/                # session UUID / short id
│   └── rexlog/             # file-backed slog for daemon/TUI
├── features/               # optional enhancements
│   ├── summarizer/         # Ollama descriptions
│   ├── audio/              # UI sounds
│   └── lua/                # optional daemon scripting
└── testutil/               # cross-package test helpers (test-only)
```

### Rationale (one line each)

| Directory | Why it exists |
|-----------|----------------|
| `surface/` | Everything the user runs directly (`rex`, `rex attach`, TUI) |
| `surface/cli/*` | 38 command files split by command family |
| `surface/tui/` | Bubble Tea model spans many files; kept one package to avoid export churn |
| `daemon/` | PTY supervision, persistence, classification |
| `daemon/boot/` | Extracted from `cmd/rex-daemon` so `main` stays thin |
| `wire/` | Shared socket protocol + client SDK |
| `catalog/` | `tools.yaml` and `config.yaml` domains |
| `runtime/` | Socket paths, daemon spawn, IDs, slog setup |
| `features/` | Can disable Ollama/Lua/audio without breaking core kanban |
| `testutil/` | Shared daemon fixture for integration tests |

## Where new code goes

```
Need to…                          → Put it in…
────────────────────────────────────────────────────────────
Add a CLI subcommand                → surface/cli/{family}/  (+ register in cmd/rex/main.go)
Add TUI key/view/wizard behavior    → surface/tui/
Change wire message or intent       → wire/protocol/ (+ data/schemas in docs/agents/)
Add daemon session handler          → daemon/server/
Change PTY or transcript I/O        → daemon/pty/ or daemon/state/
Add tool/agent to default install   → catalog/registry/builtin.yaml
Add user-facing setting             → catalog/settings/registry.go only
Add UI sound                        → features/audio/
Add Ollama/summary behavior         → features/summarizer/
Add Lua hook                        → features/lua/ + daemon/boot/
```

## Conventions

### Go filenames

Use **`snake_case.go`** and **`*_test.go`** (Go ecosystem; overrides repo-wide camelCase for `.go` files).

### Go directories

Lowercase, match package name. Exception: `cmd/rex-daemon` (hyphenated binary name, `package main`).

### Tests

| Tier | Location | How to run |
|------|----------|------------|
| Unit | Co-located `*_test.go` | `go test ./internal/surface/cli/core/...` |
| Integration | Same package; uses `testutil` or UDS | `go test ./internal/daemon/server/...` |
| E2E | `daemon/server/e2e_test.go` | included in `make test` |
| Black-box | `wire/client` `package client_test` | `go test ./internal/wire/client/...` |
| Snapshot | `surface/tui/*_snapshot_test.go` | `go test ./internal/surface/tui/...` |

**All tiers:** `make test` (runs `go test ./...`).

**Fixtures:** `testdata/` at repo root (e.g. `testdata/tools-user.yaml` for registry merge tests). From `internal/catalog/registry`, reference with `../../../testdata/...`.

**Choosing a tier:** Same-package, no socket → unit. Real UDS or PTY → integration. Full daemon flow across handlers → e2e. Golden TUI strings → snapshot.

### Types

| Types | Location |
|-------|----------|
| Wire envelopes, intents, events | `wire/protocol` |
| Tool/model registry | `catalog/registry` |
| Setting definitions | `catalog/settings` |
| Session record | `daemon/state` |

No shared `internal/types/` — promote to `wire/protocol` only when two or more domains share a wire shape.

### Entry points

| Binary | Source | Role |
|--------|--------|------|
| `rex` | `cmd/rex/main.go` | Dispatches to `surface/cli/*` or `meta.RunTUI()` |
| `rex-daemon` | `cmd/rex-daemon/main.go` | Flags + `daemon/boot` + `daemon/server` |

Build: `make build` → `./rex`, `./rex-daemon` at repo root.

## Documentation

| Path | Audience |
|------|----------|
| `docs/STRUCTURE.md` | Humans + agents (layout contract) |
| `docs/agents/` | Agent-oriented module map (update paths when moving packages) |

Legacy human specs under repo `docs/*.md` on git HEAD may be stale; prefer source + `docs/agents/`.

## Verification

```sh
make build
make test
make lint
```

Smoke: `rex` (TUI), `rex status`, `rex-daemon -version`.
