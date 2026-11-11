# Round 2 roadmap: the TODO items without `***`

Date: 2026-11-11. Status: **decisions answered 2026-11-11**; ready for review. Each spec's "Decisions" table records its answers.

Round 1 (`docs/plans/00-orchestration.md`) owns every `***` item plus U4–U6 and P4. Round 2 starts once round 1's last wave merges into `integration/todo`. Where this roadmap and round 1 disagree, round 1 wins for anything it already shipped.

## Scope

Scope rules agreed 2026-11-11:
- Everything still open in `TODO.md` that round 1 does not cover.
- The ANTI settings spec is folded in as input. Only the parts U1/U2 do not already deliver are planned.
- Lua config is a layer **after** U2 (keeps D1: YAML themes now, Lua later).
- Question-mark and vague items get a spike + spec with a go/no-go gate. No build tasks before the gate. Multiplayer and research fork are deferred.
- Every feature is a toggle in Settings → Features (see "Feature flags").

| ID | TODO line | Kind | Size | Spec |
|---|---|---|---|---|
| R01 | steal the best ideas from opencode | spike | S | [R01](R01-opencode-ideas-spike.md) |
| R02 | add qwen code, cursor cli, grok build (no oh-my-pi); auto-detect models | build | L | [R02](R02-more-agents-model-detect.md) |
| R03 | "master agent" project manager (top-level line) | dup | — | Closed: duplicate of round-1 **S4**. |
| R04 | settings + help as full-screen pages (ANTI) | build | M | [R04](R04-fullscreen-settings-help.md) |
| R05 | settings config in Lua | build | L | [R05](R05-lua-config.md) |
| R06 | per-workspace `.rex` folder (+ id, edge color, system-wide rex, sync, audit trail, CLI) | epic | XL | [R06](R06-workspaces.md) |
| R07 | multiplayer? | deferred | — | Deferred epic ([R07](R07-multiplayer-spike.md) kept as notes). |
| R08 | rex fork for research only? | deferred | — | Ignored for now ([R08](R08-research-mode-spike.md) kept as notes). |
| R09 | configurable board sections (ToDo … Done) | build | L | [R09](R09-board-sections.md) |
| R10 | `:usage` page (per agent, per model, total, quota) + header meter | build | L | [R10](R10-usage.md) |
| R11 | Jira / Linear sync? | build | L | [R11](R11-linear-sync.md): Linear only, two-way |
| R12 | loop workflows (epic) | epic | L | [R12](R12-loop-workflows.md) |
| R13 | (epic) insert mode auto-chooses model | epic | M | [R13](R13-auto-model.md) |

**R03 note.** S4 already plans the "master agent" as a daemon-side overview with buckets *needs you / ready for review / failed / working / completed* and an Ollama headline. It rejects a spawned Claude "master" on cost and cross-session prompt-injection grounds. No follow-up (R03-D1): the top-level TODO line closes when S4 merges.

## Feature flags (rule for every round-2 feature)

User rule (2026-11-11): every feature, round 1 and round 2, can be turned on and off in Settings. Round 1's flags are in `../00-orchestration.md` (D15, "Feature flags").

- New settings section **Features** (`SectionFeatures`), placed after Behavior; the round-1 contiguity test covers it.
- One helper: `settings.FeatureOn(store, id) bool`. Every entry point checks it (daemon goroutines, TUI keys, `:` commands, CLI commands, spawn argv additions).
- Off means **inert**: no goroutine, subprocess, network call or file write; keys unbound; commands answer `off: enable in Settings → Features (rex config set <flag> true)`, exit code 5 from the CLI.
- Toggles apply live through U2's `SettingsHub` where possible; a flag that needs a daemon restart says so in its help text (U1 restart marker).
- Defaults: off for anything that adds network egress, runs extra code or changes behavior on its own; on for pure UI and features that stay inert without a config file.
- Each feature's owning step adds an off-is-inert test. The section, the helper and the registry test are built in round 1 as task **FF0** (D15); round 2 only adds rows.

| Flag | Feature | Default |
|---|---|---|
| `extra_agents` | R02 qwen, cursor, grok tool entries | on |
| `model_autodetect` | R02 model lists from each CLI | on |
| `fullscreen_pages` | R04 full-screen settings/help (off = U1 overlays) | on |
| `lua_config` | R05 `config.lua` layer (inert without the file) | on |
| `workspaces` | R06 `.rex/`, `rex here`, workspace config | off |
| `audit_trail` | R06 audit records + `rex audit` | on |
| `audit_full_prompt` | R06 full prompt text in audit | off |
| `board_preset` | R09 `simple` / `kanban` / `custom` (simple = off) | simple |
| `usage` | R10 usage ledger, sources, `:usage` page, `rex usage` | on |
| `usage_meter` | R10 header meter `off` / `compact` / `full` | compact |
| `usage_statusline_tap` | R10 Claude status-line tap | on |
| `linear_sync` | R11 Linear two-way | off |
| `loops` | R12 loop workflows | off |
| `auto_model` | R13 auto model in insert mode | off |
| (per R01 pick) | each approved R01 idea gets its own flag | off |

