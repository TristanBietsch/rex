# Spawn-flow fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make AI summaries actually populate the description column (auto-discover an available Ollama model when the configured one isn't pulled); fix the wizard's "describe the task" prompt being silently dropped by Claude / Codex / Gemini (replace 800ms-quiet heuristic with a per-adapter `IsReadyForInput` signal); rewire `i` to quick-spawn a configurable default agent (defaults `claude / opus / max`).

**Architecture:** Three independent fixes in the spawn flow. Summarizer gets a `resolveModel` helper + `SetModel` plumbing on the worker/client so the daemon's existing `probeOllamaHealth` (`cmd/rex-daemon/main.go:267`) can substitute when the configured model is missing. The `Adapter` interface grows `IsReadyForInput(window, idle) bool` — heuristic adapters answer via the existing `prompt_regex`; structured adapters via "first event observed." The supervisor's initial-prompt goroutine asks the adapter instead of timing 800ms of quiet. Three new `default_spawn_*` settings keys back a rewired `spawnSessionCmd` that calls the wizard's existing `deriveAgentSlug` helper directly — no new TUI plumbing.

**Tech Stack:** Go 1.x, `creack/pty`, Bubble Tea, `stretchr/testify/require`, Ollama HTTP API (`/api/tags`, `/api/generate`).

**Spec:** `docs/superpowers/specs/2026-05-19-spawn-flow-fixes-design.md`

---

## File map

**Modify (existing):**
- `internal/summarizer/worker.go` — add `resolveModel`, `SetModel`
- `internal/summarizer/client.go` — add `SetModel`
- `cmd/rex-daemon/main.go` — update `probeOllamaHealth` to substitute via `resolveModel`
- `internal/adapter/adapter.go` — extend `Adapter` interface with `IsReadyForInput`
- `internal/adapter/heuristic.go` — implement `IsReadyForInput`
- `internal/adapter/claude.go` — add `firstEvent` field, implement `IsReadyForInput`
- `internal/pty/supervisor.go` — rewire initial-prompt readiness check (lines 116-170)
- `internal/settings/registry.go` — add `SectionSpawn` + three `default_spawn_*` keys
- `internal/tui/update.go` — rewire `spawnSessionCmd` (lines 404-422) + caller at `updatePromptKey`
- `docs/settings.md`, `docs/tui.md`, `docs/registry.md` — document the changes

**Modify (existing tests):**
- `internal/summarizer/worker_test.go`
- `internal/summarizer/client_test.go`
- `internal/adapter/heuristic_test.go`
- `internal/adapter/claude_test.go`
- `internal/pty/supervisor_test.go`
- `internal/settings/registry_test.go`
- `internal/tui/update_test.go` (create if missing)

---

## Task 1: Add `resolveModel` helper to summarizer

Pure helper function: given a configured model and a list of pulled models, pick the model the worker should actually use. No I/O.

**Files:**
- Modify: `internal/summarizer/worker.go`
- Modify: `internal/summarizer/worker_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/summarizer/worker_test.go`:

```go
func TestResolveModel_ConfiguredPulledIsKept(t *testing.T) {
	got, ok := resolveModel("gemma2:2b", []string{"gemma2:2b", "llama3.1"})
	require.True(t, ok)
	require.Equal(t, "gemma2:2b", got)
}

func TestResolveModel_FallsThroughPreferenceList(t *testing.T) {
	// Configured missing; phi3:mini is the only preference-list match present.
	got, ok := resolveModel("gemma2:2b", []string{"llama3.1", "phi3:mini"})
	require.True(t, ok)
	require.Equal(t, "phi3:mini", got)
}

func TestResolveModel_PreferenceListWinsOverArbitraryFirst(t *testing.T) {
	// Both phi3:mini (preferred) and llama3.1 (arbitrary) are pulled; pref wins.
	got, ok := resolveModel("gemma2:2b", []string{"llama3.1", "phi3:mini"})
	require.True(t, ok)
	require.Equal(t, "phi3:mini", got)
}

func TestResolveModel_AnyPulledWhenPreferenceListMissing(t *testing.T) {
	got, ok := resolveModel("gemma2:2b", []string{"llama3.1"})
	require.True(t, ok)
	require.Equal(t, "llama3.1", got)
}

func TestResolveModel_NoneAvailable(t *testing.T) {
	_, ok := resolveModel("gemma2:2b", nil)
	require.False(t, ok)
	_, ok = resolveModel("gemma2:2b", []string{})
	require.False(t, ok)
}

func TestResolveModel_EmptyConfigured(t *testing.T) {
	// Caller did not specify a preferred model — pick best available.
	got, ok := resolveModel("", []string{"llama3.1"})
	require.True(t, ok)
	require.Equal(t, "llama3.1", got)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/summarizer/ -run TestResolveModel -v`
Expected: FAIL — `resolveModel undefined`

- [ ] **Step 3: Implement `resolveModel` in `internal/summarizer/worker.go`**

Add near the top of the file, after the package-level type declarations:

```go
// preferredFallbacks orders the small models the summarizer will substitute in
// when the configured model isn't pulled locally. Order matters — earlier
// entries win. Anything not in this list falls back to "first pulled" in
// whatever order Ollama returns them.
var preferredFallbacks = []string{"gemma2:2b", "llama3.2:1b", "phi3:mini", "qwen2.5:1.5b"}

// resolveModel picks the model the worker should use, given a configured
// preference and the list of models the local Ollama instance has pulled.
// Returns ("", false) if pulled is empty (no usable model at all).
func resolveModel(configured string, pulled []string) (string, bool) {
	if configured != "" {
		for _, p := range pulled {
			if p == configured {
				return configured, true
			}
		}
	}
	for _, pref := range preferredFallbacks {
		for _, p := range pulled {
			if p == pref {
				return pref, true
			}
		}
	}
	if len(pulled) > 0 {
		return pulled[0], true
	}
	return "", false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/summarizer/ -run TestResolveModel -race -v`
