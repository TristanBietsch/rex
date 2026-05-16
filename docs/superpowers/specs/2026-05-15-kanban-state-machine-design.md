# Kanban state machine — design

**Status:** draft
**Date:** 2026-05-15
**Scope:** fix lifecycle-state transitions for kanban sessions; introduce a manual completion signal; surface terminal states on the board.

## Problem

The TUI groups sessions into three columns: **Needs input**, **Working**, **Completed**. The underlying `protocol.State` machine has six values (`queued`, `working`, `needs_input`, `done`, `failed`, `crashed`). The current implementation has three bugs that make the kanban dishonest:

1. **`needs_input` is a one-way trap.** `internal/pty/supervisor.go:289` reads:
   ```go
   if next != current && next != protocol.StateWorking {
       _ = s.cfg.Store.Transition(sess.ID, next)
   }
   ```
   This explicitly drops every transition back to `working`. So when an agent prints a prompt → user replies → agent resumes generating, the adapter correctly classifies the new state as `working` but the supervisor swallows it. The row stays in **Needs input** forever.

2. **No `done` detection for interactive agents.** `internal/adapter/heuristic.go` (codex, gemini, ollama) only ever returns `working` or `needs_input`. The only path to `done` for these agents is **child process exit** (`supervisor.go:252-275`), and interactive CLIs don't exit between turns — they sit at their prompt. A finished task is indistinguishable from a pending question.

3. **`failed` and `crashed` sessions are invisible.** `internal/tui/board.go:73-76` groups by exactly three states. Sessions in `failed` (✕) or `crashed` (○) have markers defined but no column to live in. Once an agent dies the user can no longer see it on the board.

## Goals

- Make every documented transition actually happen: `working ↔ needs_input`, `working ↔ done` (the latter for adapters that have a structured signal).
- Give the user a single, explicit way to say "I'm done with this session" that works regardless of agent type.
- Surface terminal sessions in the **Completed** column with the existing markers preserved as failure-mode indicators.
- Preserve every existing behavior the change isn't trying to fix (exit-code propagation, `rex rm` semantics, transcript persistence, daemon-restart crash recovery).

## Non-goals

- No protocol break. Wire format and persistence stay backward-compatible.
- No state-stability / debounce layer. The existing `idle_ms` gating in adapters is sufficient until we observe real flapping.
- No automatic heuristic for "completion" on agents whose prompt is indistinguishable between "awaiting" and "just finished" (codex, gemini, ollama). Auto-done is opt-in per tool via `done_regex`, or via the structured Claude adapter. The fallback for everything else is the new manual completion gesture.
- No richer state model (`(lifecycle, activity)` tuple). Considered and rejected as out of scope.

## State machine

```
            ┌────── new visible output ──────┐
            ▼                                 │
   queued ──▶ working ◀───────▶ needs_input  │   alive (supervisor goroutine running, PTY open)
              │   ▲                  │        │
              │   └── new output ────┘        │
              ▼                               │
            done (auto-sticky) ◀──────────────┘

   Terminal (supervisor exits, no further transitions possible):
   ─ alive ── user presses `c` / `rex complete` ─────────▶ done    (clean shutdown via CompleteCh)
   ─ alive ── child exits, code 0 ────────────────────────▶ done
   ─ alive ── child exits, code ≠ 0 ──────────────────────▶ failed
   ─ alive ── ctx cancel (e.g. `rex rm`, daemon shutdown) ─▶ failed
   ─ found in alive state on daemon restart ──────────────▶ crashed
```

**Auto-done vs manual-done is distinguished by supervisor lifetime, not a flag.** Auto-done sessions still have an alive supervisor and PTY; new visible output flips them back to `working`. Manual-done sessions have killed the PTY and exited the supervisor; no goroutine remains to emit further transitions, so the row is frozen in **Completed**.

This means the only invariant that needs to hold for "manual completion is terminal" is: **once the supervisor goroutine returns, nobody else writes to `sess.State`.** That is already true in today's code.

## Component-by-component changes

### 1. `internal/pty/supervisor.go` — the load-bearing fix

