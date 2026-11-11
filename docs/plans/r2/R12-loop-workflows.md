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

## Plan
1. **R12-1 Loop file format + validator.** Tests: schema table, check command must be argv (no shell string), caps enforced.
2. **R12-2 Runner: repeat kind.** Tests with echo tool: completes on marker; stops at max iterations; check command exit codes; pause on needs_input with no auto answer; daemon restart → paused.
3. **R12-3 Runner: chain kind** (if D1 includes chains). Tests: step 2 receives step 1 description; failed step stops chain with state failed.
4. **R12-4 Wire + persistence.** Tests: late-joining TUI; old daemon `bad_intent` → "update daemon".
5. **R12-5 TUI cards + keys.** Goldens; R09 stage moves do not break loop children.
6. **R12-6 CLI + docs + SECURITY.md.**

## Review focus
- Agent prints the completion marker while quoting the instructions ("I will print DONE when finished"): match only on a line by itself / exact tag.
- Check command hangs: timeout kills its process group.
- User attaches and types during a repeat loop: runner pauses re-sends until the agent is idle again; never interleaves keystrokes.
- Cost runaway: caps hit → loop stops with a notice and a sound.
- Two loops in the same cwd editing the same files: warn at start.

## Decisions

| # | Question | Options (recommended first) |
|---|---|---|
| D1 | Loop kinds | (a) repeat-until first, chains in a later phase · (b) both now · (c) chains only |
| D2 | Definition format | (a) YAML files in `~/.config/rex/loops/` (and `.rex/loops/` with R06) · (b) Lua functions via R05/init.lua (`rex.loop{...}`) · (c) only ad-hoc flags: `rex loop start --until "make check" --max 8 -- "prompt"` |
| D3 | Stop condition default | (a) check command exit 0, with max-iterations cap · (b) completion marker in output · (c) user marks done (loop only re-sends on stop) |