## Cross-feature dependencies

```
round 1 (all waves, incl. FF0) ──┬─> R02 (needs U3-1 retired-tool guard, spawnSpec, A1 OutputObserver)
                      ├─> R04 (needs U1 command table + palette, U2 theme/SettingsHub)
                      │     └─> R05 (Lua layer on U2 store + SettingsHub)
                      │           └─> R06 W-config (.rex/config.lua uses R05 evaluator)
                      ├─> R09 (needs F1 boardGroups, T1 hidden sections, S4 wait_reason)
                      ├─> R10 (header right slot; S4 overview line placement)
                      └─> R13 (needs P1 wizard fields, R02 model catalog)
R09 drafts ─> R11 (Linear issues land as ToDo drafts); R06 audit ─> R11 write log
R09 ─> R12 (loop steps move cards between stages)
U4 (round-1 border) ─> R06 edge color + workspace id
spike R01: any time, ends at a user gate; R11-0 API spike before the R11 build
```

## Waves (3 lanes, same per-task pipeline as round 1)

Lane A = TUI, B = daemon/registry/protocol, C = CLI/settings/features.

| Wave | Lane A | Lane B | Lane C |
|---|---|---|---|
| 0 | — | R02-1 live captures (qwen, cursor-agent, grok) | R01 spike, R11-0 API spike (docs only) |
| 1 | R04 full-screen pages | R02-2..6 registry + detect | R05-1..4 Lua evaluator + store layer |
| 2 | R09-1..4 stage model + view | R09-5..6 stage on wire/meta | R05-5..7 TUI lock, `rex config` |
| 3 | R10-5..6 page + header meter | R10-1..4 sources, ledger, wire | R13 auto model → R10-7 CLI |
| 4b | R09-6 drafts UI | R11-1..4 Linear client, pull, push | R11-5..6 UI + docs |
| 4 | R06 W0–W4 (own orchestration file after W0 gate; W5 deferred, W6 dropped) | | |
| 5 | R12 repeat loops (chains deferred) | | |
| 6 | approved R01 ideas | | |

## Per-task pipeline

Same as round 1 (`00-orchestration.md` → "Per-task pipeline"): worktree per task, test first, one commit per step, `make check` green, docs + CHANGELOG in the same step, independent reviewer, orchestrator `dev/sandbox.sh` check, merge in wave order. Never `make install`, never write real `~/.claude`, `~/.gemini`, `~/.config/rex`, no push, no PR until the user says so.

## Decisions

| # | Decision | Answer |
|---|---|---|
| R01 | spike breadth | survey + top 5; you pick what gets built |
| R02 | tools, missing, refresh, detect | claude, agy, qwen, cursor, grok (no omp) · missing hidden in wizard, dimmed in Settings · refresh at start/reload/24 h · regex now, qwen hooks later |
| R03 | follow-up to S4 | none; TODO line closes as a duplicate of S4 |
| R04 | layout, themes, audio | two-pane inspector · dracula + tokyo_night + pantone_safe · volume now |
| R05 | API, require, locked, YAML | `rex.set` · `~/.config/rex/lua/` only · refuse + open file · YAML stays the TUI write target |
| R06 | open, data, git, sync, audit, scope | `rex here` / `rex ws open` · transcripts in daemon dir · local git exclude · no rex sync · first prompt line (full opt-in) · W0–W4 |
| R07 | multiplayer | deferred epic |
| R08 | research fork | ignored for now |
| R09 | stage, default, ToDo | hybrid · `simple` default, switchable in Settings · drafts |
| R10 | source, default, tap, page | real plan usage · compact meter · pass through to the user's status line · `:usage` page + header meter |
| R11 | tracker | Linear only, two-way |
| R12 | kinds, format, stop | repeat first · YAML · check command exit 0 + cap |
| R13 | router, confirm, scope, fallback | Ollama only · inline preview · model + effort · defaults + notice |
| — | feature flags | every feature (round 1 + 2) is a Settings → Features toggle; round 1 via D15 + FF0 |
