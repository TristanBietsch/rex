# R19: READY edge cases (no READY unless the user is needed) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Claude session is READY only when the user is needed, never flaps READY↔WORKING on subagent results, notifications or compaction, and still reaches a real READY quickly.

**Architecture:** `ClaudeHooks` keeps one explicit main-thread state (turn / parked / question) and computes the effective state, wait and reason in one function (`effective`), with a logged reason for each change. Owed subagent reports (a background subagent ended, its result not yet delivered by `UserPromptSubmit`) keep a parked session WAITING for `wakeGrace`. Hook lines gain a few words (additive; old lines keep their R17 meaning).

**Tech Stack:** Go, Claude Code hooks (sh, no jq), table-driven replay tests.

**Spec:** this file, section "Bug, research and decisions". Follows [R17](R17-ready-subagent-flap.md); `docs/tui.md` READY / WAITING.

## Bug, research and decisions

User report (verbatim): "sometimes when a agent gets an update from a subagent, it will jump to ready without actually having anything to prompt them, then it will jump back to working."

The user's installed daemon predates R17, where every `Stop` was READY even with background subagents running; R17 fixes the exact report (replay test `TestClaudeHooks_R19ReportedSymptom`, Task 1). Research in the official hooks docs (code.claude.com/docs/en/hooks, sub-agents, 2026-11-12) found more causes:

