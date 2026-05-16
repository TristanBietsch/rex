# Kanban State Machine Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix three bugs in the rex session state machine: `needs_input` no longer regresses to `working`, interactive agents have no path to `done`, and terminal (`failed`/`crashed`) sessions don't render on the kanban.

**Architecture:** Drop the supervisor's `next != StateWorking` filter (which was silently swallowing the regression), add an optional `done_regex` to the heuristic adapter for opt-in auto-done, add a manual completion path via new `IntentComplete` + `c` keybind that cleanly kills the PTY and marks the session done, and centralize the TUI board grouping so all three terminal states share the Completed column.

**Tech Stack:** Go 1.x, `creack/pty`, Bubble Tea, `stretchr/testify/require`, no cgo.

**Spec:** `docs/superpowers/specs/2026-05-15-kanban-state-machine-design.md`

---

## File map

**Modify (existing):**
- `internal/state/store.go` — add `CurrentState(id)` accessor
- `internal/protocol/intents.go` — add `IntentComplete` constant + `Complete` struct
- `internal/registry/types.go` — add `DoneRegex` field to `Detect`
- `internal/registry/loader.go` — validate optional `done_regex` compiles
- `internal/adapter/heuristic.go` — `done` field, precedence in `Detect`
- `internal/adapter/adapter.go` — pass `DoneRegex` to `NewHeuristic`
- `internal/adapter/claude.go` — bump silence heuristic 5s → 30s
- `internal/pty/supervisor.go` — `CompleteCh` field, new select case, transition rule fix
- `internal/server/server.go` — `RegisterComplete`/`UnregisterComplete`/`CompleteSession`
- `internal/server/client.go` — `IntentComplete` dispatch, `CompleteCh` wiring in `handleNewSession`
- `internal/client/client.go` — `Complete(id)` method
- `cmd/rex/main.go` — register `complete` subcommand
- `internal/cli/help.go` — add `complete` row to Session section
- `internal/tui/board.go` — `boardGroups` + `filterByGroup`, delete `filterByState`
- `internal/tui/keymap.go` — migrate three call sites to shared grouping
- `internal/tui/update.go` — `c` keybind handler
- `docs/registry.md`, `docs/tui.md`, `docs/cli.md`, `docs/protocol.md`

**Modify (existing tests):**
- `internal/state/store_test.go`
- `internal/registry/loader_test.go`
- `internal/adapter/heuristic_test.go`
- `internal/adapter/claude_test.go`
- `internal/pty/supervisor_test.go`
- `internal/server/server_test.go`
- `internal/client/client_test.go`
- `internal/tui/snapshot_test.go`

**Create:**
- `internal/cli/complete.go`
- `internal/cli/complete_test.go`

---

## Task 1: Add `Store.CurrentState` accessor

Fixes the pre-existing data race at `supervisor.go:288` where `sess.State` is read without holding `sess.mu`. Foundation for Task 5.

**Files:**
- Modify: `internal/state/store.go`
- Modify: `internal/state/store_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/state/store_test.go`:

```go
func TestStore_CurrentState(t *testing.T) {
	s := NewStore()
	sess := &Session{ID: "id1", ShortID: "id1", State: protocol.StateWorking}
	require.NoError(t, s.Add(sess))

	got, ok := s.CurrentState("id1")
	require.True(t, ok)
	require.Equal(t, protocol.StateWorking, got)

	_, ok = s.CurrentState("missing")
	require.False(t, ok)
}

func TestStore_CurrentStateConcurrentWithTransition(t *testing.T) {
	s := NewStore()
	sess := &Session{ID: "id1", ShortID: "id1", State: protocol.StateWorking}
	require.NoError(t, s.Add(sess))

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); _, _ = s.CurrentState("id1") }()
		go func() { defer wg.Done(); _ = s.Transition("id1", protocol.StateNeedsInput) }()
	}
	wg.Wait()
}
```