Expected: PASS for all six tests

- [ ] **Step 5: Commit**

```bash
git add internal/summarizer/worker.go internal/summarizer/worker_test.go
git commit -m "summarizer: add resolveModel helper for absent-model fallback"
```

---

## Task 2: Add `SetModel` to summarizer client and worker

The probe at `cmd/rex-daemon/main.go` needs a way to tell the worker "actually use this model" after substitution. Both the Client (which holds the model in HTTP requests) and the Worker (which holds the Config) need a setter.

**Files:**
- Modify: `internal/summarizer/client.go`
- Modify: `internal/summarizer/client_test.go`
- Modify: `internal/summarizer/worker.go`
- Modify: `internal/summarizer/worker_test.go`

- [ ] **Step 1: Write the failing test for Client.SetModel**

Append to `internal/summarizer/client_test.go`:

```go
func TestClient_SetModelChangesGenerateRequestModel(t *testing.T) {
	var captured string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Model string `json:"model"` }
		_ = json.NewDecoder(r.Body).Decode(&body)
		captured = body.Model
		_, _ = w.Write([]byte(`{"response":"ok"}`))
	}))
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, Model: "gemma2:2b"})
	c.SetModel("llama3.1")
	_, err := c.Generate(context.Background(), "hi")
	require.NoError(t, err)
	require.Equal(t, "llama3.1", captured)
}
```

Add imports if missing: `"context"`, `"encoding/json"`, `"net/http"`, `"net/http/httptest"`.

- [ ] **Step 2: Write the failing test for Worker.SetModel**

Append to `internal/summarizer/worker_test.go`:

```go
func TestWorker_SetModelUpdatesConfigAndClient(t *testing.T) {
	store := state.NewStore()
	w := New(Config{Model: "gemma2:2b", BaseURL: "http://127.0.0.1:11434"},
		store, func(string, int) []byte { return nil })
	require.Equal(t, "gemma2:2b", w.cfg.Model)
	w.SetModel("llama3.1")
	require.Equal(t, "llama3.1", w.cfg.Model)
	require.Equal(t, "llama3.1", w.client.model)
}
```

If `state` isn't imported, add `"github.com/tristanbietsch/rex/internal/state"`.

- [ ] **Step 3: Run to verify both fail**

Run: `go test ./internal/summarizer/ -run "SetModel" -v`
Expected: FAIL — `SetModel undefined` on both Client and Worker.

- [ ] **Step 4: Implement `Client.SetModel`**

Add to `internal/summarizer/client.go`, after the `NewClient` function:

```go
// SetModel updates which model subsequent Generate calls will target.
// Safe to call between Generate calls; not safe to call concurrently with one.
func (c *Client) SetModel(model string) {
	c.model = model
}
```

- [ ] **Step 5: Implement `Worker.SetModel`**

Add to `internal/summarizer/worker.go`, near the other Worker methods (e.g., after `MarkAvailable`):

```go
// SetModel updates both the config record and the underlying client so
// subsequent Generate calls use the new model. Intended for use by the
// daemon's health probe after substituting a fallback when the configured
// model isn't pulled. Not safe to call concurrently with Generate.
func (w *Worker) SetModel(model string) {
	w.cfg.Model = model
	w.client.SetModel(model)
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/summarizer/ -run "SetModel" -race -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/summarizer/client.go internal/summarizer/worker.go internal/summarizer/client_test.go internal/summarizer/worker_test.go
git commit -m "summarizer: add SetModel on Client and Worker"
```

---

## Task 3: Wire `resolveModel` into the daemon's Ollama health probe

The probe at `cmd/rex-daemon/main.go:267-303` currently checks for the exact configured model and flips unavailable on miss. Replace that with: look up tags, call `resolveModel`, swap the worker's model if substituted, mark available — only flip unavailable if nothing usable is pulled at all.

**Files:**
- Modify: `cmd/rex-daemon/main.go`

- [ ] **Step 1: Update `probeOllamaHealth` in `cmd/rex-daemon/main.go`**

Find the function `probeOllamaHealth` (around line 267). Replace its `check` closure (the inner function from `check := func() {` to `}` ending around line 291) with:

```go
check := func() {
	tCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tags, err := client.Tags(tCtx)
	if err != nil {
		slog.Debug("daemon: ollama unreachable", "base_url", cfg.BaseURL, "err", err)
		w.MarkUnavailable("ollama unreachable")
		return
	}
	resolved, ok := summarizer.ResolveModel(model, tags)
	if !ok {
		slog.Debug("daemon: ollama reachable but no compatible model", "configured", model, "tags", tags)
		w.MarkUnavailable("no compatible summary model pulled; try `ollama pull " + model + "`")
		return
	}
	if resolved != model {
		slog.Info("summarizer: model_substituted", "from", model, "to", resolved, "reason", "configured model not pulled")
		w.SetModel(resolved)
		model = resolved // future probes check against the substituted model
	}
	w.MarkAvailable()
}
```

The `model` variable becomes mutable through closure — that's intentional. The first probe substitutes; subsequent probes only flip back to unavailable if the substituted model itself disappears.

- [ ] **Step 2: Export `resolveModel` for use from `cmd/rex-daemon`**

In `internal/summarizer/worker.go`, rename the unexported helper to be callable from the daemon binary. Change the declaration:

