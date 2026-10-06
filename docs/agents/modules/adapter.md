# adapter

**Path:** `internal/daemon/adapter`
**Depends on:** `internal/wire/protocol`, `internal/catalog/registry` | external: `github.com/charmbracelet/x/ansi`, standard library
**Depended on by:** `internal/daemon/server`, `internal/daemon/pty`
**Entry points:** `For`, `Adapter`, `NewHeuristic`, `NewClaudeHooks`, `ClaudeHookArgs`

## Purpose

Classifies PTY output into `protocol.State` values and detects when an agent CLI is ready for initial prompt paste.

## Public surface

### Types

- `Adapter` interface — `Detect(window []byte, idle time.Duration) protocol.State`; `IsReadyForInput(window []byte, idle time.Duration) bool`.
- `HeuristicCLI` — regex + idle adapter for unstructured CLIs.
- `ClaudeHooks` — reads the per-session hook state file written by Claude Code hooks (`ClaudeHookArgs`).

### Variables

- `ErrUnknownDetect error` — unsupported `detect.kind` in registry.

### Functions

- `For(t registry.Tool, hookFile string) (Adapter, error)` — builds adapter from tool `Detect` config.
- `NewHeuristic(promptRegex, doneRegex string, idle time.Duration) (*HeuristicCLI, error)` — `promptRegex` required; invalid regex returns error.
- `NewClaudeHooks(path string) *ClaudeHooks`
- `ClaudeHookArgs() []string` — `--settings <json>` argv injecting the state hooks
- `HookFileEnv` — `REX_HOOK_FILE`

### Methods

- `(*HeuristicCLI) Detect`, `IsReadyForInput`
- `(*ClaudeHooks) Detect`, `IsReadyForInput` (always false; Claude gets its prompt on argv)

## Internal structure

- `adapter.go` — `Adapter` interface and `For`.
- `heuristic.go` — regex detection; precedence after idle gate: done > prompt > working.
- `claude_hooks.go` — hook-file state; stale `working` (15s silent) → `needs_input`.
- `claude_test.go`, `heuristic_test.go` — detection fixtures.

## Invariants

`Detect` receives a rolling PTY window (sanitized ANSI) and idle duration sampled on a tick (default 200ms in supervisor).

Heuristic `IsReadyForInput` matches prompt regex without idle gate.

## Side effects

None (pure classification on byte slices).

## Error handling

`For` returns `ErrUnknownDetect` for unknown kinds. `NewHeuristic` returns regex compile errors.

## Tests

`heuristic_test.go`, `claude_test.go` — state transitions from sample output.

## Gotchas

`Adapter` nil in `SupervisorConfig` disables classification (used by echo test tool). `done_regex` optional; when set, heuristic can reach `StateDone` after idle.

## See also

- `modules/registry.md` — `Detect` YAML shape.
- `modules/pty.md` — calls `Detect` on output chunks.
- `data/schemas.md` — `State` enum.