Add `"sync"` to the imports of `store_test.go` if not present.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/state/ -run TestStore_CurrentState -race -v`
Expected: FAIL — `s.CurrentState undefined`

- [ ] **Step 3: Implement `CurrentState` in `internal/state/store.go`**

Add after the existing `Get` method (around line 67):

```go
// CurrentState returns the session's current state under the appropriate locks.
// Returns ("", false) if the session doesn't exist.
func (s *Store) CurrentState(id string) (protocol.State, bool) {
	s.mu.RLock()
	sess, ok := s.sessions[id]
	s.mu.RUnlock()
	if !ok {
		return "", false
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.State, true
}
```

Note: `Session.mu` is a plain `sync.Mutex` (see `internal/state/session.go:37`), not an `RWMutex`, so use `Lock`/`Unlock`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/state/ -run TestStore_CurrentState -race -v`
Expected: PASS for both tests

- [ ] **Step 5: Commit**

```bash
git add internal/state/store.go internal/state/store_test.go
git commit -m "state: add Store.CurrentState accessor for race-free reads"
```

---

## Task 2: Add `IntentComplete` protocol type

Defines the new IPC verb the CLI and TUI will use to request manual completion.

**Files:**
- Modify: `internal/protocol/intents.go`

- [ ] **Step 1: Edit `internal/protocol/intents.go`**

Add `IntentComplete = "Complete"` to the const block (after line 17), and add the `Complete` struct at the end of the file.

Find the const block:
```go
IntentSetMaxConcurrent = "SetMaxConcurrent"
IntentSetSessionFleet  = "SetSessionFleet"
```
Add immediately after:
```go
IntentComplete         = "Complete"
```

Then append to the end of the file:
```go
// Complete asks the daemon to cleanly terminate a running session and mark it done.
// Distinct from Delete (which removes the session entirely) and Subscribe (read-only).
type Complete struct {
	SessionID string `json:"session_id"`
}
```

- [ ] **Step 2: Verify it builds**

Run: `go build ./internal/protocol/`
Expected: no output (success)

- [ ] **Step 3: Commit**

```bash
git add internal/protocol/intents.go
git commit -m "protocol: add IntentComplete verb for manual session completion"
```

---

## Task 3: Add `DoneRegex` field to registry schema

Schema-only change. The adapter and loader validation in Task 4.

**Files:**
- Modify: `internal/registry/types.go`

- [ ] **Step 1: Edit `internal/registry/types.go`**

Replace the `Detect` struct (lines 19-24) with:

```go
// Detect describes how the adapter decides session state.
type Detect struct {
	Kind        string `yaml:"kind"`             // "structured" | "heuristic"
	Format      string `yaml:"format,omitempty"` // when kind=structured
	PromptRegex string `yaml:"prompt_regex,omitempty"`
	DoneRegex   string `yaml:"done_regex,omitempty"` // optional; flips heuristic to StateDone after idle
	IdleMs      int    `yaml:"idle_ms,omitempty"`
}
```

- [ ] **Step 2: Verify it builds**

Run: `go build ./internal/registry/`
Expected: no output

- [ ] **Step 3: Commit**

```bash
git add internal/registry/types.go
git commit -m "registry: add optional done_regex field to detect schema"
```

---

## Task 4: Heuristic adapter `done_regex` precedence + loader validation

**Files:**
- Modify: `internal/adapter/heuristic.go`
- Modify: `internal/adapter/heuristic_test.go`
- Modify: `internal/adapter/adapter.go`
- Modify: `internal/registry/loader.go`
- Modify: `internal/registry/loader_test.go` (create if missing)

- [ ] **Step 1: Check if `loader_test.go` exists**

Run: `ls internal/registry/loader_test.go 2>/dev/null && echo found || echo missing`

If "missing", create `internal/registry/loader_test.go`:

```go
package registry

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidate_HeuristicWithoutDoneRegex(t *testing.T) {
	tools := []Tool{{
		ID: "t1", Models: []Model{{ID: "m1"}},
		Detect: Detect{Kind: "heuristic", PromptRegex: "> ", IdleMs: 100},
	}}
	require.NoError(t, validate(tools))
}
```

- [ ] **Step 2: Add failing tests for `done_regex` validation**

Append to `internal/registry/loader_test.go`:

```go
func TestValidate_HeuristicWithValidDoneRegex(t *testing.T) {
	tools := []Tool{{
		ID: "t1", Models: []Model{{ID: "m1"}},
		Detect: Detect{Kind: "heuristic", PromptRegex: "> ", DoneRegex: "^✓ done$", IdleMs: 100},
	}}
	require.NoError(t, validate(tools))
}

func TestValidate_HeuristicWithInvalidDoneRegex(t *testing.T) {
	tools := []Tool{{
		ID: "t1", Models: []Model{{ID: "m1"}},
		Detect: Detect{Kind: "heuristic", PromptRegex: "> ", DoneRegex: "[unclosed", IdleMs: 100},
	}}
	err := validate(tools)
	require.Error(t, err)
	require.Contains(t, err.Error(), "done_regex")
}
```

- [ ] **Step 3: Add failing tests for adapter precedence**

Append to `internal/adapter/heuristic_test.go`:

```go
func TestHeuristic_DoneRegexMatchesReturnsDone(t *testing.T) {
	h, err := NewHeuristic("^> ", "^✓ session complete$", 100*time.Millisecond)
	require.NoError(t, err)
	got := h.Detect([]byte("output here\n✓ session complete"), 200*time.Millisecond)
	require.Equal(t, protocol.StateDone, got)
}

func TestHeuristic_DoneRegexEmptyKeepsExistingBehavior(t *testing.T) {
	h, err := NewHeuristic("^awaiting input:", "", 100*time.Millisecond)
	require.NoError(t, err)
	got := h.Detect([]byte("awaiting input:"), 200*time.Millisecond)
	require.Equal(t, protocol.StateNeedsInput, got)
}

func TestHeuristic_DoneRegexWinsOverPrompt(t *testing.T) {
	h, err := NewHeuristic("^> ", "^✓ done$", 100*time.Millisecond)
	require.NoError(t, err)
	// Both patterns appear in the window; done wins.
	got := h.Detect([]byte("✓ done\n> "), 200*time.Millisecond)
	require.Equal(t, protocol.StateDone, got)
}

func TestHeuristic_DoneRegexGatedByIdle(t *testing.T) {
	h, err := NewHeuristic("^> ", "^✓ done$", 200*time.Millisecond)
	require.NoError(t, err)
	// Idle < idle_ms — should return Working regardless of pattern match.
	got := h.Detect([]byte("✓ done"), 10*time.Millisecond)
	require.Equal(t, protocol.StateWorking, got)
}

func TestNewHeuristic_RejectsBadDoneRegex(t *testing.T) {
	_, err := NewHeuristic("^> ", "[unclosed", 100*time.Millisecond)
	require.Error(t, err)
	require.Contains(t, err.Error(), "compile done regex")
}
```

Also update the existing test `TestNewHeuristic_RejectsBadRegex` (line 32-36) to pass an empty done regex:

```go
func TestNewHeuristic_RejectsBadRegex(t *testing.T) {
	_, err := NewHeuristic("[unclosed", "", 100*time.Millisecond)
	require.Error(t, err)
	require.Contains(t, err.Error(), "compile prompt regex")
}
```

And update every other existing `NewHeuristic(...)` call in `heuristic_test.go` to add `""` as the second argument. Six call sites: lines 12, 19, 26, 43, 52, 62 — all become `NewHeuristic(prompt, "", idle)`.

- [ ] **Step 4: Run tests to verify they fail**

Run: `go test ./internal/adapter/ ./internal/registry/ -run "Heuristic|Validate" -v`
Expected: FAIL — compilation errors (`NewHeuristic` signature mismatch, `validate` not knowing about done_regex)

- [ ] **Step 5: Update `internal/adapter/heuristic.go`**

Replace the `HeuristicCLI` struct, the `NewHeuristic` constructor, and the `Detect` method (lines 16-54). Final state:

```go
// HeuristicCLI is a regex+idle adapter for CLIs without structured output.
type HeuristicCLI struct {
	prompt *regexp.Regexp
	done   *regexp.Regexp // nil = no auto-done; manual completion or process exit only
	idle   time.Duration
}

// NewHeuristic builds a HeuristicCLI. promptRegex is required; doneRegex is optional
// (empty string disables auto-done). Returns an error if either regex is invalid.
func NewHeuristic(promptRegex, doneRegex string, idle time.Duration) (*HeuristicCLI, error) {
	prompt, err := regexp.Compile("(?m)" + promptRegex)
	if err != nil {
		return nil, fmt.Errorf("compile prompt regex %q: %w", promptRegex, err)
	}
	h := &HeuristicCLI{prompt: prompt, idle: idle}
	if doneRegex != "" {
		done, err := regexp.Compile("(?m)" + doneRegex)
		if err != nil {
			return nil, fmt.Errorf("compile done regex %q: %w", doneRegex, err)
		}
		h.done = done
	}
	return h, nil
}

// Detect implements Adapter. Precedence after the idle gate: done > prompt > working.
func (h *HeuristicCLI) Detect(window []byte, idle time.Duration) protocol.State {
	if idle < h.idle {
		return protocol.StateWorking
	}
	tail := window
	if len(tail) > 4096 {
		tail = tail[len(tail)-4096:]
	}
	withLineBreaks := cursorPositionRe.ReplaceAllString(string(tail), "\n")
	clean := ansi.Strip(withLineBreaks)
	if h.done != nil && h.done.MatchString(clean) {
		return protocol.StateDone
	}
	if h.prompt.MatchString(clean) {
		return protocol.StateNeedsInput
	}
	return protocol.StateWorking
}
```

- [ ] **Step 6: Update `internal/adapter/adapter.go`**

Change the `case "heuristic"` branch (line 20-21) to pass `DoneRegex`:

```go
case "heuristic":
	return NewHeuristic(t.Detect.PromptRegex, t.Detect.DoneRegex, time.Duration(t.Detect.IdleMs)*time.Millisecond)
```

- [ ] **Step 7: Update `internal/registry/loader.go`**

In the `validate` function, replace the `case "heuristic":` branch (lines 153-156) with:

```go
case "heuristic":
	if t.Detect.PromptRegex == "" || t.Detect.IdleMs <= 0 {
		return fmt.Errorf("tool %q: heuristic detect needs prompt_regex and idle_ms", t.ID)
	}
	if t.Detect.DoneRegex != "" {
		if _, err := regexp.Compile("(?m)" + t.Detect.DoneRegex); err != nil {
			return fmt.Errorf("tool %q: done_regex compile failed: %w", t.ID, err)
		}
	}
```

Add `"regexp"` to the imports at the top of `loader.go` if not present (check with `grep '"regexp"' internal/registry/loader.go`).

- [ ] **Step 8: Run tests to verify they pass**

Run: `go test ./internal/adapter/ ./internal/registry/ -race -v`
Expected: PASS (all tests, including the original adapter tests and the new ones)

- [ ] **Step 9: Commit**

```bash
git add internal/adapter/heuristic.go internal/adapter/heuristic_test.go internal/adapter/adapter.go internal/registry/loader.go internal/registry/loader_test.go
git commit -m "adapter: opt-in done_regex precedence for heuristic CLIs"
```

---

## Task 5: Bump Claude silence heuristic from 5s to 30s

Prevents `working ↔ needs_input` flapping during long Claude thinks, which the broken supervisor guard previously masked.

**Files:**
- Modify: `internal/adapter/claude.go`
- Modify: `internal/adapter/claude_test.go`

- [ ] **Step 1: Update the existing failing test**

The current test `TestClaudeStructured_IdleFallback` forces `lastSeen` 6 seconds in the past. With the 30s threshold it would still pass at 6s elapsed (heuristic would NOT fire), but we want a test that actually exercises the threshold. Replace the test (lines 25-33):

```go
func TestClaudeStructured_IdleFallbackAt31s(t *testing.T) {
	a := NewClaudeStructured()
	a.Detect([]byte(`{"type":"assistant"}`+"\n"), 100*time.Millisecond)
	// Force lastSeen 31s in the past — past the 30s threshold.
	a.lastSeen = time.Now().Add(-31 * time.Second)
	got := a.Detect(nil, time.Second)
	require.Equal(t, protocol.StateNeedsInput, got)
}

func TestClaudeStructured_NoFallbackUnder30s(t *testing.T) {
	a := NewClaudeStructured()
	a.Detect([]byte(`{"type":"assistant"}`+"\n"), 100*time.Millisecond)
	// 15s — well under threshold. Should stay working (no flap during deep thinks).
	a.lastSeen = time.Now().Add(-15 * time.Second)
	got := a.Detect(nil, time.Second)
	require.Equal(t, protocol.StateWorking, got)
}
```

- [ ] **Step 2: Run tests to verify the new "no fallback under 30s" test fails**

Run: `go test ./internal/adapter/ -run TestClaudeStructured -v`
Expected: `TestClaudeStructured_NoFallbackUnder30s` FAILS (heuristic still using 5s threshold)

- [ ] **Step 3: Update `internal/adapter/claude.go`**

Change line 61 from:
```go
if a.last == protocol.StateWorking && idle > 0 && time.Since(a.lastSeen) > 5*time.Second {
```
to:
```go
if a.last == protocol.StateWorking && idle > 0 && time.Since(a.lastSeen) > 30*time.Second {
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/adapter/ -run TestClaudeStructured -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/adapter/claude.go internal/adapter/claude_test.go
git commit -m "adapter: bump claude silence heuristic 5s->30s to prevent state flapping"
```

---

## Task 6: Fix supervisor transition rule (the needs_input → working regression)

This is the bug that prompted the whole project.

**Files:**
- Modify: `internal/pty/supervisor.go`
- Modify: `internal/pty/supervisor_test.go`

- [ ] **Step 1: Add a stub adapter helper to `supervisor_test.go`**

Append to `internal/pty/supervisor_test.go`:

```go
// stubAdapter returns a programmable sequence of states on successive Detect calls.
// After the sequence is exhausted, it keeps returning the final element.
type stubAdapter struct {
	mu       sync.Mutex
	sequence []protocol.State
	idx      int
}

func (s *stubAdapter) Detect(window []byte, idle time.Duration) protocol.State {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sequence) == 0 {
		return protocol.StateWorking
	}
	if s.idx >= len(s.sequence) {
		return s.sequence[len(s.sequence)-1]
	}
	out := s.sequence[s.idx]
	s.idx++
	return out
}
```

Add `"sync"` to the imports if not present.

- [ ] **Step 2: Add the failing regression test**

Append to `internal/pty/supervisor_test.go`:

```go
func TestSupervisor_NeedsInputToWorkingRegression(t *testing.T) {
	stateDir := t.TempDir()
	store := state.NewStore()
	sess := &state.Session{
		ID: "id1", ShortID: "id1", ToolID: "echo", Slug: "test",
		State: protocol.StateQueued, StartedAt: time.Now().UTC(),
	}
	require.NoError(t, store.Add(sess))

	var (
		mu          sync.Mutex
		transitions []protocol.State
	)
	store.Subscribe(func(e state.Event) {
		if e.NewState != nil {
			mu.Lock()
			transitions = append(transitions, *e.NewState)
			mu.Unlock()
		}
	})

	stub := &stubAdapter{sequence: []protocol.State{
		protocol.StateWorking,     // same as initial — no transition expected
		protocol.StateNeedsInput,  // working -> needs_input
		protocol.StateNeedsInput,  // no transition (deduped)
		protocol.StateWorking,     // the regression — needs_input -> working
		protocol.StateWorking,
	}}

	sup := New(SupervisorConfig{
		StateDir: stateDir, Store: store,
		Command:  []string{"sleep", "0.5"},
		Adapter:  stub,
		IdleTick: 10 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = sup.Run(ctx, sess)

	mu.Lock()
	defer mu.Unlock()
	sawNeeds, sawWorkingAfterNeeds := false, false
	for _, st := range transitions {
		if st == protocol.StateNeedsInput {
			sawNeeds = true
			continue
		}
		if sawNeeds && st == protocol.StateWorking {
			sawWorkingAfterNeeds = true
		}
	}
	require.True(t, sawNeeds, "expected needs_input transition; got %v", transitions)
	require.True(t, sawWorkingAfterNeeds, "expected needs_input -> working regression; got %v", transitions)
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/pty/ -run TestSupervisor_NeedsInputToWorkingRegression -v`
Expected: FAIL — `expected needs_input -> working regression; got [working needs_input done]`

- [ ] **Step 4: Fix the transition rule in `internal/pty/supervisor.go`**

Replace lines 288-291 (the `case <-ticker.C:` body's classification block, after the `windowMu.Lock`/`Unlock` for snapshot):

Find:
```go
next := s.cfg.Adapter.Detect(windowSnap, idle)
if next == "" {
	continue
}
current := sess.State
if next != current && next != protocol.StateWorking {
	_ = s.cfg.Store.Transition(sess.ID, next)
}
```

Replace with:
```go
next := s.cfg.Adapter.Detect(windowSnap, idle)
if next == "" {
	continue
}
current, ok := s.cfg.Store.CurrentState(sess.ID)
if !ok {
	continue
}
if next != current {
	_ = s.cfg.Store.Transition(sess.ID, next)
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/pty/ -run TestSupervisor_NeedsInputToWorkingRegression -race -v`
Expected: PASS

- [ ] **Step 6: Run the whole supervisor suite for regressions**

Run: `go test ./internal/pty/ -race`
Expected: PASS — `TestSupervisor_RunEchoToCompletion` and `TestLastNonEmptyLine_*` still pass.

- [ ] **Step 7: Commit**

```bash
git add internal/pty/supervisor.go internal/pty/supervisor_test.go
git commit -m "pty: allow needs_input -> working regression via Store.CurrentState dedupe"
```

---

## Task 7: Add `CompleteCh` and manual-completion select case to supervisor

**Files:**
- Modify: `internal/pty/supervisor.go`
- Modify: `internal/pty/supervisor_test.go`

- [ ] **Step 1: Add the failing test**

Append to `internal/pty/supervisor_test.go`:

```go
func TestSupervisor_CompleteSignalEndsCleanlyWithStateDone(t *testing.T) {
	stateDir := t.TempDir()
	store := state.NewStore()
	sess := &state.Session{
		ID: "id1", ShortID: "id1", ToolID: "echo", Slug: "test",
		State: protocol.StateQueued, StartedAt: time.Now().UTC(),
	}
	require.NoError(t, store.Add(sess))

	completeCh := make(chan struct{}, 1)
	sup := New(SupervisorConfig{
		StateDir:   stateDir,
		Store:      store,
		Command:    []string{"sleep", "30"}, // long enough that we know we triggered it
		CompleteCh: completeCh,
		IdleTick:   50 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- sup.Run(ctx, sess) }()

	// Let it spawn and reach StateWorking.
	time.Sleep(100 * time.Millisecond)
	completeCh <- struct{}{}

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("supervisor did not return after complete signal")
	}

	got, _ := store.Get("id1")
	require.Equal(t, protocol.StateDone, got.State)
}

func TestSupervisor_CtxCancelStillProducesFailed(t *testing.T) {
	// Regression guard: existing ctx-cancel behavior must remain StateFailed.
	stateDir := t.TempDir()
	store := state.NewStore()
	sess := &state.Session{
		ID: "id2", ShortID: "id2", ToolID: "echo", Slug: "test",
		State: protocol.StateQueued, StartedAt: time.Now().UTC(),
	}
	require.NoError(t, store.Add(sess))

	sup := New(SupervisorConfig{
		StateDir: stateDir, Store: store,
		Command:  []string{"sleep", "30"},
		IdleTick: 50 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- sup.Run(ctx, sess) }()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("supervisor did not return after ctx cancel")
	}

	got, _ := store.Get("id2")
	require.Equal(t, protocol.StateFailed, got.State)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/pty/ -run "Supervisor_(CompleteSignal|CtxCancel)" -v`
Expected: `TestSupervisor_CompleteSignalEndsCleanlyWithStateDone` FAILS — `CompleteCh` is not a field of `SupervisorConfig`.
The CtxCancel test should already pass (existing behavior).

- [ ] **Step 3: Add `CompleteCh` to `SupervisorConfig`**

In `internal/pty/supervisor.go`, find the `SupervisorConfig` struct (lines 20-35). Add the new field just before `RegisterResize`:

```go
// CompleteCh signals a clean shutdown — supervisor kills the child and
// transitions to StateDone. Distinct from ctx cancel, which means StateFailed.
// Buffered size 1; sender should use a non-blocking send.
CompleteCh chan struct{}
```

- [ ] **Step 4: Add the new select case in `Run`**

In `internal/pty/supervisor.go`, find the `for { select { ... } }` block (around line 241-293). Add a new case after `case <-ctx.Done():` and before `case rerr := <-errc:`:

```go
case <-s.cfg.CompleteCh:
	slog.Info("pty: complete signal received, killing child cleanly",
		"session", sess.ID, "pid", cmd.Process.Pid)
	_ = cmd.Process.Kill()
	<-errc          // drain the reader goroutine
	_ = cmd.Wait()  // reap the child; mirrors the natural-exit branch
	if err := s.cfg.Store.Transition(sess.ID, protocol.StateDone); err != nil {
		return err
	}
	sess.LastEventAt = time.Now().UTC()
	if err := state.WriteMeta(s.cfg.StateDir, sess); err != nil {
		return fmt.Errorf("write final meta: %w", err)
	}
	return nil
```

Note: order matters — `Store.Transition` first (which takes `sess.mu` and writes state safely), then update `LastEventAt` and `WriteMeta`. We do NOT write `sess.State = ...` directly here; that would race with concurrent readers.

- [ ] **Step 5: Guard the new case against a nil channel**

A nil channel in a `select` case blocks forever, which is exactly what we want when `CompleteCh` is not configured. **No additional guard needed.** Verify by inspection: tests that don't pass `CompleteCh` will have `s.cfg.CompleteCh == nil`, and `<-nil` in select is permanently blocked, which is correct.

- [ ] **Step 6: Run all supervisor tests**

Run: `go test ./internal/pty/ -race -v`
Expected: PASS for all tests including the two new ones.

- [ ] **Step 7: Commit**

```bash
git add internal/pty/supervisor.go internal/pty/supervisor_test.go
git commit -m "pty: add CompleteCh select case for clean manual termination"
```

---

## Task 8: Server `RegisterComplete` / `CompleteSession` plumbing

Mirrors the existing `RegisterStop` pattern (server.go:269-296).

**Files:**
- Modify: `internal/server/server.go`
- Modify: `internal/server/server_test.go`

- [ ] **Step 1: Add the failing test**

Append to `internal/server/server_test.go`:

```go
func TestServer_RegisterAndCompleteSession(t *testing.T) {
	s := &Server{}
	called := 0
	s.RegisterComplete("id1", func() { called++ })
	s.CompleteSession("id1")
	require.Equal(t, 1, called)
}

func TestServer_CompleteSessionUnknownIsNoop(t *testing.T) {
	s := &Server{}
	require.NotPanics(t, func() { s.CompleteSession("missing") })
}

func TestServer_UnregisterCompleteSilencesFurtherCalls(t *testing.T) {
	s := &Server{}
	called := 0
	s.RegisterComplete("id1", func() { called++ })
	s.UnregisterComplete("id1")
	s.CompleteSession("id1")
	require.Equal(t, 0, called)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server/ -run TestServer_RegisterAndComplete -v`
Expected: FAIL — methods don't exist.

- [ ] **Step 3: Add the fields and methods to `Server`**

In `internal/server/server.go`, find the `Server` struct (lines 67-86). Add the new field after `stopFuncs`:

```go
completeMu    sync.Mutex
completeFuncs map[string]func()
```

Then append after the `StopSession` method (after line 296):

```go
// RegisterComplete stores a complete-signal closure for a session.
// handleNewSession registers a closure that sends to the supervisor's CompleteCh.
func (s *Server) RegisterComplete(sessionID string, fn func()) {
	s.completeMu.Lock()
	defer s.completeMu.Unlock()
	if s.completeFuncs == nil {
		s.completeFuncs = make(map[string]func())
	}
	s.completeFuncs[sessionID] = fn
}

// UnregisterComplete removes the registered closure after the supervisor exits.
func (s *Server) UnregisterComplete(sessionID string) {
	s.completeMu.Lock()
	defer s.completeMu.Unlock()
	delete(s.completeFuncs, sessionID)
}

// CompleteSession invokes the registered complete closure (no-op if absent).
// Unlike StopSession, this does NOT block on supervisor exit — the closure
// is a non-blocking send to a buffered channel.
func (s *Server) CompleteSession(sessionID string) {
	s.completeMu.Lock()
	fn := s.completeFuncs[sessionID]
	s.completeMu.Unlock()
	if fn != nil {
		fn()
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/server/ -run TestServer_RegisterAndComplete -race -v`
Expected: PASS for all three new tests.

- [ ] **Step 5: Commit**

```bash
git add internal/server/server.go internal/server/server_test.go
git commit -m "server: add RegisterComplete/UnregisterComplete/CompleteSession"
```

---

## Task 9: Wire `IntentComplete` dispatch and `CompleteCh` in `handleNewSession`

**Files:**
- Modify: `internal/server/client.go`

- [ ] **Step 1: Find `handleNewSession`**

Run: `grep -n "func handleNewSession\|pty.New\|RegisterStop\|UnregisterStop" internal/server/client.go`

You'll see `pty.New(pty.SupervisorConfig{...})` as a struct literal (around lines 280-297) and `srv.RegisterStop(...)` shortly after (around line 302). The supervisor goroutine is launched at line 308 with several `defer srv.Unregister*` calls.

- [ ] **Step 2: Declare `completeCh` before `pty.New`**

Immediately BEFORE the `sup := pty.New(pty.SupervisorConfig{...})` call (around line 280), insert:

```go
completeCh := make(chan struct{}, 1)
```

- [ ] **Step 3: Add `CompleteCh` to the inline `SupervisorConfig` struct literal**

Inside the struct literal passed to `pty.New(pty.SupervisorConfig{...})`, add a new field. Place it near `InputCh:` for grouping with other channel-typed fields:

```go
CompleteCh: completeCh,
```

- [ ] **Step 4: Register/unregister the closure around `RegisterStop`**

Immediately AFTER the existing `srv.RegisterStop(sess.ID, ...)` block (after line 305), insert:

```go
srv.RegisterComplete(sess.ID, func() {
	select {
	case completeCh <- struct{}{}:
	default:
		// already pending; further signals are no-ops
	}
})
```

Inside the goroutine launched right after (around line 308), add a `defer srv.UnregisterComplete(sess.ID)` line. Place it alongside the existing `defer srv.UnregisterStop(sess.ID)` (line 310):

```go
go func() {
	defer close(done)
	defer srv.UnregisterStop(sess.ID)
	defer srv.UnregisterComplete(sess.ID)   // <-- new line
	defer srv.UnregisterInputChannel(sess.ID)
	defer srv.ReleaseSession()
	_ = sup.Run(sessCtx, sess)
}()
```

- [ ] **Step 5: Add `IntentComplete` dispatch in the intent switch**

In `internal/server/client.go`, find the `case protocol.IntentDelete:` block (around line 73). Add a new case after the IntentDelete handler closes (after line 87) and before `case protocol.IntentSubscribe:`:

```go
case protocol.IntentComplete:
	var p protocol.Complete
	if err := json.Unmarshal(env.Data, &p); err != nil {
		writeError(w, env.ID, protocol.ErrCodeBadIntent, err.Error())
		continue
	}
	if _, ok := cfg.Store.Get(p.SessionID); !ok {
		writeError(w, env.ID, protocol.ErrCodeUnknownSession, "session not found")
		continue
	}
	srv.CompleteSession(p.SessionID)
```

Note: this follows the existing IntentDelete/IntentRename pattern — no Ack on success, only error on failure.

- [ ] **Step 6: Verify it builds**

Run: `go build ./...`
Expected: no output

- [ ] **Step 7: Run server tests**

Run: `go test ./internal/server/ -race`
Expected: PASS (existing tests still pass; we haven't added e2e coverage here yet)

- [ ] **Step 8: Commit**

```bash
git add internal/server/client.go
git commit -m "server: dispatch IntentComplete and wire CompleteCh per session"
```

---

## Task 10: Add `client.Complete` method

**Files:**
- Modify: `internal/client/client.go`
- Modify: `internal/client/client_test.go`

- [ ] **Step 1: Inspect `client_test.go` for the existing test style**

Run: `head -40 internal/client/client_test.go`

You'll see tests that use a pipe-backed Client. Follow the same pattern.

- [ ] **Step 2: Add a failing test**

Append to `internal/client/client_test.go`:

```go
func TestClient_CompleteWritesIntent(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	c := &Client{
		conn: clientConn,
		r:    protocol.NewReader(clientConn),
		w:    protocol.NewWriter(clientConn),
	}

	done := make(chan error, 1)
	go func() { done <- c.Complete("sess-1") }()

	r := protocol.NewReader(serverConn)
	env, err := r.Read()
	require.NoError(t, err)
	require.Equal(t, protocol.KindIntent, env.Kind)
	require.Equal(t, protocol.IntentComplete, env.Type)

	var p protocol.Complete
	require.NoError(t, json.Unmarshal(env.Data, &p))
	require.Equal(t, "sess-1", p.SessionID)

	require.NoError(t, <-done)
}
```

Add imports if missing: `"encoding/json"`, `"net"`, `"github.com/tristanbietsch/rex/internal/protocol"`.

- [ ] **Step 3: Run to verify it fails**

Run: `go test ./internal/client/ -run TestClient_Complete -v`
Expected: FAIL — `c.Complete undefined`

- [ ] **Step 4: Add the method to `internal/client/client.go`**

After the existing `Delete` method (after line 97):

```go
// Complete asks the daemon to cleanly terminate a session and mark it done.
func (c *Client) Complete(sessionID string) error {
	return c.w.WriteIntent(protocol.IntentComplete, "", protocol.Complete{SessionID: sessionID})
}
```

- [ ] **Step 5: Run to verify it passes**

Run: `go test ./internal/client/ -run TestClient_Complete -race -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/client/client.go internal/client/client_test.go
git commit -m "client: add Complete method to send IntentComplete"
```

---

## Task 11: Add `rex complete` CLI subcommand

**Files:**
- Create: `internal/cli/complete.go`
- Create: `internal/cli/complete_test.go`
- Modify: `cmd/rex/main.go`
- Modify: `internal/cli/help.go`

- [ ] **Step 1: Create `internal/cli/complete.go`**

Write `internal/cli/complete.go`:

```go
package cli

import (
	"flag"

	"github.com/tristanbietsch/rex/internal/client"
)

// RunComplete cleanly terminates a running session and marks it done.
// Distinct from rm (which deletes) and archive (which renames).
func RunComplete(args []string) error {
	fs := flag.NewFlagSet("complete", flag.ContinueOnError)
	socket := fs.String("socket", DefaultSocket(), "UDS path")
	if err := fs.Parse(args); err != nil {
		return NewExitError(ExitInvalidArgs, err.Error())
	}
	if fs.NArg() != 1 {
		return NewExitError(ExitInvalidArgs, "complete: exactly one selector required")
	}
	sel := fs.Arg(0)

	c, err := client.Dial(*socket)
	if err != nil {
		return NewExitError(ExitDaemonUnreachable, err.Error())
	}
	defer c.Close()
	sess, err := ResolveSelector(c, sel)
	if err != nil {
		return err
	}
	if err := c.Complete(sess.ID); err != nil {
		return NewExitError(ExitGeneric, err.Error())
	}
	return nil
}
```

- [ ] **Step 2: Create `internal/cli/complete_test.go`**

Look at `internal/cli/output_test.go` for the existing test style. The `RunComplete` exit-code paths can be tested with a fake socket. Minimal coverage:

```go
package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunComplete_NoSelectorReturnsInvalidArgs(t *testing.T) {
	err := RunComplete([]string{})
	require.Error(t, err)
	ec, ok := err.(ExitError)
	require.True(t, ok)
	require.Equal(t, ExitInvalidArgs, ec.ExitCode())
}

func TestRunComplete_TooManyArgsReturnsInvalidArgs(t *testing.T) {
	err := RunComplete([]string{"a", "b"})
	require.Error(t, err)
	ec, ok := err.(ExitError)
	require.True(t, ok)
	require.Equal(t, ExitInvalidArgs, ec.ExitCode())
}

func TestRunComplete_DaemonUnreachable(t *testing.T) {
	err := RunComplete([]string{"--socket", "/tmp/definitely-not-a-real-socket-rex-test", "some-id"})
	require.Error(t, err)
	ec, ok := err.(ExitError)
	require.True(t, ok)
	require.Equal(t, ExitDaemonUnreachable, ec.ExitCode())
}
```

- [ ] **Step 3: Register the subcommand in `cmd/rex/main.go`**

In `cmd/rex/main.go`, find the `switch args[0]` block (lines 24-77). Add a new case near `archive` (alphabetical-ish; existing order isn't strict):

```go
case "complete":
	return cli.RunComplete(args[1:])
```

Place it between `case "archive":` and `case "reload":` for visual grouping.

- [ ] **Step 4: Add `complete` to the help screen**

In `internal/cli/help.go`, find the Session section (lines 36-49). Insert a new row between `archive` and `log`:

```go
{"complete <sel>", "cleanly terminate a session and mark it done"},
```

- [ ] **Step 5: Run tests + build**

Run:
```bash
go build ./...
go test ./internal/cli/ -run TestRunComplete -v
```
Expected: build succeeds, three new tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/complete.go internal/cli/complete_test.go cmd/rex/main.go internal/cli/help.go
git commit -m "cli: add 'rex complete <sel>' command and help entry"
```

---

## Task 12: TUI — shared `boardGroups` + `filterByGroup`

Centralizes the three-column grouping so failed/crashed sessions appear under **Completed** AND can be navigated to via j/k and `3`. Critical: skipping the keymap.go consolidation would leave failed/crashed rows visible but unselectable.

**Files:**
- Modify: `internal/tui/board.go`
- Modify: `internal/tui/keymap.go`
- Modify: `internal/tui/snapshot_test.go`

- [ ] **Step 1: Add failing snapshot test**

Append to `internal/tui/snapshot_test.go`:

```go
func TestBoardSnapshot_FailedAndCrashedRenderUnderCompleted(t *testing.T) {
	now := time.Now()
	sessions := []protocol.SessionSummary{
		{ID: "w1", ShortID: "w1", ToolID: "claude", ModelID: "opus",
			Slug: "working-one", State: protocol.StateWorking, LastEventAt: now.Add(-1 * time.Minute)},
		{ID: "d1", ShortID: "d1", ToolID: "claude", ModelID: "opus",
			Slug: "done-one", State: protocol.StateDone, LastEventAt: now.Add(-2 * time.Minute)},
		{ID: "f1", ShortID: "f1", ToolID: "claude", ModelID: "opus",
			Slug: "failed-one", State: protocol.StateFailed, LastEventAt: now.Add(-3 * time.Minute)},
		{ID: "c1", ShortID: "c1", ToolID: "claude", ModelID: "opus",
			Slug: "crashed-one", State: protocol.StateCrashed, LastEventAt: now.Add(-4 * time.Minute)},
	}
	m := Model{
		Sessions: sessions, Filter: "all", SelectedID: "w1",
		Width: 120, Height: 30, Focus: FocusBoard,
	}
	out := m.View()
	require.Contains(t, out, "Completed", "Completed header should render")
	require.Contains(t, out, "done-one", "done sessions should be visible")
	require.Contains(t, out, "failed-one", "failed sessions should appear under Completed")
	require.Contains(t, out, "crashed-one", "crashed sessions should appear under Completed")
}

func TestOrderedSessions_IncludesFailedAndCrashed(t *testing.T) {
	sessions := []protocol.SessionSummary{
		{ID: "w1", State: protocol.StateWorking},
		{ID: "f1", State: protocol.StateFailed},
		{ID: "c1", State: protocol.StateCrashed},
		{ID: "d1", State: protocol.StateDone},
	}
	m := Model{Sessions: sessions, Filter: "all"}
	got := orderedSessions(m)
	ids := make([]string, 0, len(got))
	for _, s := range got {
		ids = append(ids, s.ID)
	}
	require.ElementsMatch(t, []string{"w1", "f1", "c1", "d1"}, ids)
}
```

Ensure `"github.com/stretchr/testify/require"` is imported.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run "TestBoardSnapshot_Failed|TestOrderedSessions" -v`
Expected: FAIL — failed/crashed rows missing from output / orderedSessions.

- [ ] **Step 3: Add `boardGroups` + `filterByGroup` to `internal/tui/board.go`**

At the top of `internal/tui/board.go` after the imports, insert:

```go
// boardGroup defines one section of the kanban. All board grouping (rendering,
// navigation, jump-to-section) reads from boardGroups so the three views stay
// in sync. Adding a new state means updating exactly one place.
type boardGroup struct {
	Title string
	Match func(protocol.State) bool
}

// boardGroups is the canonical kanban layout. Completed includes all terminal
// states (done/failed/crashed); distinct markers (●/✕/○) preserve the
// at-a-glance distinction within the column.
var boardGroups = []boardGroup{
	{"Needs input", func(s protocol.State) bool { return s == protocol.StateNeedsInput }},
	{"Working", func(s protocol.State) bool { return s == protocol.StateWorking }},
	{"Completed", func(s protocol.State) bool {
		return s == protocol.StateDone || s == protocol.StateFailed || s == protocol.StateCrashed
	}},
}

// filterByGroup applies the group predicate plus the active tool filter.
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

- [ ] **Step 4: Migrate `renderBoard` in `internal/tui/board.go`**

Replace the existing `renderBoard` opening (lines 69-95) — the per-group loop. Find:

```go
groups := []struct {
	title string
	state protocol.State
}{
	{"Needs input", protocol.StateNeedsInput},
	{"Working", protocol.StateWorking},
	{"Completed", protocol.StateDone},
}

gapBetween := densityGap(m)
var lines []string
for i, g := range groups {
	rows := filterByState(m.Sessions, g.state, m.Filter)
	if i > 0 {
		for j := 0; j < gapBetween; j++ {
			lines = append(lines, "")
		}
	}
	lines = append(lines, "  "+styleSectionTitle.Render(g.title))
	if len(rows) == 0 {
		lines = append(lines, "    "+styleMuted.Render("(none)"))
	} else {
		for _, s := range rows {
			lines = append(lines, renderRow(m, s, width))
		}
	}
}
```

Replace with:

```go
gapBetween := densityGap(m)
var lines []string
for i, g := range boardGroups {
	rows := filterByGroup(m.Sessions, g, m.Filter)
	if i > 0 {
		for j := 0; j < gapBetween; j++ {
			lines = append(lines, "")
		}
	}
	lines = append(lines, "  "+styleSectionTitle.Render(g.Title))
	if len(rows) == 0 {
		lines = append(lines, "    "+styleMuted.Render("(none)"))
	} else {
		for _, s := range rows {
			lines = append(lines, renderRow(m, s, width))
		}
	}
}
```

Delete the old `filterByState` function entirely (lines 118-130).

- [ ] **Step 5: Migrate `internal/tui/keymap.go`**

Replace `orderedSessions` (lines 7-17):

```go
// orderedSessions returns sessions in display order: Needs input → Working → Completed.
// Filter is applied within each group. All terminal states (done/failed/crashed)
// are included so j/k navigation matches what the user sees on the board.
func orderedSessions(m Model) []protocol.SessionSummary {
	out := make([]protocol.SessionSummary, 0, len(m.Sessions))
	for _, g := range boardGroups {
		out = append(out, filterByGroup(m.Sessions, g, m.Filter)...)
	}
	return out
}
```

Replace `selectedBoardLine` (lines 62-90):

```go
// selectedBoardLine returns the line index in the unscrolled board where the
// selected session is rendered, or -1 if nothing is selected or visible.
func selectedBoardLine(m Model) int {
	if m.SelectedID == "" {
		return -1
	}
	line := 0
	for i, g := range boardGroups {
		rows := filterByGroup(m.Sessions, g, m.Filter)
		if i > 0 {
			line++ // blank separator
		}
		line++ // section title
		if len(rows) == 0 {
			line++ // "(none)"
			continue
		}
		for _, s := range rows {
			if s.ID == m.SelectedID {
				return line
			}
			line++
		}
	}
	return -1
}
```

Replace `jumpToSection` (lines 125-131). The signature should now take an index into `boardGroups` instead of a `protocol.State`:

```go
// jumpToSection moves selection to the first row of the group at the given index
// (0=Needs input, 1=Working, 2=Completed). No-op if the group is empty.
func jumpToSection(m Model, groupIdx int) Model {
	if groupIdx < 0 || groupIdx >= len(boardGroups) {
		return m
	}
	rows := filterByGroup(m.Sessions, boardGroups[groupIdx], m.Filter)
	if len(rows) > 0 {
		m.SelectedID = rows[0].ID
	}
	return ensureVisible(m)
}
```

- [ ] **Step 6: Update `jumpToSection` callers in `internal/tui/update.go`**

Find the three jump-to-section keys (around lines 242-247):

```go
case "1":
	return jumpToSection(m, protocol.StateNeedsInput), nil
case "2":
	return jumpToSection(m, protocol.StateWorking), nil
case "3":
	return jumpToSection(m, protocol.StateDone), nil
```

Replace with:

```go
case "1":
	return jumpToSection(m, 0), nil
case "2":
	return jumpToSection(m, 1), nil
case "3":
	return jumpToSection(m, 2), nil
```

- [ ] **Step 7: Run the tests**

Run: `go test ./internal/tui/ -race -v`
Expected: PASS for all tests including the two new ones; the existing `TestBoardSnapshot` should still pass since its `demoSessions` only uses the three "active" states.

- [ ] **Step 8: Run the full build**

Run: `go build ./...`
Expected: no output. (If anything else referenced `filterByState`, it will error here.)

- [ ] **Step 9: Commit**

```bash
git add internal/tui/board.go internal/tui/keymap.go internal/tui/update.go internal/tui/snapshot_test.go
git commit -m "tui: shared boardGroups so failed/crashed render under Completed and navigate"
```

---

## Task 13: TUI `c` keybind to send Complete

**Files:**
- Modify: `internal/tui/update.go`

- [ ] **Step 1: Add `completeSessionCmd` helper**

In `internal/tui/update.go`, find `deleteSessionCmd` (around line 424-431). Add an analogous function immediately after:

```go
func completeSessionCmd(c *client.Client, sessionID string) tea.Cmd {
	return func() tea.Msg {
		if err := c.Complete(sessionID); err != nil {
			return DaemonErrMsg{Err: err}
		}
		return nil
	}
}
```

- [ ] **Step 2: Add the `c` key handler**

Find the FocusBoard key switch in `updateKey` (around lines 221-283). Insert a new case after `case "G":` (the bottom-of-list key) and before `case "1":`:

```go
case "c":
	if m.SelectedID == "" {
		return m, nil
	}
	if m.Audio != nil {
		m.Audio.Play(audio.EventClose)
	}
	return m, completeSessionCmd(m.Client, m.SelectedID)
```

The `audio.EventClose` event is the same one used for confirm-cancel actions — appropriate for "I'm done with this." If audio is nil (some test paths), the helper is a no-op.

- [ ] **Step 3: Verify build**

Run: `go build ./...`
Expected: no output.

- [ ] **Step 4: Run TUI tests**

Run: `go test ./internal/tui/ -race`
Expected: PASS — existing snapshot tests don't exercise key handling, so this passes by build.

- [ ] **Step 5: Manual smoke test**

Build and run the daemon + TUI:

```bash
go build -o rex ./cmd/rex
go build -o rex-daemon ./cmd/rex-daemon
./rex-daemon &  # if not already running
./rex
```

Inside the TUI:
1. Press `n` to spawn an echo session (any model).
2. Wait for it to appear under Working.
3. Press `c`.
4. The row should flip to Completed (●) within ~200ms; the process exits.

If audio plays a chime on `c`, that's expected. If the row stays put, check the daemon log at `~/.local/state/rex/daemon.log`.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/update.go
git commit -m "tui: add 'c' keybind to send Complete intent for the selected session"
```

---

## Task 14: Documentation

**Files:**
- Modify: `docs/registry.md`
- Modify: `docs/tui.md`
- Modify: `docs/cli.md`
- Modify: `docs/protocol.md`

- [ ] **Step 1: Document `done_regex` in `docs/registry.md`**

Find the section that describes the `detect` block (likely shows `prompt_regex` and `idle_ms`). Add an entry for `done_regex`:

```markdown
- `done_regex` (optional, heuristic only) — when set, the adapter emits
  `StateDone` whenever the (idle-gated) output window matches this pattern.
  Useful for tools that print a recognizable completion footer (e.g., `^✓ done`).
  If both `done_regex` and `prompt_regex` would match the same window, `done_regex`
  wins. Leave empty for tools whose prompt is identical between "awaiting" and
  "just finished" (codex, gemini, ollama) — use `rex complete` instead.
```

- [ ] **Step 2: Document `c` in `docs/tui.md`**

Find the keybinds table. Add a new row after `dd` (delete):

```markdown
| `c` | Complete: cleanly terminate the selected session and mark it done |
```

- [ ] **Step 3: Document `rex complete` in `docs/cli.md`**

Find the command table. Add a row after `archive`:

```markdown
| `rex complete <sel>` | Cleanly terminate a session and mark it done. Exit `0` on success, `2` not found, `3` ambiguous |
```

- [ ] **Step 4: Document `IntentComplete` in `docs/protocol.md`**

Find the intent vocabulary section. Add:

```markdown
- `Complete { session_id }` — daemon kills the session's PTY cleanly and
  transitions it to `done`. No-op if the session is already terminal.
  Distinct from `Delete` (which removes the session entirely).
```

- [ ] **Step 5: Commit**

```bash
git add docs/registry.md docs/tui.md docs/cli.md docs/protocol.md
git commit -m "docs: document done_regex, c keybind, rex complete, IntentComplete"
```

---

## Task 15: Full regression run + smoke test

- [ ] **Step 1: Run the full test suite with race detector**

Run: `go test -race ./...`
Expected: PASS — every package.

- [ ] **Step 2: Run vet + build**

Run:
```bash
go vet ./...
go build ./...
```
Expected: no output for either.

- [ ] **Step 3: End-to-end smoke**

```bash
go build -o rex ./cmd/rex
go build -o rex-daemon ./cmd/rex-daemon
```

In one terminal: `./rex-daemon`
In another:
```bash
./rex new "long-running test" --tool echo --model long  # spawns a 5-step echo
./rex ls       # row should appear under Working
./rex status   # "1 working" or "1 awaiting input"
```

Wait for the session to print prompt-style output (or use `--model prompt`). Then:
```bash
./rex complete <short-id>   # use the 4-char id from `rex ls`
./rex ls                    # row should now show state=done
```

Verify the transcript persisted:
```bash
ls ~/.local/share/rex/sessions/<short-id>*/
cat ~/.local/share/rex/sessions/<short-id>*/meta.json | jq .state
# expected: "done"
```

- [ ] **Step 4: Daemon log check**

Tail `~/.local/state/rex/daemon.log` while running the smoke. Look for:
- `pty: complete signal received, killing child cleanly`
- The session's final `state=done` broadcast event

If the log shows `state=failed` after `rex complete`, the dispatch is going through ctx-cancel instead of CompleteCh — revisit Task 9.

- [ ] **Step 5: No commit (verification only)**

If everything passes, the feature is shipped. If anything fails, file an issue with the daemon log excerpt.

---

## Self-review notes

Spec coverage check (cross-referencing the spec's "Component-by-component changes"):

- §1 supervisor transition rule fix → Task 6
- §1 supervisor CompleteCh handler → Task 7
- §2 heuristic done_regex → Task 4
- §3 Claude 30s bump → Task 5
- §4 registry types DoneRegex → Task 3
- §5 loader validation → Task 4 (combined for atomicity)
- §6 builtin.yaml unchanged → no task (intentional, opt-in only)
- §7 Store.CurrentState → Task 1
- §8 IntentComplete → Task 2
- §9 server Register/Unregister/Complete → Task 8
- §10 client.go IntentComplete dispatch + handleNewSession wiring → Task 9
- §11 client.Complete → Task 10
- §12 cli/complete.go → Task 11
- §13 cmd/rex + help.go → Task 11 (combined)
- §14 c keybind → Task 13
- §15 boardGroups + filterByGroup migration → Task 12
- §16 docs → Task 14

All spec sections have at least one task. Tests live with the code they cover and land in the same commit.

Sequencing: tasks 1-2 are independent foundations (run in parallel). Tasks 3-4 depend on 2 (DoneRegex schema first). Task 6 depends on Task 1. Task 7 depends on Task 6. Tasks 8-9 depend on Task 7. Task 10 depends on Task 2. Task 11 depends on Task 10. Task 12 depends on nothing (TUI-internal). Task 13 depends on Task 10. Task 14 documentation depends on everything. Task 15 verifies.