```go
// Before:
func resolveModel(configured string, pulled []string) (string, bool) {

// After:
func ResolveModel(configured string, pulled []string) (string, bool) {
```

Update all test references in `internal/summarizer/worker_test.go`: search and replace `resolveModel(` with `ResolveModel(` in the six tests added in Task 1.

- [ ] **Step 3: Verify it builds**

Run: `go build ./...`
Expected: no output (success)

- [ ] **Step 4: Run summarizer tests again to confirm rename didn't break anything**

Run: `go test ./internal/summarizer/ -race`
Expected: PASS

- [ ] **Step 5: Run all daemon tests**

Run: `go test ./cmd/rex-daemon/ -race`
Expected: PASS (existing tests don't exercise the probe; this just confirms the import compiles)

- [ ] **Step 6: Commit**

```bash
git add internal/summarizer/worker.go internal/summarizer/worker_test.go cmd/rex-daemon/main.go
git commit -m "daemon: substitute summary model when configured one isn't pulled"
```

---

## Task 4: Extend `Adapter` interface with `IsReadyForInput`

Pure interface change — the implementations land in Tasks 5 and 6. After this commit, the interface compiles but neither adapter satisfies it, so the package won't build cleanly until Task 5 lands. Keep this commit small and complete the next two quickly.

**Files:**
- Modify: `internal/adapter/adapter.go`

- [ ] **Step 1: Edit `internal/adapter/adapter.go`**

Replace the `Adapter` interface (current shape: one `Detect` method) with:

```go
// Adapter classifies output chunks into states and also reports when the agent
// is ready to receive its first user input.
type Adapter interface {
	Detect(window []byte, idle time.Duration) protocol.State
	// IsReadyForInput reports whether the agent's CLI is currently parked at
	// its input prompt (or otherwise able to accept typed/pasted input).
	// Used by the supervisor's initial-prompt goroutine to decide WHEN to
	// paste the wizard's "describe the task" text.
	IsReadyForInput(window []byte, idle time.Duration) bool
}
```

- [ ] **Step 2: Verify the build BREAKS (as expected — implementations missing)**

Run: `go build ./internal/adapter/ 2>&1 | head -5`
Expected: errors like `*HeuristicCLI does not implement Adapter (missing method IsReadyForInput)` and similar for `*ClaudeStructured`.

This is intentional. Tasks 5 and 6 complete the interface. We commit the interface change separately so each implementation commit is self-contained.

- [ ] **Step 3: Commit (with a deliberately broken intermediate state, fixed in next 2 tasks)**

```bash
git add internal/adapter/adapter.go
git commit -m "adapter: add IsReadyForInput method to interface"
```

Note: the workspace is non-building between this commit and Task 6. Run tasks 5 and 6 in immediate succession; do not push or merge between them.

---

## Task 5: Implement `IsReadyForInput` on `HeuristicCLI`

Returns true when the existing `prompt_regex` matches the cleaned output window — same signal the state machine uses for `StateNeedsInput`, applied here for a related-but-distinct purpose. Idle parameter is ignored.

**Files:**
- Modify: `internal/adapter/heuristic.go`
- Modify: `internal/adapter/heuristic_test.go`

- [ ] **Step 1: Add failing tests**

Append to `internal/adapter/heuristic_test.go`:

```go
func TestHeuristic_IsReadyForInput_MatchesPromptRegex(t *testing.T) {
	h, err := NewHeuristic("^>>> ", "", 100*time.Millisecond)
	require.NoError(t, err)
	require.True(t, h.IsReadyForInput([]byte("hi there\n>>> Send a message"), 0))
}

func TestHeuristic_IsReadyForInput_NoMatch(t *testing.T) {
	h, err := NewHeuristic("^>>> ", "", 100*time.Millisecond)
	require.NoError(t, err)
	require.False(t, h.IsReadyForInput([]byte("loading model..."), 0))
}

func TestHeuristic_IsReadyForInput_IgnoresIdle(t *testing.T) {
	h, err := NewHeuristic("^>>> ", "", 100*time.Millisecond)
	require.NoError(t, err)
	// Same window, both very-low and very-high idle → same answer.
	got1 := h.IsReadyForInput([]byte(">>> "), 1*time.Millisecond)
	got2 := h.IsReadyForInput([]byte(">>> "), 60*time.Second)
	require.True(t, got1)
	require.True(t, got2)
}

func TestHeuristic_IsReadyForInput_AnsiAndCursorScrubbed(t *testing.T) {
	// Codex (v0.130.0) draws ›-prefixed prompt via cursor positioning + cyan ANSI.
	h, err := NewHeuristic("^› ", "", 100*time.Millisecond)
	require.NoError(t, err)
	raw := "\x1b[2J\x1b[H• Test received.\x1b[5;1H\x1b[K\x1b[36m› \x1b[mtype here"
	require.True(t, h.IsReadyForInput([]byte(raw), 0))
}
```

- [ ] **Step 2: Run to verify failures**

Run: `go test ./internal/adapter/ -run TestHeuristic_IsReadyForInput -v`
Expected: build error (`IsReadyForInput undefined`) or test failures.

- [ ] **Step 3: Implement `IsReadyForInput` on `HeuristicCLI`**

Add to `internal/adapter/heuristic.go`, after the `Detect` method:

```go
// IsReadyForInput returns true when the agent's prompt regex matches the
// cleaned output window. The visible prompt IS the ready signal, so no idle
// gate is applied — the idle parameter is part of the Adapter contract but
// ignored here.
func (h *HeuristicCLI) IsReadyForInput(window []byte, _ time.Duration) bool {
	tail := window
	if len(tail) > 4096 {
		tail = tail[len(tail)-4096:]
	}
	withLineBreaks := cursorPositionRe.ReplaceAllString(string(tail), "\n")
	clean := ansi.Strip(withLineBreaks)
	return h.prompt.MatchString(clean)
}
```

- [ ] **Step 4: Run all heuristic tests including the new ones**

Run: `go test ./internal/adapter/ -run TestHeuristic -race -v`
Expected: PASS (existing tests + four new `IsReadyForInput` tests)

- [ ] **Step 5: Commit**

```bash
git add internal/adapter/heuristic.go internal/adapter/heuristic_test.go
git commit -m "adapter: heuristic IsReadyForInput via prompt regex match"
```

---

## Task 6: Implement `IsReadyForInput` on `ClaudeStructured`

Add a `firstEvent bool` field that flips true on the first parsed JSON event. Returns its value (ignoring window and idle).

**Files:**
- Modify: `internal/adapter/claude.go`
- Modify: `internal/adapter/claude_test.go`

- [ ] **Step 1: Add failing tests**

Append to `internal/adapter/claude_test.go`:

```go
func TestClaudeStructured_IsReadyForInput_FalseBeforeAnyEvent(t *testing.T) {
	a := NewClaudeStructured()
	require.False(t, a.IsReadyForInput(nil, 0))
}

func TestClaudeStructured_IsReadyForInput_TrueAfterFirstEvent(t *testing.T) {
	a := NewClaudeStructured()
	// Drive a single system event through Detect to flip the flag.
	a.Detect([]byte(`{"type":"system","subtype":"init"}`+"\n"), 100*time.Millisecond)
	require.True(t, a.IsReadyForInput(nil, 0))
}

func TestClaudeStructured_IsReadyForInput_RemainsTrueAfterMoreEvents(t *testing.T) {
	a := NewClaudeStructured()
	a.Detect([]byte(`{"type":"system","subtype":"init"}`+"\n"), 100*time.Millisecond)
	a.Detect([]byte(`{"type":"assistant"}`+"\n"), 100*time.Millisecond)
	a.Detect([]byte(`{"type":"result","subtype":"success"}`+"\n"), 100*time.Millisecond)
	require.True(t, a.IsReadyForInput(nil, 0))
}
```

- [ ] **Step 2: Run to verify failures**

Run: `go test ./internal/adapter/ -run TestClaudeStructured_IsReadyForInput -v`
Expected: build errors / failures.

- [ ] **Step 3: Update `ClaudeStructured` struct and `Detect` method in `internal/adapter/claude.go`**

Modify the struct declaration (around line 12-18) to add `firstEvent`:

```go
type ClaudeStructured struct {
	mu         sync.Mutex
	last       protocol.State
	buffer     []byte
	lastSeen   time.Time
	firstEvent bool // set on first successful JSON event parse
}
```

Inside the JSON-parsing loop in `Detect` (around line 50-57 — the `switch obj["type"]` section), set `a.firstEvent = true` BEFORE the switch:

```go
// Before:
a.lastSeen = time.Now()
switch obj["type"] {

// After:
a.lastSeen = time.Now()
a.firstEvent = true
switch obj["type"] {
```

- [ ] **Step 4: Add the `IsReadyForInput` method**

Append after the existing `Detect` method (i.e., before the closing of the file or the existing `indexNewline` helper):

```go
// IsReadyForInput returns true once at least one JSON event has been parsed.
// Claude Code emits a `system` init event within ~300-800ms of spawn — well
// before the input field initializes — so this is the earliest reliable
// "agent CLI is alive" signal. Window and idle are unused but kept for
// interface consistency.
func (a *ClaudeStructured) IsReadyForInput(_ []byte, _ time.Duration) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.firstEvent
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/adapter/ -run TestClaudeStructured -race -v`
Expected: PASS — including all existing Claude tests + three new `IsReadyForInput` tests.

- [ ] **Step 6: Verify the full adapter package builds**

Run: `go build ./internal/adapter/`
Expected: no output (the interface change from Task 4 is now satisfied by both implementations).

- [ ] **Step 7: Commit**

```bash
git add internal/adapter/claude.go internal/adapter/claude_test.go
git commit -m "adapter: claude IsReadyForInput via first-JSON-event observed"
```

---

## Task 7: Rewire supervisor initial-prompt goroutine to use `IsReadyForInput`

Replace the 800ms-quiet check at `internal/pty/supervisor.go:116-170` with an adapter-driven readiness probe. If `InitialPrompt` is set but no adapter is configured, log a warning and skip the paste — there's no honest way to detect readiness without an adapter, and no current code path hits this case (every registered tool has an adapter; the only nil-adapter test, `TestSupervisor_RunEchoToCompletion`, doesn't use `InitialPrompt`).

