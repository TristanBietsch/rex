# R12 Loop workflows (epic)

## Summary
- Run an agent session in a loop until a goal is met, or run a fixed chain of steps (plan → build → review → fix) where each step is a rex session and the output of one feeds the next.
- Loops are owned by the daemon (they must survive the TUI closing), defined as data, visible on the board as one parent card with child step cards.

## Current behavior
- One prompt per session. `rex send` / `reply` can type into a live session. `init.lua` hooks (`rex.on`, `rex.send`) can script reactions to state changes in the daemon; this is the only automation today.
- Claude Code has its own in-session loop plugins (e.g. ralph-loop) that re-feed a prompt from a Stop hook; rex does not know about them.
- S4 classifies "ready for review"; T3 adds resume; R09 adds stages.

## Design

### Loop kinds (Decision D1)
- **Repeat**: same session, re-send the prompt (or a follow-up prompt) each time the agent stops, until a stop condition: a completion marker in output (`<promise>DONE</promise>` style regex), a check command exits 0 (e.g. `make check`), or max iterations / max wall time.
- **Chain**: ordered steps, each a new session (tool/model/prompt template), the next starts when the previous reaches done/review. Templates get `{{prev.description}}`, `{{prev.last_lines}}`, `{{cwd}}`, `{{loop.iteration}}`.

### Definition format (Decision D2)
```yaml
# ~/.config/rex/loops/fix-until-green.yaml
name: fix-until-green
kind: repeat
tool: claude
model: sonnet
prompt: "Run make check and fix the first failure. Stop when it passes."
until:
  check: ["make", "check"]     # run in session cwd, timeout 15m, exit 0 = done
  max_iterations: 8
  max_minutes: 120
```

### Safety
- Check commands come only from loop files the user wrote (or R06 trusted workspaces), never from agent output.
- Loops require `skip_permissions` off by default; a loop that would wait forever on a permission prompt pauses and alerts "needs you" instead of auto-answering.
- Hard caps: max iterations default 10, max wall time default 2 h, global max concurrent loops 2.

### Daemon
- `internal/daemon/loop`: `Runner` subscribes to store events, drives steps, persists loop state in `loops/<id>.json` (restart → paused, never auto-resumed).
- Wire: `StartLoop`, `PauseLoop`, `StopLoop` intents; `Loop` event; `SessionSummary.LoopID`, `LoopStep` (additive).

### UI
- Parent card `⟳ fix-until-green 3/8` with children indented; `p` pause/resume, `x` stop. `:loop <name>`; `rex loop ls|start|pause|stop|log`.

## Feature flag
- `loops` (off). Off = runner not started, loop files not read, `rex loop` / `:loop` refuse.
- Lives in Settings → Features (roadmap "Feature flags"). Its owning step adds an **off-is-inert** test: with the flag off, no goroutine/subprocess/network/file write happens and the feature's keys and commands answer "off: enable in Settings → Features".
- Flag lands in step R12-1.

## Plan
1. **R12-1 Loop file format + validator.** Tests: schema table, check command must be argv (no shell string), caps enforced.
2. **R12-2 Runner: repeat kind.** Tests with echo tool: completes on marker; stops at max iterations; check command exit codes; pause on needs_input with no auto answer; daemon restart → paused.
3. **R12-3 Runner: chain kind (deferred, D1).** Tests: step 2 receives step 1 description; failed step stops chain with state failed.
4. **R12-4 Wire + persistence.** Tests: late-joining TUI; old daemon `bad_intent` → "update daemon".
5. **R12-5 TUI cards + keys.** Goldens; R09 stage moves do not break loop children.
6. **R12-6 CLI + docs + SECURITY.md.**

## Review focus
- Agent prints the completion marker while quoting the instructions ("I will print DONE when finished"): match only on a line by itself / exact tag.
- Check command hangs: timeout kills its process group.
- User attaches and types during a repeat loop: runner pauses re-sends until the agent is idle again; never interleaves keystrokes.
- Cost runaway: caps hit → loop stops with a notice and a sound.
- Two loops in the same cwd editing the same files: warn at start.

## Decisions (answered 2026-11-11)

| # | Question | Answer |
|---|---|---|
| D1 | Kinds | Repeat-until in round 2; chains later (R12-3 deferred). |
| D2 | Format | YAML in `~/.config/rex/loops/` (+ `.rex/loops/` in trusted workspaces). |
| D3 | Stop default | Check command exits 0, plus max-iterations cap. |

## Build notes

Built: R12-1, R12-2, R12-4 and R12-6 for the **repeat** kind. See [loops.md](../../loops.md).

- **Out of scope, not built:** R12-3 (chains, deferred by D1) and **R12-5 (TUI loop cards and keys)**: round-1 U1 is restyling the TUI. The board shows a loop's session as an ordinary row; `SessionSummary.loop_id` / `loop_iter` and `Snapshot.loops` / the `Loop` event carry what the cards will need. `rex loop` is the only surface.
- Code: `internal/features/loop` (format, validator, check runner, runner), `internal/daemon/boot/loop.go` (Host over the server and store; flag wiring), `internal/daemon/server/loop.go` (intents), `internal/wire/protocol/loop.go`, `internal/surface/cli/loopcli`.
- `StartLoop` takes a file **name** (the stem), not a path: a client never names a path. The spec's `file|name` is name only. `.rex/loops/` in trusted workspaces (D2) is not read.
- Turn end: Claude needs an explicit `wait_reason` review (an unexplained needs_input waits for the hook); other tools use working to needs_input. A last line ending in `?` pauses the loop for both, using the same rule as the board (`overview.IsQuestion`).
- Pauses are either **automatic** (`needs_input`, `user_input`: the loop continues by itself after a clean turn end that followed your answer or input) or **held** (`user`, `daemon_restart`, `session_ended`, `send_failed`, `feature_off`: only `ResumeLoop` continues). A session that exits pauses the loop as `session_ended`, so a daemon shutdown leaves loops paused too.
- Resume of a loop whose session exited uses the server's Resume path (same id, resume args); if the tool cannot resume, `ResumeLoop` fails with `bad_state` and the loop can be stopped.
- A paused loop still counts toward the cap of 2 live loops.
- Marker validation also refuses a marker that matches a whole line of the prompts, since the agent echoes them.
- Known gap: a Claude turn shorter than one daemon poll may go unseen (no working state observed); `max_minutes` ends such a stalled loop. The cap "sound" in Review focus is not built (no TUI work).