1. **A background subagent's report re-enters the main loop through `UserPromptSubmit`** ("Claude Code also runs them on ... a background subagent reporting back to the session that started it"). Subagents spawned from the main conversation start in the background by default; the `Agent` call returns at once with `status: "async_launched"`.
2. **Notification types.** Only `permission_prompt`, `elicitation_dialog`, `elicitation_url_dialog`, `agent_needs_input` and `quota_auto_resume_stale` wait on the user. `auth_success`, `auth_storage_failure`, `elicitation_complete`, `elicitation_response`, `agent_completed`, `quota_auto_resume_fired`, `plugin_notification` are informational. R17 treats every type but `idle_prompt` as a question: READY at once, skipping the 2 s hold and the subagent hold.
3. **`SessionStart` with source `compact`** fires after an auto-compaction, mid-turn; R17's hook writes `needs_input` for it.
4. **`StopFailure`** (turn ended by an API error: rate limit, overload, auth) fires no `Stop`; rex is not hooked to it.
5. **`Stop` input has `background_tasks`** (in-flight tasks with `type` `subagent`, `shell`, `monitor`, ...), present when the task registry is reachable.
6. Nested subagents exist (a subagent's `Agent` call); the spawning subagent waits for them, so their result never reaches the main agent.

Decisions:

- Notification classes: question (above five, plus an absent type for old Claude versions) → READY at once, reason `input`, the open tool wait is kept (it resumes after the answer). `idle_prompt`, `quota_auto_resume_disabled` → turn ended (park). Everything else, including unknown future types → no state change, logged.
- `SessionStart` compact writes `compacted` instead of a state word: no state change.
- `StopFailure` writes the same lines as `Stop` (READY review, parks on subagents).
- Owed reports: a background subagent (not foreground, not nested) that ends while the main turn runs owes a report; one `prompt` line pays one. A parked session with an owed report or a pending wake stays WAITING for `wakeGrace` from the later of the Stop and the last end, then READY. Ends while parked keep R17's wake rule (any main-thread line pays them).
- `Agent`/`Task` `PostToolUse` with `async_launched` writes `wait- launch`: the wait closes and the subagents it started are background (an Esc no longer drops them).
- A subagent's `PreToolUse` of `Agent`/`Task` writes `nest+`: the next started subagent is nested and owes no report.
- `Stop` writes `bg-agents <n>` (count of `"type":"subagent"` entries) when `background_tasks` is present: running subagents above `n` (oldest sign of life first) are dropped as ended. A lost `SubagentStop` then costs `wakeGrace`, not 15 min.
- `UserPromptSubmit` writes `working\nprompt`; `prompt` itself closes the last turn's wait.
- A `agent~` (sign of life) for a subagent whose `agent-` already came is ignored; `agent+` (resume, same id) counts it again.
- Rejected: answer-key detection after a permission prompt (Enter / digit → WORKING). A deny answer ends the turn with no hook, indistinguishable from an approved long command, so it would hide a real READY for 15 min. Kept: READY (question) until the next hook.
- Rejected: background shells/monitors in `background_tasks` keeping the session WAITING. A dev server or `tail -f` never ends, so READY would never come. Kept as a known gap.

## Edge cases

| Case | Expected | R17 | R19 |
|---|---|---|---|
| Stop with background subagents, each report wakes the main agent | WAITING, READY once at the end | ok | ok |
| Background subagent ends while the main turn runs, report after Stop | no READY between | READY edge at Stop (2 s hold masks a fast wake) | owed report, WAITING ≤ 10 s |
| Subagent ends right at Stop (`agent-` just before `turn-done`) | no READY before its report | READY edge | owed report |
| Several end within wakeGrace | one READY after the last report | ok | ok |
| Report later than wakeGrace | READY, then WORKING, READY | same | accepted (bound) |
| Informational notification mid-turn (`elicitation_response`, `plugin_notification`, `auth_success`, `quota_auto_resume_fired`, `agent_completed`) | stays WORKING | READY at once, no hold | no change |
| Question while a Bash or Task wait is open | READY; WAITING after the answer's hook | wait cleared, WORKING after | wait kept |
| Permission prompt from a subagent | READY at once | ok | ok |
| Answering a permission prompt for a long command | WORKING | READY until PostToolUse | accepted gap (deny is silent) |
| Auto-compaction mid-turn | WORKING | READY (held 2 s) | no change |
| API error ends the turn (`StopFailure`) | READY | 30 s fallback, never with subagents | READY |
| Lost `SubagentStop` (crash, kill) while parked | READY soon | 15 min | `bg-agents` at Stop: wakeGrace |
| Subagent quiet 15 min but alive | WAITING | dropped, back on next tool call | same, accepted |
| Background `Agent` call (async_launched) then Esc | WAITING on it | subagent dropped as killed | kept |
| Nested subagent ends | no owed report | n/a | `nest+`, no report owed |
| Resumed subagent (same id) | counted again | ok | ok |
| `agent~` arriving after `agent-` | ended stays ended | resurrected 15 min | ignored |
| `/clear`, resume, startup | READY | ok | ok |
| Hook file truncated | state reset, prompts kept | ok | ok (owed reset too) |
| Daemon restart mid-turn | session reloads as crashed | n/a | n/a |
| Queued prompt typed while working | no READY at Stop | 2 s hold masks | same |
| Esc that closes a menu | WORKING | READY after 5 s (known gap) | same |
| Sleep / clock jump | timers unaffected | monotonic clock | same |
| Partial / unknown lines | ignored safely | ok | ok |
| Background Bash / Monitor at Stop | READY (may wake later) | READY then flap | accepted gap |
| Cron / `/loop` / cross-session message prompt | a new turn, READY after it | same | same (documented) |
| Codex, Qwen, Cursor, Grok, agy | prompt or 15 s still screen + 2 s hold | same | same (no subagent signal) |

## Global Constraints

- `wakeGrace = 10 * time.Second`, `escBound = 5 * time.Second`, `hookStaleWorking = 30 * time.Second`, `subagentQuiet = 15 * time.Minute`, ready hold 2 s: unchanged.
- Hook commands: one `printf` per hook, plain `sh`/`sed`/`tr`/`grep`, always exit 0.
- Old hook lines keep their R17 meaning (sessions spawned before an upgrade).
- No wire change. Don't touch `notify.go` (shared with the audio fix), `tui.go` view, R18 files.
- All R17 tests stay green.

## Review Focus

1. A `needs_input` word followed by `notify <info-type>` in the same write must not clear a tool wait or park. Test `TestClaudeHooks_R19InfoNotificationNoChange`.
2. Two ends, one batched report (`prompt` once): READY after wakeGrace, not never. Test `TestClaudeHooks_R19OwedReportBounded`.
3. `bg-agents` larger than the known set changes nothing. Test `TestClaudeHooks_R19BgAgentsReconcile`.
4. `wait- launch` with a foreground Task still running: no READY while subagents run. Test `TestClaudeHooks_R19LaunchKeepsCount`.
5. An `agent+` for an id that already ended counts again. Test `TestClaudeHooks_R19ResumedSubagent`.

---

### Task 1: Replay tests

**Files:** Create `internal/daemon/adapter/claude_hooks_r19_test.go`.

- [ ] **Step 1:** `TestClaudeHooks_R19ReportedSymptom` (the user's report, must pass on R17): prompt, `Agent` bg launch a and b, Stop; `agent- a`, report `working\nwait-\nprompt`, work, Stop (b runs); `agent- b`, report, Stop. Zero READY edges until the last Stop, then exactly one.
- [ ] **Step 2:** A table test `TestClaudeHooks_R19Sequences` over hook sequences (name, lines with clock steps, expected effective state after each step, expected READY edge count), one row per R19 cell of the table above.
- [ ] **Step 3:** Hook command tests running the real shell: Notification types, SessionStart compact, Stop with `background_tasks`, `PostToolUse` async_launched, subagent `PreToolUse` Agent, StopFailure.
- [ ] **Step 4:** `go test ./internal/daemon/adapter -run R19` → Step 1 passes, the rest fail. Commit `test(adapter): replay READY edge cases`.

### Task 2: One state model in claude_hooks.go

**Files:** Modify `internal/daemon/adapter/claude_hooks.go`.

- [ ] **Step 1:** `func (a *ClaudeHooks) effective(idle time.Duration) (st protocol.State, kind string, since time.Time, reason, why string)` is the only place that decides; `Detect`, `Waiting`, `WaitReason` call it; `why` changes are logged at Info (`adapter: claude state`, `why`).
- [ ] **Step 2:** Fields: `question bool`, `endReason string`, `wakes, reports int`, `lastEnd time.Time`, `nested map[string]bool`, `nestPending int`, `ended map[string]bool` (cap 256). `wakeAt` goes.
- [ ] **Step 3:** `read` groups a `needs_input` line with a following `notify` line; `apply` takes the new words `compacted`, `nest+`, `bg-agents`, `wait- <kind>`.
- [ ] **Step 4:** Hook commands per the decisions; `StopFailure` added to `claudeHookStates`.
- [ ] **Step 5:** `go test ./internal/daemon/...` green. Commit `fix(adapter): claude READY only when the user is needed`.

### Task 3: Docs, review, gate

- [ ] `docs/tui.md` READY rules and known gaps; `CHANGELOG.md` `[Unreleased]` Fixed; roadmap row R19.
- [ ] Separate reviewer agent; fix findings; merge `integration/todo`; `make check`.