**Transition rule.** Replace lines 288-291:
```go
current, ok := s.cfg.Store.CurrentState(sess.ID)
if !ok {
    continue
}
if next != "" && next != current {
    _ = s.cfg.Store.Transition(sess.ID, next)
}
```
The `next != current` dedupe handles both "don't broadcast spam on repeated `working` ticks" and "allow `done → working` resurrection." No state-specific filtering.

**Manual-completion path.** Add `CompleteCh chan struct{}` to `SupervisorConfig`. The `select` block gains a new case alongside `<-ctx.Done()` and `<-errc`:
```go
case <-s.cfg.CompleteCh:
    slog.Info("pty: complete signal received, killing child cleanly",
        "session", sess.ID, "pid", cmd.Process.Pid)
    _ = cmd.Process.Kill()
    <-errc          // drain the reader goroutine
    _ = cmd.Wait()  // reap the child process; mirrors the natural-exit branch at line 251
    if err := s.cfg.Store.Transition(sess.ID, protocol.StateDone); err != nil {
        return err
    }
    if err := state.WriteMeta(s.cfg.StateDir, sess); err != nil {
        return fmt.Errorf("write final meta: %w", err)
    }
    return nil
```
This mirrors the existing process-exit handler (line 252-275) with two corrections review surfaced:
1. **`cmd.Wait()` is required** to reap the child. Without it the PID lingers until `sessCtx` is canceled (which may never happen for a manually-completed session).
2. **`Store.Transition` runs before `WriteMeta`** so the lock-owning path writes `sess.State` instead of an unlocked bare assignment racing with concurrent readers (`CurrentState`, `Summary()`). `WriteMeta` then reads the post-transition snapshot. Subscribers may observe the event slightly before the meta file is fully written, but the in-memory state is canonical; a meta-write failure surfaces as a supervisor error (same as the existing exit handler).

The existing exit handler at lines 252-275 retains its bare `sess.State = final` write — that race predates this work and fixing it is out of scope here.

### 2. `internal/adapter/heuristic.go` — opt-in auto-done

Extend `HeuristicCLI` with an optional second regex:
```go
type HeuristicCLI struct {
    prompt *regexp.Regexp
    done   *regexp.Regexp   // nil = never auto-done
    idle   time.Duration
}
```
Classification order: idle gate → `done` → `prompt` → `working`.
```go
func (h *HeuristicCLI) Detect(window []byte, idle time.Duration) protocol.State {
    if idle < h.idle {
        return protocol.StateWorking
    }
    // ...existing window prep (cursor-position replace + ansi.Strip)...
    if h.done != nil && h.done.MatchString(clean) {
        return protocol.StateDone
    }
    if h.prompt.MatchString(clean) {
        return protocol.StateNeedsInput
    }
    return protocol.StateWorking
}
```

### 3. `internal/adapter/claude.go` — bump silence heuristic

The Claude structured adapter already emits `StateDone` on `type:"result"` and resets `a.last = StateWorking` on the next `assistant`/`user` message (claude.go:51-56). The supervisor fix in §1 is what unblocks the resurrection round-trip end-to-end.

**However**, the existing 5-second silence heuristic at claude.go:61-63 becomes a flapping source under the new transition rule:
```go
if a.last == protocol.StateWorking && idle > 0 && time.Since(a.lastSeen) > 5*time.Second {
    return protocol.StateNeedsInput
}
```
This was harmless before because the supervisor swallowed the regression back to `working` — so once Claude flipped to `needs_input` via this heuristic, it stuck. With the new rule, a deep think will cycle `working → needs_input → working → needs_input` on every quiet beat past 5s and every burst of new assistant chunks. Users will see Claude rows oscillate during long thinks.

**Mitigation**: bump the threshold from `5*time.Second` to `30*time.Second`. Permission-prompt detection latency grows from 5s to 30s — acceptable trade for stable rows during normal thinking. Document the threshold as a tuning knob; if real usage shows flapping at 30s too, we add the per-session stability layer (Approach B from the brainstorm) at that point.

### 4. `internal/registry/types.go` — registry schema

