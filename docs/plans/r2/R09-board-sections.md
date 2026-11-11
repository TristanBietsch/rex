# R09 Configurable board sections (ToDo, In Progress, In review, Testing, Ready for Release, Done)

## Summary
- Today the board has three sections derived purely from process state: Needs you, Working, Done (`boardGroups`, `internal/surface/tui/board.go`).
- The TODO asks for workflow stages: **ToDo, In Progress, In review, Testing, Ready for Release, Done**, changeable in settings.
- Those are not process states. Two of them can be derived (In Progress ≈ working/needs you, In review ≈ S4's "ready for review"); the rest need a user-set **stage** on the session, and ToDo needs sessions that have not started yet.

## Current behavior (after round 1 F1/T1/S4)
- F1: `boardGroup{ID, Title, Count, Alert, Empty, Completed, Match}` table; all views read it; `resolveSection` maps refs.
- T1: `board_hidden_sections` setting and filter chips.
- S4: `wait_reason` on Claude sessions and `overview.Classify` buckets incl. "ready for review".
- No user-assigned stage field. No backlog: a session exists only once spawned.

## Design

### Stage model (Decision D1, recommended hybrid)
- New `SessionSummary.Stage string` (additive, persisted in meta). Empty = automatic.
- A section definition is data:
  ```yaml
  board_sections:            # setting, list; default = today's three
    - {id: todo,    title: "ToDo",              match: {stage: todo}}
    - {id: doing,   title: "In Progress",       match: {auto: [working, queued, needs_input]}, alert_on: needs_input}
    - {id: review,  title: "In review",         match: {auto: [review]}}            # S4 Classify == Review
    - {id: testing, title: "Testing",           match: {stage: testing}}
    - {id: release, title: "Ready for Release", match: {stage: release}}
    - {id: done,    title: "Done",              match: {auto: [done, failed, crashed]}, completed: true}
  ```
- Rule: a non-empty `Stage` wins; otherwise the first section whose `auto` list contains the session's derived bucket. Every session lands in exactly one section (fallback: last `completed` section), so nothing becomes invisible (same invariant F1 documents).
- Presets (Decision D2): `simple` (today's 3), `kanban` (the six above). Custom lists via YAML/Lua (R05).
- "Needs you" alerting survives as `alert_on: needs_input` inside In Progress, so the user still sees waiting agents in red.

### Moving cards
- `m` then a section number/`h`/`l` moves the selected card (sets `Stage`); `:stage <ref>`; `rex stage <sel> <ref>`; `M` clears to automatic.
- Moving into a `completed` section = mark done (existing Retire path). Moving out = T4's undo-complete.

### ToDo backlog (Decision D3)
- Recommended: a **draft** session: `NewSession` with `draft: true` stores prompt/tool/model/cwd in meta, state `draft` (new protocol state, additive), no process. `enter`/`s` on a draft spawns it. Survives daemon restarts (not remapped to crashed).

## Feature flag
- `board_preset` enum is the toggle: `simple` (default) = today's board, no stages, no drafts; `kanban` / `custom` turn on stages and drafts.
- Lives in Settings → Features (roadmap "Feature flags"). Its owning step adds an **off-is-inert** test: with the flag off, no goroutine/subprocess/network/file write happens and the feature's keys and commands answer "off: enable in Settings → Features".
- Flag lands in step R09-2.

## Plan
1. **R09-1 Section spec type.** `internal/catalog/board`: `Section`, `Match`, `Resolve(s SessionSummary, bucket overview.Bucket, secs []Section) int`. Tests: table over every state × stage; fallback invariant property test (every input maps to some index).
2. **R09-2 Setting + presets.** `board_preset` enum (`simple|kanban|custom`) + `board_sections` list (custom). Validation: unique ids, ≥1 completed section, ≤9 sections. Tests: invalid lists rejected with the offending entry.
3. **R09-3 TUI reads sections dynamically.** `boardGroups` built from the setting; F1 `Match` closures generated from `Resolve`. Header counts generalize from 3 fixed numbers to per-section counts (compact form when > 4). Tests: goldens for both presets at 120 and 80 columns; T1 hidden-sections still work with new ids.
4. **R09-4 Move keys + commands.** `m`/`M`, `:stage`. Tests: move to completed section retires; move out of Done unretires; move on old daemon (no Stage) shows "update daemon" notice.
5. **R09-5 Daemon Stage.** `SetStage` intent, persisted, patched. Tests: persist/restore; invalid stage id rejected (`bad_intent`); stage cleared on resume (T3) only if section is completed.
6. **R09-6 Drafts (if D3 = a).** `draft` state, `StartDraft` intent, spawn path through `spawnSpec`. Tests: drafts survive restart; draft start obeys concurrency cap; `rex ls` shows drafts.
7. **R09-7 CLI + docs.** `rex stage`, `rex new --draft`, `docs/tui.md`, `docs/protocol.md`, CHANGELOG.

## Review focus
- User deletes a custom section that sessions are staged into: those sessions fall back to automatic, not invisible.
- `needs_input` session parked in Testing: still alerts (badge + `tab` next-needs-you reaches it).
- Screensaver counts and `rex status` exit code (exits 1 while anything needs input) keep their meaning under the kanban preset.
- Mouse hit-testing (`board_mouse.go`) with 6 sections in a short terminal.
- Mixed versions: new TUI + old daemon must render with `simple` behavior.

## Decisions (answered 2026-11-11)

| # | Question | Answer |
|---|---|---|
| D1 | Stage source | Hybrid: auto from state; user can pin any card to any stage. |
| D2 | Default | `simple` (3 sections) stays default; `board_preset` in Settings switches to `kanban` or `custom`. |
| D3 | ToDo column | Draft sessions (new `draft` state, survives restarts). |