**Files:**
- Modify: `internal/pty/supervisor.go`
- Modify: `internal/pty/supervisor_test.go`

- [ ] **Step 1: Extend `stubAdapter` in `supervisor_test.go` to satisfy the new method**

Find the `stubAdapter` declaration added in the kanban work (around lines 75-95 of `supervisor_test.go`). Replace the entire struct + method block with:

```go
// stubAdapter returns a programmable sequence of states on successive Detect
// calls and a programmable ready-flag. After the sequence is exhausted, it
// keeps returning the final element.
type stubAdapter struct {
	mu       sync.Mutex
	sequence []protocol.State
	idx      int
	ready    bool // controls IsReadyForInput
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

func (s *stubAdapter) IsReadyForInput(window []byte, idle time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ready
}

// setReady flips the stub's ready flag — used by tests to simulate the
// agent becoming ready partway through the session lifetime.
func (s *stubAdapter) setReady(b bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ready = b
}
```

- [ ] **Step 2: Add the failing test**

Append to `internal/pty/supervisor_test.go`:

```go
func TestSupervisor_InitialPromptWaitsForIsReadyForInput(t *testing.T) {
	stateDir := t.TempDir()
	store := state.NewStore()
	sess := &state.Session{
		ID: "id1", ShortID: "id1", ToolID: "echo", Slug: "test",
		State: protocol.StateQueued, StartedAt: time.Now().UTC(),
	}
	require.NoError(t, store.Add(sess))

	stub := &stubAdapter{ready: false}

	sup := New(SupervisorConfig{
		StateDir:      stateDir,
		Store:         store,
		Command:       []string{"bash", "-c", "while :; do echo tick; sleep 0.2; done"},
		Adapter:       stub,
		InitialPrompt: "hello-from-test",
		IdleTick:      50 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Flip ready=true after 400ms — long after the 200ms readiness poll would
	// have fired with the old behavior, but we should still see the paste.
	go func() {
		time.Sleep(400 * time.Millisecond)
		stub.setReady(true)
	}()

	done := make(chan error, 1)
	go func() { done <- sup.Run(ctx, sess) }()

	// Wait long enough for the paste + submit. The supervisor never returns
	// for this command, so cancel and read the result.
	time.Sleep(900 * time.Millisecond)
	cancel()
	<-done

	// Verify the transcript contains the bracketed-paste payload AND its
	// content, written after ready flipped.
	tail, err := state.TranscriptTail(stateDir, sess.ID, 8192)
	require.NoError(t, err)
	require.Contains(t, string(tail), "hello-from-test",
		"prompt should have landed in the transcript once IsReadyForInput went true")
}

func TestSupervisor_InitialPromptNilAdapterIsNoop(t *testing.T) {
	// When InitialPrompt is set but no adapter is configured, the supervisor
	// has no readiness signal — it should log and skip the paste rather than
	// guess. Verifies the supervisor still completes cleanly (i.e., the
	// initial-prompt goroutine exits without leaking).
	stateDir := t.TempDir()
	store := state.NewStore()
	sess := &state.Session{
		ID: "id2", ShortID: "id2", ToolID: "echo", Slug: "test",
		State: protocol.StateQueued, StartedAt: time.Now().UTC(),
	}
	require.NoError(t, store.Add(sess))

	sup := New(SupervisorConfig{
		StateDir:      stateDir,
		Store:         store,
		Command:       []string{"bash", "-c", "echo hello; echo done"},
		Adapter:       nil,
		InitialPrompt: "should-not-appear",
		IdleTick:      50 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, sup.Run(ctx, sess))

	tail, _ := state.TranscriptTail(stateDir, sess.ID, 8192)
	require.NotContains(t, string(tail), "should-not-appear",
		"no-adapter sessions should NOT receive the initial prompt — no readiness signal to gate on")
}
```