Add the new optional field:
```go
type DetectConfig struct {
    Kind        string `yaml:"kind"`
    PromptRegex string `yaml:"prompt_regex,omitempty"`
    DoneRegex   string `yaml:"done_regex,omitempty"`
    IdleMs      int    `yaml:"idle_ms,omitempty"`
}
```

### 5. `internal/registry/loader.go` — compile + validate

Compile `DoneRegex` when non-empty. Required-field validation at line 155 stays as-is (`prompt_regex + idle_ms`); `done_regex` is opt-in. Invalid syntax surfaces with a `tool %q: done_regex compile failed: %w` error.

### 6. `internal/registry/builtin.yaml` — leave empty

No built-in tool gets `done_regex`. The prompts of codex/gemini/ollama look identical whether the agent just finished a turn or is awaiting an answer; encoding a regex would produce false positives. Claude is covered by its structured adapter. Users who write their own tool entries can opt in.

### 7. `internal/state/store.go` — race-free state read

Add a `CurrentState` accessor so the supervisor can read state under the existing lock:
```go
func (s *Store) CurrentState(id string) (protocol.State, bool) {
    s.mu.RLock()
    sess, ok := s.sessions[id]
    s.mu.RUnlock()
    if !ok {
        return "", false
    }
    sess.mu.RLock()
    defer sess.mu.RUnlock()
    return sess.State, true
}
```
Replaces the pre-existing unlocked read at `supervisor.go:288`.

### 8. `internal/protocol/intents.go` — new intent

```go
IntentComplete = "Complete"

type Complete struct {
    SessionID string `json:"session_id"`
}
```

### 9. `internal/server/server.go` — parallel to stop plumbing

Mirror the existing `RegisterStop`/`UnregisterStop`/`StopSession` (server.go:269-296) for completion:
```go
completeMu    sync.Mutex
completeFuncs map[string]func()

func (s *Server) RegisterComplete(id string, fn func()) { /* mirror RegisterStop */ }
func (s *Server) UnregisterComplete(id string)          { /* mirror UnregisterStop */ }
func (s *Server) CompleteSession(id string)             { /* mirror StopSession */ }
```

### 10. `internal/server/client.go` — dispatch + wiring

Two changes:

**(a) New intent handler**, modeled on the existing `IntentDelete` case (near client.go:81):
```go
case protocol.IntentComplete:
    var p protocol.Complete
    if err := json.Unmarshal(env.Data, &p); err != nil { /* ErrCodeBadIntent */ }
    if _, ok := srv.cfg.Store.Get(p.SessionID); !ok { /* ErrCodeUnknownSession */ }
    srv.CompleteSession(p.SessionID)
    /* Ack */
```

**(b) Wire `CompleteCh` in `handleNewSession`** alongside the existing `RegisterStop` (near client.go:300-303):
```go
completeCh := make(chan struct{}, 1)  // buffered so non-blocking send always wins
cfg.CompleteCh = completeCh
srv.RegisterComplete(sess.ID, func() {
    select { case completeCh <- struct{}{}: default: }
})
defer srv.UnregisterComplete(sess.ID)
```
Buffered channel of size 1 + non-blocking send means a second `CompleteSession` call is a safe no-op even if the supervisor hasn't drained the first signal yet.

### 11. `internal/client/client.go` — peer method

```go
func (c *Client) Complete(id string) error {
    // send Intent envelope { Type: "Complete", Data: Complete{SessionID: id} }
    // wait for matching Ack / ErrorEvent
}
```

### 12. `internal/cli/complete.go` — new CLI command

Modeled on `internal/cli/archive.go`:
```go
func RunComplete(args []string) error {
    // parse --socket flag, exactly one selector required
    // ResolveSelector → client.Complete(id)
    // exit codes: 0 success, 2 not found, 3 ambiguous
}
```

### 13. `cmd/rex/` + `internal/cli/help.go` — register command

Add `complete` to the top-level dispatcher and the `rex --help` listing.

### 14. `internal/tui/keymap.go` + `internal/tui/update.go` — `c` keybind

