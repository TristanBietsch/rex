# usage

**Path:** `internal/features/usage`
**Depends on:** `internal/wire/protocol`, `internal/wire/client`, `internal/runtime/paths`
**Depended on by:** `internal/daemon/boot`, `cmd/rex-daemon`, `internal/surface/cli/inspect`
**Entry points:** `RunTap`, `NewHub`, `ShouldInjectTap`, `ParseStatusLine`

Spec: [docs/plans/r2/R10-usage.md](../../plans/r2/R10-usage.md).

## Purpose

What the agents used: per-session tokens and cost in an append-only ledger, the plan quotas Claude Code reports, and the aggregates behind `rex usage`. Off (the `usage` setting) means inert: no tap, no goroutine, no file.

## Public surface

- `Source`, `Reading`, `Sources`, `DefaultSources`: the source interface. `EstimateSource` (rex's own token counter, always `Estimated`) and `GrokSource` (registered; answers `ErrNotImplemented` until R10-1b captures `grok usage`).
- `ParseStatusLine(raw)`: keeps only the documented fields of Claude's status-line JSON; every field may be absent or null.
- `RunTap(ctx, TapOptions)`: `rex-daemon statusline-tap`. Sends a `UsageSample` (fire and forget, 200 ms cap) and runs the **user-level** status line command with the identical stdin, passing stdout, stderr and the exit code through. `UserStatusLine(home)` reads only `$CLAUDE_CONFIG_DIR/settings.json` (default `~/.claude/settings.json`), never a project file.
- `ShouldInjectTap(usageOn, mode, home)` and `TapCommand(exe, session, socket)`: the `auto|on|off` decision and the injected `statusLine.command`.
- `Ledger` (`OpenLedger`, `Append`, `Latest`, `Trim`) and `Record`: `usage.jsonl`, 0600; latest line per session wins; torn or corrupt lines skipped with one warning.
- `Hub` (`NewHub`, `SetEnabled`, `Observe`, `Removed`, `Sample`, `Report`, `Query`, `Usage`, `Close`): live totals, the write queue (store subscribers never touch a file), quotas and `OnUsage` publishing. `Aggregate` and `RangeStart` build the `UsageReport`.

## Wiring

`internal/daemon/boot/usage.go` (`UsageTap`, setting reads) and `usage_hub.go` (`NewUsage`: store subscription, `WatchSettings` applier). The server hooks are `Config.UsageTap`, `OnUsageSample` and `UsageQuery`; `Server.SetUsage` fans the `Usage` event out like the overview.
