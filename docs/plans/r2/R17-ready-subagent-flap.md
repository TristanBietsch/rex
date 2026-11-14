# R17: READY only when the agent is truly ready (subagent flap fix) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Claude session waiting on subagents shows in WAITING (working, `waiting_on: subagent`, no sound) and turns READY once, after the last subagent ends and the main agent stops.

**Architecture:** The Claude hook adapter derives an *effective* state from the raw hook word plus the subagent set, a "parked" flag (main turn ended) and a short wake grace. The PTY supervisor holds a working→needs_input change for 2 s before it publishes it (a question Notification skips the hold). The TUI plays the attention sound once per user prompt (new `prompts` counter from the `UserPromptSubmit` hook) unless a new question arrives.

**Tech Stack:** Go, Claude Code hooks (sh), vt10x PTY supervisor, Bubble Tea TUI.

**Spec:** user report + decisions in this file (section "Bug and decisions"); `docs/tui.md` (READY / WAITING / WORKING / DONE).

## Bug and decisions

User report: while an agent waits for subagents it plays the READY sound, then goes back to working, and loops.

Root cause (confirmed by failing tests in Task 1):

1. `ClaudeHooks.Detect`: `last == working && idle >= 15s` → needs_input, ignoring running subagents and the open tool wait. The main agent's screen can stay still for 15 s on a subagent; the subagent's next `PostToolUse` writes `working` and flips it back.
2. `Stop` maps to needs_input even while background subagents run. Each subagent completion wakes the main agent: working → Stop → READY + sound, once per subagent.

Decisions (user):