Bind `c` (single keystroke; the existing `dd` for delete and `a` for archive leave it free) to a new `KeyComplete` message. Handler reads `m.SelectedID` and calls `client.Complete(id)`. No confirmation dialog: the action is observable (row flips to ●, transcript preserved on disk) and matches the directness of `dd`/`a`.

### 15. `internal/tui/board.go` + `internal/tui/keymap.go` — shared grouping

**Critical**: `filterByState` is called from **four** sites that all build group lists from the same three-state slice. If we only fix `board.go`, failed/crashed rows render in the **Completed** column but navigation breaks: `j`/`k` skip them, `selectedBoardLine` returns -1 (breaks scroll-follow), and `3` (jump to Completed) misses them.

Call sites that must share one source of truth:
- `board.go:73-76` — renders the three columns
- `keymap.go:11` (`orderedSessions`) — flat list used by `j`/`k` row movement
- `keymap.go:67-69` (`selectedBoardLine`) — maps `SelectedID` to a board row index
- `keymap.go:126` (`jumpToSection`) — `1`/`2`/`3` key jumps

**Fix**: introduce one shared grouping in `internal/tui/board.go`:
```go
type boardGroup struct {
    Title string
    Match func(protocol.State) bool
}

var boardGroups = []boardGroup{
    {"Needs input", func(s protocol.State) bool { return s == protocol.StateNeedsInput }},
    {"Working",     func(s protocol.State) bool { return s == protocol.StateWorking }},
    {"Completed",   func(s protocol.State) bool {
        return s == protocol.StateDone || s == protocol.StateFailed || s == protocol.StateCrashed
    }},
}

func filterByGroup(sessions []protocol.SessionSummary, g boardGroup, filter string) []protocol.SessionSummary {
    out := make([]protocol.SessionSummary, 0, len(sessions))
    for _, s := range sessions {
        if !g.Match(s.State) {
            continue
        }
        if filter != "all" && filter != "" && s.ToolID != filter {
            continue
        }
        out = append(out, s)
    }
    return out
}
```
All four call sites switch to `boardGroups` + `filterByGroup`. The old `filterByState` becomes dead code and is deleted in the same commit.

For `jumpToSection`: the `1`/`2`/`3` keys map to indices into `boardGroups` (was: `StateNeedsInput`/`StateWorking`/`StateDone` constants). Behavior is identical for the existing three states; failed/crashed now scroll into view alongside `done` when the user hits `3`.

Markers (●/✕/○) stay distinct within the Completed column so users can still see failure modes at a glance.

### 16. Documentation

- `docs/registry.md`: document `done_regex` field and precedence (`done` > `prompt`).
- `docs/tui.md`: add `c` to the keybind table next to `dd` / `a`.
- `docs/cli.md`: add `rex complete <sel>` row with exit codes.
- `docs/protocol.md`: add `IntentComplete` to the intent vocabulary.

## Edge cases

- **`c` on `queued`.** Supervisor hasn't entered the `select` yet (it's between `Store.Transition(working)` at line 97 and the ticker setup). Buffered `CompleteCh` will hold the signal; the supervisor consumes it on first select iteration. Worst case the spawn fails first (ctx-canceled by some other path) and we land in `StateFailed` — acceptable race, not a correctness bug.
- **`c` on auto-done.** PTY is still alive. Kill it, transition to `done` (already there — redundant but harmless broadcast). Manual-done becomes terminal because the supervisor exits.
- **`c` on `failed`/`crashed`/manual-done.** No registered completion func (it was unregistered on supervisor exit). `CompleteSession` is a no-op. Server responds with `Ack`; this matches existing `StopSession` semantics for already-exited sessions.
- **Daemon restart mid-auto-done.** `persist.go:83-85` only marks `queued/working/needs_input` as crashed. `done` is preserved as-is — correct: the PTY died with the daemon, so the row is genuinely terminal now, indistinguishable from manual-done.
- **Adapter ticker races with completion.** If the ticker fires just as `CompleteCh` is signaled, the select picks one. The transition rule is idempotent (`next != current`); duplicate transitions to `done` are harmless.
- **Claude `result` followed by `assistant`.** Already handled by `claude.go` setting `a.last = StateWorking` on the next assistant message. The supervisor fix makes this round-trip end-to-end.
- **`rex rm` on a session that's mid-completion.** Both paths converge on supervisor exit; `Remove` then deletes the entry. State during teardown is fleeting and not observable.

