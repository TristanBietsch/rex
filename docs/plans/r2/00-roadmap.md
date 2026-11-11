# Round 2 roadmap: the TODO items without `***`

Date: 2026-11-11. Status: **draft, waiting on user decisions** (see each spec's "Decisions" table).

Round 1 (`docs/plans/00-orchestration.md`) owns every `***` item plus U4–U6 and P4. Round 2 starts once round 1's last wave merges into `integration/todo`. Where this roadmap and round 1 disagree, round 1 wins for anything it already shipped.

## Scope

Scope rules agreed 2026-11-11:
- Everything still open in `TODO.md` that round 1 does not cover.
- The ANTI settings spec is folded in as input. Only the parts U1/U2 do not already deliver are planned.
- Lua config is a layer **after** U2 (keeps D1: YAML themes now, Lua later).
- Question-mark and vague items get a spike + spec with a go/no-go gate. No build tasks before the gate.

| ID | TODO line | Kind | Size | Spec |
|---|---|---|---|---|
| R01 | steal the best ideas from opencode | spike | S | [R01](R01-opencode-ideas-spike.md) |
| R02 | add qwen code, oh my pi, cursor cli, grok build; auto-detect models | build | L | [R02](R02-more-agents-model-detect.md) |
| R03 | "master agent" project manager (top-level line) | dup | — | Covered by round-1 **S4** (overview: needs you / ready for review / completed). See note below. |
| R04 | settings + help as full-screen pages (ANTI) | build | M | [R04](R04-fullscreen-settings-help.md) |
| R05 | settings config in Lua | build | L | [R05](R05-lua-config.md) |
| R06 | per-workspace `.rex` folder (+ id, edge color, system-wide rex, sync, audit trail, CLI) | epic | XL | [R06](R06-workspaces.md) |
| R07 | multiplayer? | spike | S | [R07](R07-multiplayer-spike.md) |
| R08 | rex fork for research only? | spike | S | [R08](R08-research-mode-spike.md) |
| R09 | configurable board sections (ToDo … Done) | build | L | [R09](R09-board-sections.md) |
| R10 | usage meter top right | build | M | [R10](R10-usage-meter.md) |
| R11 | Jira / Linear sync? | spike | S | [R11](R11-issue-sync-spike.md) |
| R12 | loop workflows (epic) | epic | L | [R12](R12-loop-workflows.md) |
| R13 | (epic) insert mode auto-chooses model | epic | M | [R13](R13-auto-model.md) |

**R03 note.** S4 already plans the "master agent" as a daemon-side overview with buckets *needs you / ready for review / failed / working / completed* and an Ollama headline. It rejects a spawned Claude "master" on cost and cross-session prompt-injection grounds. The open choice is only whether to add a **read-only** Claude PM follow-up after S4 ships (decision R03-D1 below).

## Cross-feature dependencies

```
round 1 (all waves) ──┬─> R02 (needs U3 retired-tool guard, spawnSpec, A1 OutputObserver)
                      ├─> R04 (needs U1 command table + palette, U2 theme/SettingsHub)
                      │     └─> R05 (Lua layer on U2 store + SettingsHub)
                      │           └─> R06 W-config (.rex/config.lua uses R05 evaluator)
                      ├─> R09 (needs F1 boardGroups, T1 hidden sections, S4 wait_reason)
                      ├─> R10 (header right slot; S4 overview line placement)
                      └─> R13 (needs P1 wizard fields, R02 model catalog)
R06 ─> R11 (issue import lands as ToDo cards, needs R09 ToDo column)
R09 ─> R12 (loop steps move cards between stages)
U4 (round-1 border) ─> R06 edge color + workspace id
spikes R01, R07, R08, R11: any time; each ends at a user gate
```

## Waves (3 lanes, same per-task pipeline as round 1)

Lane A = TUI, B = daemon/registry/protocol, C = CLI/settings/features.

| Wave | Lane A | Lane B | Lane C |
|---|---|---|---|
| 0 | — | R02-1 live captures (qwen, omp, cursor-agent, grok) | spikes R01, R07, R08, R11 (docs only) |
| 1 | R04 full-screen pages | R02-2..6 registry + detect | R05-1..4 Lua evaluator + store layer |
| 2 | R09-1..4 stage model + view | R09-5..6 stage on wire/meta | R05-5..7 TUI lock, `rex config` |
| 3 | R10-3..5 meter view | R10-1..2 usage sources | R13 auto model |
| 4 | R06 epic (own orchestration file, after its spec gate) | | |
| 5 | R12 loops | | |
| 6 | approved spike follow-ups (R01/R07/R08/R11) | | |

## Per-task pipeline

Same as round 1 (`00-orchestration.md` → "Per-task pipeline"): worktree per task, test first, one commit per step, `make check` green, docs + CHANGELOG in the same step, independent reviewer, orchestrator `dev/sandbox.sh` check, merge in wave order. Never `make install`, never write real `~/.claude`, `~/.gemini`, `~/.config/rex`, no push, no PR until the user says so.

## Decisions

Each spec ends with a "Decisions" table. Recommended option is listed first. The orchestrator copies answers here once given.

| # | Decision | Answer |
|---|---|---|
| R03-D1 | Follow-up to S4 | (a) none, S4 is enough · (b) read-only Claude PM session reading `rex overview -json` after S4 |
