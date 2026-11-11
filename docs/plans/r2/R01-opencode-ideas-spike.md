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

## Findings (2026-11-11)

Checked: opencode 1.18.35 installed locally (`opencode --help`, `stats`, `session`), docs at opencode.ai (snapshots, agents, server). License: MIT (github.com/anomalyco/opencode). Ideas only; no code copied.

### What opencode has, and where rex stands

| opencode feature | How it works there | rex today / plan |
|---|---|---|
| Snapshots + `/undo` `/redo` | Separate internal git object store; captures the session's directory before each model call and after each step; `/undo` restores files to before the reverted step (removes files the step created); not a backup, cannot undo shell side effects; `snapshots: false` disables | nothing; an agent's bad turn is undone by hand |
| Session diff (`/session/:id/diff`) | files changed by a session, from its snapshots | nothing; S4 says "ready for review" but there is nothing to review in rex |
| Custom commands | markdown prompt templates in `~/.config/opencode/commands/` and `.opencode/commands/`, with arguments | nothing; every prompt typed by hand |
| Agents (build / plan, user-defined) | markdown files with frontmatter: model, prompt, tool permissions; `.opencode/agents/` per project; Tab switches | P1 adds name/model/skills per spawn; no saved presets |
| `export` / `import` | session as JSON (file or URL) | transcripts are raw PTY bytes; no export |
| `stats` | tokens and cost by model, `--days`, `--project` | R10 `:usage` page |
| `serve` / `attach` / `web` | headless HTTP server, OpenAPI 3.1 at `/doc`, SSE `/global/event`, basic-auth password env, mDNS | rex daemon is UDS only; remote access deferred with R07 |
| `--fork` | fork a session when continuing | R08 deferred |
| `/share` | public URL of a session | rejected: public links conflict with the "never publish live session output" rule |
| `pr <number>` | checks out a PR branch, then runs the agent | nothing |
| LSP, formatters, providers, MCP, plugins | agent-side features | out of scope: rex supervises agents, it is not one |

### Top 5 (ranked by value for a multi-agent board ÷ effort)

| Rank | Idea | What ships in rex | Size | Flag | Depends on |
|---|---|---|---|---|---|
| 1 | **Turn snapshots + undo** | daemon snapshots a session's cwd into a shadow git dir (`~/.local/share/rex/snapshots/<session>/`, `GIT_DIR` + `GIT_WORK_TREE`, never touches the user's repo or index) at each working → needs-you transition; `:undo` / `rex undo <sel>` restores files to before the last turn; warns when another live session shares the cwd | L | `turn_snapshots` (off) | T2 transitions |
| 2 | **Session diff / review view** | `d` on a card or `:diff`: files changed by the session (first snapshot vs now, or per turn), full-screen page with a file list + unified diff; `rex diff <sel>` | M | `session_diff` (on when snapshots on) | 1, R04 frame |
| 3 | **Prompt templates** | markdown files in `~/.config/rex/prompts/` and `.rex/prompts/` (R06) with frontmatter (tool, model, effort) and `$ARGUMENTS`; `:p <name> args`, wizard picker, `rex new -template` | M | `prompt_templates` (on) | P1 wizard |
| 4 | **Spawn presets** | saved tool + model + effort + skills + name prefix (`~/.config/rex/presets/*.yaml`); wizard shows presets first; R13 auto model can pick a preset | S–M | `spawn_presets` (on) | P1, R13 |
| 5 | **Session export** | `rex export <sel> -o md|json`: metadata + sanitized transcript text (ANSI stripped, U1 sanitizer), never raw bytes; local file only | S | `session_export` (on) | P4 output kit |

Also considered, not in the top 5: `rex pr <n>` (spawn an agent on a PR checkout in a new git worktree; M, useful, but needs a worktree-per-session design first) and a local read-only HTTP+SSE API (waits on R07).

Risks to carry into specs:
- Snapshots of a large repo: copy-on-write via git objects is cheap, but untracked large files must be capped (opencode caps untracked files at 2 MB) and `.gitignore` respected.
- Undo with two agents in one folder can revert the other agent's work: refuse by default when another live session shares the cwd, `--force` to override.
- Diff and export output may contain secrets: shown locally only; never attached to PRs, issues or commits (repo rule).

## Exit
- **Gate:** user picks which of the top 5 become build specs (R01-1..R01-5). Each gets its own flag, default as listed.

## Decisions (answered 2026-11-11)

| # | Question | Answer |
|---|---|---|
| D1 | Spike breadth | Survey + top 5 ranked by value/effort; user picks which become build specs. |
| D2 | Gate: which ideas get built | All five: R01-1 turn snapshots + undo, R01-2 session diff, R01-3 prompt templates, R01-4 spawn presets, R01-5 session export. Each needs its own build spec before code. |
