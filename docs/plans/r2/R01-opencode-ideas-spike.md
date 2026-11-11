# R01 Spike: ideas worth taking from opencode

## Goal
TODO: "steal the best ideas from opencode and implement them in rex (like the kanban board, the summary, the audit trail, etc)". Note that kanban, summary and audit trail are already rex items (board, S1–S4, R06-W4), so the spike looks for what opencode has that rex lacks.

## Method (docs only, ~1 h)
1. Read opencode's public docs and source (installed locally: `opencode` in `/opt/homebrew/bin`). No proprietary code copied; ideas only, with license noted (opencode is MIT, verify).
2. Run `opencode` once in a temp folder to see the UX.
3. For each candidate, fill: what it is, user value for a multi-agent board, rex equivalent today, effort (S/M/L), conflicts with round 1/2.

## Candidates to check (from opencode's known feature set; verify each)
| Idea | Why it might fit rex |
|---|---|
| Client/server split with several frontends attached to one server | rex already does this (daemon + TUI/CLI); compare protocol ergonomics, maybe an HTTP/SSE read-only API for a web or phone view (ties to R07) |
| Session share links | read-only share of one session transcript (ties to R07) |
| Snapshot-based `/undo` `/redo` of file changes per turn | rex could snapshot the worktree (git stash/commit) at each needs-you transition → "revert this agent's last turn" |
| Agent modes (build / plan) with per-mode permissions and models | spawn presets; ties to R13 auto model and P1 skills |
| Custom commands as markdown files | rex `:` commands / prompt templates from files |
| Themes + keybind config files | covered by U1/U2/R05 |
| Session compaction / summary | covered by S1–S3 |
| LSP diagnostics feedback | out of rex's scope (agent-side) |
| Export session to markdown | `rex export <sel> -o md` (cheap, useful with audit trail) |

## Exit
- A ranked list (top 5) with sizes, written into this file.
- **Gate:** user picks which become R01-x build specs. Nothing is built from this spike directly.

## Decisions

| # | Question | Options (recommended first) |
|---|---|---|
| D1 | Spike breadth | (a) feature survey + top 5 ranked by value/effort · (b) deep dive on one area you name (e.g. undo/redo snapshots) · (c) skip the spike, pick from the candidate table now |
