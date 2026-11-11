# R10 Usage: `:usage` page and header meter

## Summary
- TODO (2026-11-11): "`:usage` page, shows the usage of each agent, the total usage, and the usage of each model (like cursor's :usage) and remaining quota for each model". Decided: build the **page** and keep a **compact header meter** as its one-line summary.
- **Page** (`:usage`, `rex usage`): per agent (tool), per model, and total, over a time range (today / 7 d / 30 d), plus remaining quota and reset time wherever the tool reports one.
- **Header meter**: right side of the board's top row, e.g. `claude ▰▰▱▱▱ 42% · grok ▰▱▱▱▱ 18%`, the worst quota window per tool with a live session.
- The hard part is the source of truth. rex only knows `tokens ≈ output_bytes/4` per session, which is not plan usage. Real numbers come from the tools themselves.

## Current behavior
- `SessionSummary.Tokens` is approximate (OutputBytes/4); the stats overlay and `rex stats` sum it.
- Header (`header.go` `renderHeader`) has the logo and counts on the left and nothing on the right. S4 adds an overview line below the chips; T1 adds the chip row.
- Claude sessions are spawned with an injected `--settings` file for hooks (`claude_hooks.go`).
- `grok usage` prints plan usage, or a session's persisted token and cost usage (checked locally, grok 1.0.77). Claude Code's status line input JSON carries session cost and model, and in recent versions plan rate-limit usage (verify field names in R10-1). Codex shows limits only in its TUI `/status`.
- Session data is deleted on prune/reap, so a 7-day or 30-day view needs its own record (below).

## Design

### Sources (D1)
- `usage.Source` interface in `internal/features/usage`: `Name() string; Poll(ctx) ([]Reading, error)`.
  - `Reading{Tool, Model, SessionID string; Window string /* "5h","week","month","session" */; UsedPct float64; ResetsAt time.Time; CostUSD float64; Tokens int64; Estimated, Stale bool; At time.Time}`.
  - `Model` is empty for a plan-wide quota and set for a per-model quota or per-session cost.
- **Claude, through a status-line tap:** the injected `--settings` also sets `statusLine.command` to `rex-daemon statusline-tap --session <id>`. The tap sends the usage fields from the JSON to the daemon over the socket (session cost, model, rate-limit windows), then **runs the user's own status line command** (read from their Claude settings) with the same stdin, so their status line is unchanged (D3). Only the listed fields are kept.
- **Grok, through `grok usage`:** the daemon polls every 5 min while a grok session is live and once when the page opens; it parses plan % and reset, and per-session tokens/cost where printed.
- **Any other tool:** rex's own per-session token estimate, marked `~` (Estimated).
- Remaining quota per model is shown only where a tool reports it per model. Otherwise the plan-wide quota appears on the agent row and model rows show usage only (no invented per-model quotas).

### Usage ledger (needed for 7 d / 30 d)
- `~/.local/share/rex/usage.jsonl`, 0600, append-only. A line is written when a session ends, and at most every 10 min for a live session (latest totals: tool, model, session id, tokens, cost, estimated flag). Quota readings are not stored; they are live only.
- Aggregation keeps the latest line per session id, so periodic lines never double count.
- Size: trimmed to the last 90 days on daemon start.
- With R06 on, each line also carries `workspace_id`, and the page can filter by workspace.

### Daemon and wire
- The daemon keeps the latest quota readings per tool and model and the ledger aggregates. New `Usage` event + `Snapshot.usage` (additive, memory only), the same fan-out pattern as S4 `Overview`.
- `UsageQuery{range}` intent returns aggregates for the page and the CLI (so the TUI never reads the ledger itself).

### Page: `:usage` (R04 full-screen frame)
```
∴ REX › USAGE                 today · 7d · 30d           updated 12s ago
──────────────────────────────────────────────────────────────────────────
AGENT / MODEL          SESSIONS   TOKENS     COST    QUOTA          RESETS
claude                       14    1.2M    $18.40    5h ▰▰▱▱▱ 42%    2h10m
                                                     wk ▰▱▱▱▱ 18%    4d
  opus                        3    410k    $11.90
  sonnet                     11    790k     $6.50
grok                          2    ~88k     $0.70    ▰▱▱▱▱ 31%       9d
  grok-build                  2    ~88k     $0.70
antigravity                   1    ~40k        —     —
──────────────────────────────────────────────────────────────────────────
TOTAL                        17    1.3M    $19.10
──────────────────────────────────────────────────────────────────────────
esc back  ←/→ range  r refresh  / filter  ~ = estimate
```
- `←`/`→` (or `1`/`2`/`3`) switch the range; `r` refreshes (polls grok now); `/` filters by tool or model; with R06 on, `w` toggles this workspace / all.
- Quota bar colors from U1 tokens: `success < 60% ≤ warn < 85% ≤ danger`. Stale readings are dimmed with "as of 14:02".
- Narrow (U5 `mobile`): one card per agent, models listed under it, quota line last.