- [ ] **Step 3: Run tests to verify the new ready-gated test fails**

Run: `go test ./internal/pty/ -run "TestSupervisor_InitialPrompt" -v`
Expected: `TestSupervisor_InitialPromptWaitsForIsReadyForInput` may pass or fail depending on timing — the existing 800ms-quiet code will fire as soon as the command's output quiets, which never happens for the `while :; do echo tick;` loop. But the test asserts the prompt LANDED, which the OLD code might still achieve if ticks have brief enough quiet gaps. Run a few times to confirm.

A more reliable failure mode: the nil-adapter test should pass with old code (preserving existing behavior); the IsReadyForInput test should fail because the current code never calls that method.

If the test happens to pass against the unchanged supervisor, force the failure by adding `t.Skip("baseline check")` to step 4's implementation and re-running. The test is correct; the timing edge is hard to demonstrate from a unit test alone.

- [ ] **Step 4: Replace the readiness check in `internal/pty/supervisor.go`**

Find the `if s.cfg.InitialPrompt != ""` block (line 120 area). Inside the goroutine, find the `case <-poll.C:` body. Replace ONLY the readiness-detection lines (the `windowMu.Lock(); ready := ...; windowMu.Unlock(); if !ready { continue }` block).

Also extend the early return: if `InitialPrompt != ""` but `Adapter == nil`, the goroutine has no honest signal to wait on. Skip the paste entirely and log a warning. Add this check at the top of the `go func(prompt string) {` body, BEFORE the existing `deadline := time.NewTimer(...)` line:

```go
if s.cfg.Adapter == nil {
    slog.Warn("pty: initial_prompt set but no adapter; cannot detect readiness", "session", sess.ID)
    return
}
```

Then in the same goroutine's `case <-poll.C:` branch, replace the existing readiness-detection lines:

```go
case <-poll.C:
    windowMu.Lock()
    ready := len(window) > 0 && time.Since(lastChunk) > 800*time.Millisecond
    windowMu.Unlock()
    if !ready {
        continue
    }
```

With:

```go
case <-poll.C:
    windowMu.Lock()
    idle := time.Since(lastChunk)
    snap := append([]byte(nil), window...)
    windowMu.Unlock()
    if !s.cfg.Adapter.IsReadyForInput(snap, idle) {
        continue
    }
```

