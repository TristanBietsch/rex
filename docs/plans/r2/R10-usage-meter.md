# R10 Usage meter (top right of the board)

## Summary
- A compact meter right-aligned on the header row: how much of the user's agent plan is used, e.g. `claude 5h 42% · wk 18%  grok 31%`. Configurable in settings (on/off, which tools, compact vs full).
- The hard part is the source of truth. rex only knows `tokens ≈ output_bytes/4` per session, which is not plan usage.

## Current behavior
- `SessionSummary.Tokens` is approximate (OutputBytes/4); stats overlay and `rex stats` sum it.
- Header (`header.go` `renderHeader`) has the logo + counts on the left; nothing on the right. S4 adds an overview line below the chips; T1 adds the chip row.
- Claude sessions are spawned with an injected `--settings` file for hooks (`claude_hooks.go`).
- `grok usage` prints plan usage (checked locally, grok 1.0.77). Claude Code's status line input JSON carries session cost and, in recent versions, plan rate-limit usage (verify field names in step R10-1). Codex shows limits in `/status` (TUI only).

## Design

### Sources (Decision D1)
- `usage.Source` interface in `internal/features/usage`: `Name() string; Poll(ctx) (Reading, error)`; `Reading{Tool, Window string /* "5h","week","month" */, UsedPct float64, ResetsAt time.Time, CostUSD float64, Stale bool}`.
- **Claude via status line** (recommended): the injected `--settings` also sets `statusLine.command` to `rex-daemon statusline-tap --session <id>`; the tap writes the JSON's usage fields to the daemon over the socket, then **execs the user's own status line command** (read from their Claude settings) with the same stdin so their status line is unchanged. Only fields we need are kept; nothing else is stored.
- **Grok via `grok usage`**: daemon polls every 5 min while a grok session is live; parse plan %.
- **Fallback for any tool**: rex's own per-tool token estimate over the window, labeled `~` to show it is an estimate.

### Daemon + wire
- Daemon aggregates latest readings per tool; new `Usage` event + `Snapshot.usage` (additive, memory only), same fan-out pattern as S4 `Overview`.

### View
- Right side of header row 1: `usage_meter` setting `off|compact|full` (default per Decision D2). Compact: worst window per tool as a 5-cell bar `▰▰▱▱▱ 42%`. Full: every window + reset time.
- Colors: U1 tokens `success < 60% ≤ warn < 85% ≤ danger`. Drops cleanly when width is short (counts win over meter).
- `rex usage` CLI with P4 `-o` formats.

## Plan
1. **R10-1 Capture.** Record Claude status-line JSON from a sandboxed session (scratchpad only); record `grok usage` output. Update the field table here.
2. **R10-2 Sources.** `usage` package, Claude tap (`rex-daemon statusline-tap`), Grok poller, estimate fallback. Tests: tap passes stdin through byte-for-byte to a stub user command and preserves its exit code; parse fixtures (synthetic, shaped like captures); missing fields → Stale; grok poll stops when no grok session is live.
3. **R10-3 Wire.** `Usage` event + snapshot. Tests: late-joining TUI gets current value; old daemon → meter hidden.
4. **R10-4 Header view.** Tests: goldens at 120/80/60 cols; meter dropped before counts are cut; colors by threshold.
5. **R10-5 Settings + CLI + docs.** `usage_meter`, `usage_tools`; `rex usage`; `docs/settings.md`, SECURITY.md (status line tap).

## Review focus
- User has no status line configured: tap prints nothing and exits 0; Claude shows no status line as before.
- User's status line command is slow or hangs: tap must not add latency beyond its own parse; it never blocks Claude's render (stream stdin through, write to daemon async with 200 ms cap).
- Daemon down while Claude runs: tap still execs user command.
- Plan with no rate limit (API key billing): show cost instead of %.
- Field names change in a Claude update: Stale, never crash; `rex doctor` reports it.

## Decisions

| # | Question | Options (recommended first) |
|---|---|---|
| D1 | Usage source | (a) real plan usage: Claude status-line tap + `grok usage` poll, estimate fallback for others · (b) rex estimates only (tokens/cost from its own counters; simple, inaccurate) · (c) parse each tool's local logs (e.g. `~/.claude/projects/*.jsonl`, ccusage-style) |
| D2 | Default | (a) `compact`, only for tools with a live session · (b) off until enabled · (c) `full` |
| D3 | Wrapping the user's Claude status line | (a) tap execs the user's status line so it stays the same · (b) tap replaces it with a rex status line · (c) don't touch status line; Claude usage only via (c) logs |