### Header meter
- Right side of header row 1, controlled by `usage_meter` (`off|compact|full`, default `compact`, D2).
  - Compact: the worst window per tool with a live session, as a 5-cell bar plus %.
  - Full: every window plus reset time.
- Dropped first when the row is short; the counts always win. Mouse click on the meter opens `:usage`.

### CLI
- `rex usage [--range today|7d|30d] [--tool claude] [--by model|tool] [-o text|json|md|agent]` uses the P4 output kit. Same aggregates as the page.

## Feature flag
- `usage` (on): ledger, sources, page, `rex usage`. Off = no tap, no grok polling, no ledger writes; `:usage` and `rex usage` answer "off".
- `usage_meter` (`off|compact|full`, default `compact`): header meter only; ignored while `usage` is off.
- `usage_statusline_tap` (on): off = Claude is spawned without the tap; Claude rows use the estimate.
- All three live in Settings → Features. Each owning step adds an **off-is-inert** test: with the flag off, no goroutine, subprocess, network call or file write happens, and the feature's keys and commands answer "off: enable in Settings → Features".
- Flags land in R10-2 (`usage`, `usage_statusline_tap`) and R10-6 (`usage_meter`).

## Plan
1. **R10-1 Capture.** Record Claude status-line JSON from a sandboxed session (scratchpad only, never committed) and `grok usage` output (plan and per-session forms). Write the field table into this file. No code.
1b. **R10-1b capture `grok usage`. TODO.** Capture `grok usage` output (plan and per-session forms) from a sandboxed session, add the field table here, then write the grok parser and poller. Until then `usage.GrokSource` is registered but answers `ErrNotImplemented`: no parser exists and none may be guessed.
2. **R10-2 Sources + flags.** `usage` package, Claude tap (`rex-daemon statusline-tap`), grok poller, estimate fallback; `usage` and `usage_statusline_tap` flags. Tests: tap passes stdin byte-for-byte to a stub user command and keeps its exit code; parse fixtures (synthetic, shaped like the captures); missing fields → Stale; grok poll stops when no grok session is live; off-is-inert for both flags.
3. **R10-3 Ledger.** Append, periodic snapshot, latest-per-session aggregation, 90-day trim. Tests: no double count across periodic lines; prune/reap of a session leaves its ledger line; 0600 perms; corrupt line skipped with a warning; Windows path.
4. **R10-4 Wire.** `Usage` event, snapshot, `UsageQuery` intent. Tests: late-joining TUI gets current readings; old daemon → page shows "update daemon", meter hidden.
5. **R10-5 Page.** `usage_page.go` on the R04 frame, `:usage`, range keys, filter, refresh. Goldens at 120×40, 80×24 and mobile; estimated values marked `~`; stale values dimmed; zero sessions shows an empty-state line.
6. **R10-6 Header meter.** `usage_meter` setting; click opens the page. Goldens at 120/80/60 cols; the meter drops before the counts are cut; colors by threshold.
7. **R10-7 CLI + docs.** `rex usage` with all `-o` formats; `docs/tui.md`, `docs/cli.md`, `docs/settings.md`, `docs/paths.md` (ledger), SECURITY.md (status-line tap), CHANGELOG.

## Review focus
- User has no status line configured: the tap prints nothing and exits 0; Claude shows no status line, as before.
- User's status line command is slow or hangs: the tap adds no latency beyond its own parse and never blocks Claude's render (stream stdin through; write to the daemon asynchronously with a 200 ms cap).
- Daemon down while Claude runs: the tap still runs the user's command; that usage is missing until the next tap call.
- Plan without rate limits (API-key billing): show cost, quota column `—`.
- Claude changes field names in an update: readings go Stale, nothing crashes, `rex doctor` reports it.
- Session resumed (T3) after its ledger line was written: totals keep growing under the same session id, not as a second session.

## Decisions (answered 2026-11-11)

| # | Question | Answer |
|---|---|---|
| D1 | Source | Claude status-line tap + `grok usage` poll; rex token estimate (`~`) for other tools. |
| D2 | Header meter default | `compact`, only tools with a live session. |
| D3 | User's Claude status line | The tap passes stdin through to the user's own status line command, so it looks unchanged. |
| D4 | Page vs meter | Both: `:usage` page (per agent, per model, total, quota, resets) plus the compact header meter. |