Everything below (paste + 120ms delay + `\r` + return) is unchanged. The nil-adapter check at the top guarantees we never reach this point without an adapter, so the call is safe.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/pty/ -race`
Expected: PASS for both new tests and all existing supervisor tests.

- [ ] **Step 6: Commit**

```bash
git add internal/pty/supervisor.go internal/pty/supervisor_test.go
git commit -m "pty: gate initial-prompt paste on adapter IsReadyForInput"
```

---

## Task 8: Add `default_spawn_*` settings keys

Three new settings keys backing the `i` quick-spawn. Out-of-the-box defaults are `claude / opus / max`.

**Files:**
- Modify: `internal/settings/registry.go`
- Modify: `internal/settings/registry_test.go`

- [ ] **Step 1: Add the failing tests**

Append to `internal/settings/registry_test.go`:

```go
func TestRegistry_HasDefaultSpawnTool(t *testing.T) {
	e, ok := findEntry("default_spawn_tool")
	require.True(t, ok, "default_spawn_tool key must be registered")
	require.Equal(t, "claude", e.Default)
}

func TestRegistry_HasDefaultSpawnModel(t *testing.T) {
	e, ok := findEntry("default_spawn_model")
	require.True(t, ok)
	require.Equal(t, "opus", e.Default)
}

func TestRegistry_HasDefaultSpawnEffort(t *testing.T) {
	e, ok := findEntry("default_spawn_effort")
	require.True(t, ok)
	require.Equal(t, "max", e.Default)
}