- While subagents run after the main turn ended, or while any subagent / tool wait is open: working with `waiting_on = subagent` (WAITING, no sound). READY once, after the last subagent ends and the main agent stops.
- READY only when: (a) a Notification other than `idle_prompt`; (b) Stop with zero running subagents and no open tool wait; (c) the stale fallback with no running subagents and no open wait. No cheap screen check exists (Claude's input box is on screen while it works too), so (c) uses a longer bound: 30 s silence (was 15 s), and 15 min (`subagentQuiet`) when a tool wait is open.

- 2026-11-11 decision (user: fix before merge): an Esc during an open Bash or foreground Task wait left the session WORKING for up to 15 min. Fix: the daemon sees the interrupt key (Esc, Ctrl+C, also kitty / modifyOtherKeys forms) on the attach input path and tells the adapter (`InterruptObserver.UserInterrupt`); with no main-thread hook line for `escBound = 5 s`, the turn counts as interrupted: the tool wait closes, foreground subagents of the open Task wait with no sign of life since the key are dropped, and the session parks (READY at once with nothing left, WAITING on background subagents, then READY once through the wake grace). Rejected: (a) screen check, Claude's input box shows while it works and background tasks animate, so it could never confirm or would stall; (c) hooks, the docs say Stop does not run on a user interrupt and cancelling a running tool fires no PostToolUseFailure (its `is_interrupt` is only for aborts that surface as failures), and `idle_prompt` only fires while the user seems away.

## Global Constraints

- Hold before publishing working→needs_input: `readyHold = 2 * time.Second`, injectable time in tests.
- Wake grace after the last background subagent ends: `wakeGrace = 10 * time.Second`.
- Stale fallback: `hookStaleWorking = 30 * time.Second`; with an open wait and no subagents: `subagentQuiet` (15 min).
- Hook lines stay one `printf` per hook; new line `prompt` from `UserPromptSubmit`.
- Wire change is additive: `SessionSummary.Prompts int json:"prompts,omitempty"`.
- Don't edit `tui.go` view, `keymap.go`, R04/R05/P2 files.

## Review Focus

1. Esc interrupt during a Bash wait (no Stop, wait open): READY after 15 min of silence, not never. Test in Task 2. Since 2026-11-11: READY `escBound` (5 s) after the Esc key the daemon forwarded; tests `TestClaudeHooks_Esc*`.
2. A permission prompt from a subagent while parked: READY at once (question). Test in Task 2.
3. Hook file truncated (resume): prompt counter keeps counting, never goes back. Test in Task 2.
4. Sessions started before this change (no `prompt` lines): `prompts` stays 0, TUI falls back to debounce only. Test in Task 4.
5. Done transitions and needs_input→working are never held. Test in Task 3.

---

### Task 1: Failing replay tests (confirm root cause)

**Files:**
- Create: `internal/daemon/adapter/claude_hooks_ready_test.go`

- [ ] **Step 1:** Write tests replaying hook lines with an injected clock:
  - `TestClaudeHooks_ForegroundSubagentStillScreenStaysWorking`: `working`, `wait+ subagent`, `agent+ a`; `Detect(nil, 20*time.Second)` == working; `Waiting()` kind == `subagent`.
  - `TestClaudeHooks_StopWithBackgroundSubagentsIsWaiting`: `agent+ a`, `needs_input`, `turn-done`; Detect == working, Waiting == subagent, WaitReason == "".
  - `TestClaudeHooks_SubagentWakeThenStopReadyOnce`: full sequence (Stop with a, b running; `agent- a`; wake `working`; `needs_input`/`turn-done`; `agent- b`; wake; final Stop). Count Detect results per tick: exactly one working→needs_input edge, at the end.
  - `TestClaudeHooks_QuietSubagentEndsParkedWait`: parked with one agent, clock + `subagentQuiet` + `wakeGrace` → needs_input, Waiting empty.
- [ ] **Step 2:** `go test ./internal/daemon/adapter -run 'ForegroundSubagent|BackgroundSubagents|ReadyOnce|QuietSubagentEnds'` → FAIL (needs_input where working expected).
- [ ] **Step 3:** Commit `test(adapter): replay subagent READY flap`.

### Task 2: Adapter fix

**Files:** Modify `internal/daemon/adapter/claude_hooks.go`; update `claude_hooks_wait_test.go`, `wait_reason_test.go`, `claude_hooks_test.go` where they pin the old behavior.

**Interfaces — Produces:** `type PromptCounter interface { Prompts() int }` implemented by `*ClaudeHooks`.

- [ ] **Step 1:** Add fields `parked bool`, `parkedSince time.Time`, `wakeAt time.Time` (zero = no wake expected), `prompts int`. `turn-done` sets parked (since = the subagent wait's start if one was open, else now); `wait+`/`wait-` clear it; any state word clears `wakeAt`; an agent set dropping to 0 while parked sets `wakeAt = now`; `prompt` line increments `prompts` (not reset on truncation).
- [ ] **Step 2:** `effective(idle)`: needs_input word, reason ≠ input, and (running agents > 0 or `now-wakeAt < wakeGrace`) → working. Working word: stale only if no agents, no wake, and idle ≥ 30 s (no wait) or ≥ `subagentQuiet` (wait open). `Waiting()` and `WaitReason()` use the effective state; parked + agents → `subagent`, `parkedSince`.
- [ ] **Step 3:** `UserPromptSubmit` hook prints `working\nwait-\nprompt\n`.
- [ ] **Step 4:** Tests from Task 1 + Review Focus 1–3 pass; `go test ./internal/daemon/adapter`. Commit `fix(adapter): claude stays working while subagents run`.

### Task 3: Supervisor flap guard + prompts on the wire

**Files:** Create `internal/daemon/pty/ready_hold.go` + `_test.go`; modify `supervisor.go` (classification tick), `internal/daemon/state/{session.go,store.go}`, `internal/wire/protocol/events.go`.

**Interfaces — Produces:** `func (h *readyHold) admit(current, next protocol.State, question bool, now time.Time) (publish bool, why string)`; `SupervisorConfig.ReadyHold time.Duration` (default 2 s); `Store.SetPrompts(id string, n int) error` broadcasting patch `prompts`.

- [ ] **Step 1:** Unit tests: hold 2 s then publish (`why == "held"`); flip back inside 2 s cancels (`"flap"` logged, no publish); question publishes at once (`"question"`); other transitions publish at once.
- [ ] **Step 2:** Implement; supervisor asks `reasons.WaitReason() == protocol.WaitInput` for `question`, logs `pty: state transition ... reason=<why>`, and publishes wait reason "" while a hold is pending.
- [ ] **Step 3:** `go test ./internal/daemon/...`; commit `fix(pty): hold READY 2s before publishing`.

### Task 4: TUI sound once per turn

**Files:** Modify `internal/surface/tui/notify.go`, `model.go` (patch apply for `prompts`, wait_reason→input hook), `notify_test.go`.

- [ ] **Step 1:** Tests: two attention entries with the same `prompts > 0` → one sound; a `wait_reason: input` patch in needs_input → sound; `prompts` 0 → old behavior.
- [ ] **Step 2:** Implement `rungPrompt map[string]int`; commit `fix(tui): attention sound once per turn`.

### Task 5: Other adapters, docs, CHANGELOG

- [ ] heuristic/osc stale silence: document (no subagent signal; covered by the hold).
- [ ] Background Bash: hook JSON has no completion event → known gap in `docs/tui.md`.
- [ ] `docs/tui.md` READY definition; CHANGELOG `[Unreleased]` Fixed; roadmap row R17.
- [ ] Review by a separate agent, fix findings, merge `integration/todo`, `make check`.

## Outcome notes

- Other adapters (`heuristic.go` for codex, qwen, cursor, grok; `osc_status.go` for agy): not the same class. They have no subagent or tool-wait signal; a still screen (15 s) or the prompt match is their only READY signal, and their agents animate a spinner while busy. They get the 2 s supervisor hold and the TUI debounce; no per-turn gate (no prompt count).
- Background Bash (`run_in_background`): PreToolUse exposes `tool_input.run_in_background`, but no hook fires when the command ends, so it cannot be tracked like a subagent. Documented as a known gap in `docs/tui.md`; the second READY (when Claude wakes on the result) is silent because the prompt count did not change.
- Review fixes: a question no longer marks the turn as rung (the READY after the answer chimes); a parked session whose subagent wrote working words and whose main agent does not wake within `wakeGrace` turns READY (review); `parkedSince` starts at the Task wait; `prompt` unparks; the hold resets on a tick with no adapter answer.
