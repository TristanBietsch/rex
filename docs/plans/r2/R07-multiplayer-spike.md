# R07 Spike: multiplayer

## Goal
TODO: "multiplayer?". Decide whether more than one person (or one person on several devices) should see or drive the same rex board, and how.

## Constraints from today's design
- The daemon listens on a Unix socket only, 0600, single user. SECURITY.md treats socket access as full control (it can type into every agent with skip-permissions possibly on). Any network exposure is a large security change.
- Agents run with the owner's credentials and file access. A second person "driving" an agent acts as the owner.

## Options to evaluate
| Option | What it is | Risk | Size |
|---|---|---|---|
| A. Read-only web/phone view | Daemon serves an HTTP+SSE read-only board on localhost; reach it via SSH tunnel or Tailscale. No input path. | low | M |
| B. Same user, several devices | Remote TUI over SSH to the machine running rex (works today) + document it; maybe `rex attach --ssh host` helper | low | S |
| C. Shared board, multiple users | Daemon TCP listener with per-user keys, roles (viewer / operator), audit of who typed what | high | XL |
| D. Pairing on one session | tmux-style shared attach: two clients attached to one PTY (multiple clients already supported); add presence + "who's typing" | medium | M |

## Method
1. Threat model each option in one page (who can reach it, what they can do, how keys are managed).
2. Prototype A for 2 h on a branch (local only) if the user picks A.
3. Check overlap with R06-D4 (sync) and R01 (share links).

## Exit
- Recommendation + threat model in this file. **Gate:** user approves an option before any build spec.

## Decisions (answered 2026-11-11)

| # | Question | Answer |
|---|---|---|
| D1 | Meaning | **Deferred.** Treated as a future epic; no spike in round 2. |