## Tests

### `internal/adapter/heuristic_test.go`
- `done_regex` matches → `StateDone`
- `done_regex` empty (zero value) → existing behavior unchanged
- both regexes match → `StateDone` wins
- `done_regex` would match but `idle < idle_ms` → `StateWorking` (idle gate still applies)

### `internal/pty/supervisor_test.go`
- `working → needs_input → working` ← the bug the user reported; without this test it can silently regress
- `working → done → working` (Claude-style resurrection via structured adapter)
- `needs_input → done → needs_input` (theoretical but cheap)
- `CompleteCh` fires while session is `working` → child killed, final state `done`, return nil
- `CompleteCh` fires while session is `needs_input` → same outcome
- `CompleteCh` fires while session is auto-`done` → same outcome
- `ctx.Cancel()` → final state `failed` (existing behavior preserved; regression guard)
- Process exits with code 0 → `done`
- Process exits with code ≠ 0 → `failed`, `ExitCode` populated

### `internal/state/store_test.go`
- `CurrentState` returns `(state, true)` for existing session, `("", false)` for unknown
- Race test: N goroutines calling `CurrentState` concurrently with `Transition`; expect clean run under `-race`

### `internal/server/server_test.go`
- `RegisterComplete` + `CompleteSession` → registered closure invoked exactly once
- `UnregisterComplete` → subsequent `CompleteSession` is a no-op
- `CompleteSession` on unknown id → no-op (matches `StopSession` semantics)

### `internal/server/e2e_test.go`
- Spawn an echo-style session, send `IntentComplete`, observe `SessionUpdated{state: done}`, verify the session no longer accepts input
- Spawn a stub adapter that returns `needs_input`, then `working`; assert the row regresses to `working` (the user-visible bug)

### `internal/tui/snapshot_test.go`
- Snapshot with one session per state; assert `failed`/`crashed` render under **Completed** with markers `✕` / `○`

### `internal/cli/complete_test.go`
- Selector resolves → intent sent → exit 0
- Unknown selector → exit 2
- Ambiguous selector → exit 3

### `internal/registry/loader_test.go`
- Loader accepts `done_regex` set; valid regex compiles
- Loader rejects invalid regex syntax with a clear error
- Loader accepts entries without `done_regex` (backward compat)

## Sequencing

Suggested execution order for the implementation plan, dependencies-first:

1. **Foundations** (no behavior change): `Store.CurrentState`, `IntentComplete` + `Complete` type
2. **Supervisor**: `CompleteCh` field, new select case, the transition-rule fix
3. **Adapter**: `HeuristicCLI.done` field, registry schema + loader, `Detect` precedence
4. **Server**: `RegisterComplete`/`UnregisterComplete`/`CompleteSession`, handler dispatch, `handleNewSession` wiring
5. **Client + CLI**: `client.Complete`, `internal/cli/complete.go`, dispatcher registration, help text
6. **TUI**: board predicate change, `c` keybind, snapshot test update
7. **Docs**: registry / tui / cli / protocol

Each numbered group lands as its own commit. Tests live with the code they cover and land in the same commit.

## Risks

- **Flapping under noisy adapters.** Removing the `next != StateWorking` filter means a misbehaving adapter could oscillate. The known case is Claude's 5s silence heuristic (§3) — addressed by bumping the threshold to 30s. For heuristic adapters the `idle_ms` gate + 200ms ticker should be sufficient. If we observe flapping in practice on other adapters, layer a per-session stability counter (Approach B from the brainstorm) at that point.
- **`done_regex` false positives.** A poorly-written regex could mark an in-progress agent as done. Mitigation: opt-in only, never enabled by default, surfaced in `docs/registry.md` with a "test against a real transcript" warning.
- **Buffered `CompleteCh` race.** A second `CompleteSession` call within microseconds of the first could be dropped silently. Acceptable — the desired terminal state is already in flight.