// findEntry is a small helper for these tests — locates an entry by ID.
func findEntry(id string) (Entry, bool) {
	for _, e := range Entries() {
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}
```

Check whether `Entries()` exists with that exact name. If the registry exposes the entries via a different accessor (e.g., `All()` or an exported variable), adjust `findEntry` accordingly. Run `grep -n "^func .* \\*Registry\\|^func Entries\\|^var.*Entries\\|^var.*defaultEntries" internal/settings/registry.go` to find it.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/settings/ -run "DefaultSpawn" -v`
Expected: FAIL — keys not registered.

- [ ] **Step 3: Add `SectionSpawn` constant + three entries**

In `internal/settings/registry.go`:

(a) Find where other Section constants are declared (likely near the top — e.g., `SectionAppearance`, `SectionAudio`, `SectionBehavior`, `SectionSummary`). Add:

```go
const SectionSpawn Section = "Spawn"
```

(b) Find the slice or function that registers all default entries (likely a `var defaultEntries = []Entry{...}` or `func Entries() []Entry`). Append three entries — place them adjacent to other related sections (after `SectionSummary` is natural):

```go
{
    ID: "default_spawn_tool", Label: "Quick-spawn tool", Section: SectionSpawn,
    Type: TypeString, Default: "claude",
    Help: "Tool ID used when pressing 'i' in the TUI. Must match an entry in tools.yaml.",
},
{
    ID: "default_spawn_model", Label: "Quick-spawn model", Section: SectionSpawn,
    Type: TypeString, Default: "opus",
    Help: "Model ID used when pressing 'i'. Must be a model offered by the selected tool.",
},
{
    ID: "default_spawn_effort", Label: "Quick-spawn effort", Section: SectionSpawn,
    Type: TypeString, Default: "max",
    Help: "Effort tier used when pressing 'i'. Ignored for tools without an effort axis.",
},
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/settings/ -run "DefaultSpawn" -race -v`
Expected: PASS for all three.

- [ ] **Step 5: Run the full settings suite to catch any unrelated breakage**

Run: `go test ./internal/settings/ -race`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/settings/registry.go internal/settings/registry_test.go
git commit -m "settings: add default_spawn_{tool,model,effort} keys (SectionSpawn)"
```

---

## Task 9: Rewire `spawnSessionCmd` to use settings + reuse wizard slug helpers

The TUI's `spawnSessionCmd` currently hardcodes `ToolID: "echo", ModelID: "short"` (`internal/tui/update.go:411-417`). Replace those values with reads from `m.Store`, and route through the wizard's existing `deriveAgentSlug` helper so the slug matches the format other sessions use (e.g., `cc.opus.fix-auth-bug`).

**Files:**
- Modify: `internal/tui/update.go`
- Modify: `internal/tui/update_test.go` (create if missing)

- [ ] **Step 1: Check whether `update_test.go` already exists**

Run: `ls internal/tui/update_test.go 2>/dev/null && echo found || echo missing`

If "missing", create `internal/tui/update_test.go` with the package declaration:

```go
package tui

import (
	"testing"

	"github.com/stretchr/testify/require"
)
```

- [ ] **Step 2: Add the failing test**

Append to `internal/tui/update_test.go`:

```go
func TestDeriveAgentSlug_QuickSpawnFormat(t *testing.T) {
	// Confirms the wizard's slug helper produces the format spawnSessionCmd
	// will use after Task 9 — guards against any future drift if the helpers
	// are refactored.
	got := deriveAgentSlug("claude", "opus", "fix auth bug", nil)
	require.Equal(t, "cc.opus.fix-auth-bug", got)

	got = deriveAgentSlug("ollama", "llama3.1", "write readme", nil)
	require.Equal(t, "ol.llama3-1.write-readme", got)
}
```

- [ ] **Step 3: Run to verify it passes (it's a regression guard, not a TDD fail)**

Run: `go test ./internal/tui/ -run TestDeriveAgentSlug_QuickSpawnFormat -v`
Expected: PASS — wizard helpers already produce these outputs.

- [ ] **Step 4: Rewrite `spawnSessionCmd` in `internal/tui/update.go`**

Find the existing `spawnSessionCmd` (around lines 404-422). Replace the entire function with:

```go
// spawnSessionCmd is invoked from `i`-keybind quick-spawn. It reads the
// configured default tool/model/effort from the settings store and uses the
// wizard's existing slug helpers so a quick-spawned session is indistinguishable
// from one created via the full wizard. Daemon-side validation surfaces errors
// for unknown tool/model IDs via the existing DaemonErrMsg path.
func spawnSessionCmd(c *client.Client, store *settings.Store, sessions []protocol.SessionSummary, prompt string) tea.Cmd {
	return func() tea.Msg {
		toolID, _ := store.Get("default_spawn_tool").(string)
		modelID, _ := store.Get("default_spawn_model").(string)
		effort, _ := store.Get("default_spawn_effort").(string)

		existing := make([]string, 0, len(sessions))
		for _, s := range sessions {
			if s.Slug != "" {
				existing = append(existing, s.Slug)
			}
		}
		slug := deriveAgentSlug(toolID, modelID, prompt, existing)

		cwd, _ := os.Getwd()
		if err := c.NewSession(protocol.NewSession{
			ToolID:        toolID,
			ModelID:       modelID,
			Effort:        effort,
			Slug:          slug,
			CWD:           cwd,
			InitialPrompt: prompt,
		}); err != nil {
			return DaemonErrMsg{Err: err}
		}
		return nil
	}
}
```

- [ ] **Step 5: Update the caller in `updatePromptKey`**

Find `updatePromptKey` (around line 374-402). The relevant call is around line 388: `return m, spawnSessionCmd(m.Client, text)`. Replace with:

```go
return m, spawnSessionCmd(m.Client, m.Store, m.Sessions, text)
```

- [ ] **Step 6: Add the `settings` import if not present**

Check the imports at the top of `update.go`:

```go
import (
    "github.com/tristanbietsch/rex/internal/settings"
)
```

Add if missing. The `protocol` import should already be there from the existing code.

- [ ] **Step 7: Build to catch any signature errors**

Run: `go build ./...`
Expected: no output (success).

- [ ] **Step 8: Add a more meaningful test that asserts the spawn intent contents**

Append to `internal/tui/update_test.go`:

```go
func TestSpawnSessionCmd_UsesSettingsDefaults(t *testing.T) {
	// Build a settings store with the documented out-of-the-box defaults.
	// (If the store API is settings.NewStore() vs settings.New() vs something
	// else, follow the existing test fixture pattern in the settings package.)
	store := settings.NewDefaultStore() // adjust to the actual constructor name

	// Capture the NewSession payload by using a fake Client that records intents.
	// If client.Client can't be easily faked here, this test can also drive the
	// full daemon path via the existing server e2e harness — but the unit-level
	// shape is what we want to lock in.
	captured := struct {
		ToolID, ModelID, Effort, Slug string
	}{}
	fake := newRecordingClient(func(req protocol.NewSession) {
		captured.ToolID = req.ToolID
		captured.ModelID = req.ModelID
		captured.Effort = req.Effort
		captured.Slug = req.Slug
	})

	cmd := spawnSessionCmd(fake, store, nil, "fix auth bug")
	_ = cmd() // execute the tea.Cmd

	require.Equal(t, "claude", captured.ToolID)
	require.Equal(t, "opus", captured.ModelID)
	require.Equal(t, "max", captured.Effort)
	require.Equal(t, "cc.opus.fix-auth-bug", captured.Slug)
}
```

**If `settings.NewDefaultStore` doesn't exist**, run `grep -n "func New\\|func .* Store" internal/settings/*.go` to find the constructor pattern and the API for setting/getting defaults. Adjust the test setup accordingly. Same for `newRecordingClient` — if no fake-client harness exists in the package, the simplest pattern is an inline `Client` shim with a single-method interface:

```go
type clientLike interface {
    NewSession(protocol.NewSession) error
}

func newRecordingClient(onSession func(protocol.NewSession)) *recordingClient { ... }
```

If `*client.Client` is a concrete type with no interface (likely), and you can't easily fake it, change `spawnSessionCmd`'s signature to accept the interface above (3-line refactor) and accept the small test indirection. **Do not skip this test** — it's the load-bearing guarantee that pressing `i` actually spawns Claude.

- [ ] **Step 9: Run tests**

Run: `go test ./internal/tui/ -race -v`
Expected: PASS, including the slug-format guard and the spawn-defaults test.

- [ ] **Step 10: Commit**

```bash
git add internal/tui/update.go internal/tui/update_test.go
git commit -m "tui: i-key quick-spawn reads default_spawn_* from settings"
```

---

## Task 10: Documentation

Document the three user-facing changes.

**Files:**
- Modify: `docs/settings.md` (or wherever settings keys are listed)
- Modify: `docs/tui.md`
- Modify: `docs/registry.md`

- [ ] **Step 1: Document the new settings keys in `docs/settings.md`**

Find the table or list of settings (search for `summary_enabled` or another key as an anchor). Insert a new section or rows for the spawn defaults:

```markdown
### Spawn

| Key | Default | Description |
|-----|---------|-------------|
| `default_spawn_tool` | `claude` | Tool ID used when pressing `i` in the TUI. Must match an entry in tools.yaml. |
| `default_spawn_model` | `opus` | Model ID for `i`-spawn. Must be a model offered by the configured tool. |
| `default_spawn_effort` | `max` | Effort tier for `i`-spawn. Ignored for tools without an effort axis. |
```

- [ ] **Step 2: Document the new `i` behavior in `docs/tui.md`**

Find the keybinds table. Locate the `i` entry (or add one if missing). Update the description to reflect the new behavior:

```markdown
| `i` | Quick-spawn: focus the prompt, type a task, press enter to spawn a session with the configured default tool/model/effort (out of the box: Claude Opus, max effort). Change in Settings → Spawn. |
```

- [ ] **Step 3: Document the `prompt_regex` dual-use in `docs/registry.md`**

Find the heuristic `detect` section. Add a one-paragraph note explaining the dual semantic of `prompt_regex`:

```markdown
> The `prompt_regex` is used for two purposes: classifying the session state
> (matches → `needs_input`) AND deciding when the agent is ready to receive
> its initial prompt from the wizard. A correctly-tuned regex makes both
> behaviors work without extra config. For tools whose prompt is identical
> between "awaiting" and "just finished" (codex, gemini, ollama), this is
> fine — the readiness signal fires once at first prompt appearance, then
> the state machine takes over.
```

- [ ] **Step 4: Commit**

```bash
git add docs/settings.md docs/tui.md docs/registry.md
git commit -m "docs: spawn defaults, i-key quick-spawn, prompt_regex dual-use"
```

---

## Task 11: Full regression + smoke test

- [ ] **Step 1: Run the full test suite with race detector**

Run: `go test -race ./...`
Expected: PASS for every package. (Note: a pre-existing `TestLoad_UserExtends` failure in `internal/registry` may still appear — confirm it failed before this branch with `git stash && git checkout master -- internal/registry && go test ./internal/registry/ -run TestLoad_UserExtends; git checkout -; git stash pop`. Out of scope.)

- [ ] **Step 2: Run vet + build**

Run:
```bash
go vet ./...
go build ./...
```
Expected: no output for either.

- [ ] **Step 3: End-to-end smoke: AI summary**

Stop any running daemon (`pkill rex-daemon` or use existing daemon control). Then:

```bash
go build -o rex ./cmd/rex
go build -o rex-daemon ./cmd/rex-daemon
./rex-daemon &
sleep 1
```

Check the daemon log for the substitution event:

```bash
tail -n 50 ~/.local/state/rex/daemon.log | grep -E "summarizer|model_substituted"
```

Expected: one of:
- `summarizer: model_substituted from=gemma2:2b to=<your-pulled-model>` (if the configured default isn't pulled)
- `daemon: summarizer health flip available=true` (no substitution because configured model is present)

Either way, `available=true` is the success criterion. If you see `available=false reason="no compatible summary model pulled..."`, pull at least one model: `ollama pull gemma2:2b`.

- [ ] **Step 4: End-to-end smoke: prompt delivery for Claude / Codex / Gemini / Ollama**

Open `./rex` (TUI). Press `n` to walk through the wizard. Pick each of `claude`, `codex`, `gemini`, `ollama` in turn (one at a time — quit and re-launch between to keep the board clear). At the "describe the task" step (image 3 in the brief), enter `this is a test`. Hit enter.

Expected for ALL FOUR:
- Within 5-10 seconds the session's last-line / description shows the agent processing the prompt (NOT the agent's idle landing screen).
- The TUI's row description column will show an AI-generated summary (if the summarizer is healthy from Step 3) or the raw last-line output (if it isn't).

If any agent still shows its idle landing screen instead of processing the prompt: the `IsReadyForInput` signal isn't firing for that tool. Tail the daemon log for `pty: initial_prompt timed out waiting for ready` and report which tool/session.

- [ ] **Step 5: End-to-end smoke: `i` quick-spawn defaults**

In the TUI, press `i`. The bottom `λ` prompt focuses. Type `make a tiny test` and hit enter.

Expected:
- A new session appears with slug `cc.opus.make-a-tiny-test`
- Tool column shows `claude` / `opus`
- Effort badge (if displayed) shows `max`
- Daemon log shows `pty: started ... argv="[claude --output-format=stream-json --verbose --model opus ...]"`

Now change the default. Press `S` for Settings, find `Spawn` section, change `default_spawn_tool` to `ollama` and `default_spawn_model` to `llama3.1` (or whichever you have pulled). Exit Settings. Press `i` again, type `another test`.

Expected:
- New session slug starts with `ol.llama3-1.`
- Tool column shows `ollama / llama3.1`

- [ ] **Step 6: No commit (verification only)**

If everything passes, the feature is shipped. If any step fails, file the daemon log excerpt + the symptom.

---

## Self-review notes

Spec coverage:

- §1 Summarizer auto-discovery → Tasks 1, 2, 3
- §1 SummarizerHealth reason string update → Task 3 Step 1 (folded into the probe rewrite)
- §2 `Adapter.IsReadyForInput` interface → Task 4
- §2 HeuristicCLI implementation → Task 5
- §2 ClaudeStructured implementation + `firstEvent` field → Task 6
- §2 Supervisor rewire → Task 7
- §3 `SectionSpawn` + three keys → Task 8
- §3 `spawnSessionCmd` rewire using existing `deriveAgentSlug` → Task 9
- §3 Caller update (`updatePromptKey`) → Task 9 Step 5
- Section 6 (docs) → Task 10
- Smoke verification → Task 11

Sequencing: Tasks 1, 2, 3 are sequential (Task 3 depends on `ResolveModel` from Task 1 + `SetModel` from Task 2). Task 4 → 5 + 6 (5 and 6 can run in parallel after 4). Task 7 depends on 5 and 6 (the interface must have both implementations). Task 8 is independent (settings). Task 9 depends on 8 (reads the new keys) and is independent of the adapter chain. Task 10 docs depend on everything. Task 11 verifies.

**Deliberate deviation from the spec:** the spec proposed extracting a `slug.go` file with copied `toolShortID`/`modelShortID` helpers. The plan instead **reuses the wizard's existing `deriveAgentSlug`/`toolShort`/`modelShort` helpers directly** (they live in the same `tui` package). One less file, one fewer chance of divergence. Functionally identical.
