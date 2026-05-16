# AI Description Column Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the middle "desc" column in the rex board with a stable, AI-generated activity description per session, produced by a local Ollama daemon and rendered with a configurable text animation when it changes.

**Architecture:** A new `internal/summarizer/` package runs a single-goroutine worker in `rex-daemon`. The PTY supervisor signals "session is idle with new bytes" onto a channel; the worker reads the sanitized transcript tail, calls Ollama via HTTP, and writes the result to `state.Store.UpdateDescription`. The existing event broker delivers the change to the TUI, which renders the new `s.Description` field through one of four animation effects.

**Tech Stack:** Go 1.24, `charmbracelet/bubbletea`, `charmbracelet/lipgloss`, local Ollama HTTP API (`/api/generate`, `/api/tags`), `gopkg.in/yaml.v3` for settings persistence (already in use), `log/slog` for structured logging.

**Reference spec:** `docs/superpowers/specs/2026-05-15-ai-description-column-design.md` (commit `4d1c1e8`).

---

## File structure

**New files:**
- `internal/summarizer/config.go` — `Config` struct + defaults
- `internal/summarizer/client.go` — Ollama HTTP client (keep-alive)
- `internal/summarizer/client_test.go`
- `internal/summarizer/prompt.go` — prompt builder + response cleaner
- `internal/summarizer/prompt_test.go`
- `internal/summarizer/worker.go` — channel consumer; dedup, skip-if-unchanged, retry, health flag
- `internal/summarizer/worker_test.go`
- `internal/tui/anim.go` — `DescAnim` struct + `renderAnimFrame` + effect implementations
- `internal/tui/anim_test.go`

**Modified files:**
- `internal/protocol/events.go` — `Description` on `SessionSummary`; new `EventSummarizerHealth` constant + `SummarizerHealth` payload
- `internal/state/session.go` — `Description`, `DescriptionAt` fields; updated `Summary()` and `fromSummary()`
- `internal/state/store.go` — `UpdateDescription` method
- `internal/pty/supervisor.go` — `SummaryRequest chan<- string` in `SupervisorConfig`; `dirty bool` + `lastSummaryAt time.Time` in `Run()`; trigger emission in the existing `ticker.C` branch
- `internal/server/server.go` (or wherever `Config` lives) — pass `SummaryRequest` through `server.Config` → `SupervisorConfig`
- `internal/server/client.go` — forward `SummaryRequest` from server config into `pty.SupervisorConfig`
- `internal/settings/types.go` — add `SectionSummary` section
- `internal/settings/registry.go` — three new settings: `summary_enabled`, `summary_model`, `desc_animation`
- `internal/tui/model.go` — `DescAnim map[string]DescAnim`, `BackendUnavailable bool`, `BackendUnavailableReason string`, `applyPatch` handling `description`
- `internal/tui/update.go` — register `DescAnim` on `SessionUpdated` when description changes; `descTickMsg` handler; consume `SummarizerHealth` envelope
- `internal/tui/board.go` — read `s.Description` (fallback `LastLine`); apply animation frame
- `internal/tui/header.go` — render dim banner when `BackendUnavailable`
- `internal/tui/settings.go` — implicit (registry-driven; verify cycling works for new enum)
- `cmd/rex-daemon/main.go` — load `settings.Store`, build `summarizer.Worker`, run health probe, kick the worker, thread `Worker.Channel()` through `server.Config`

---

## Task 1: state — add Description + DescriptionAt fields, persistence, applyPatch hook

**Files:**
- Modify: `internal/protocol/events.go`
- Modify: `internal/state/session.go`
- Modify: `internal/state/persist.go` (no edits needed — `fromSummary` change covers it)
- Modify: `internal/tui/model.go:applyPatch`
- Test: `internal/state/store_test.go` (or new `session_test.go` if no `store_test.go` covers Summary roundtrip)

- [ ] **Step 1: Write the failing test**

If `internal/state/store_test.go` doesn't exist, create it. Otherwise append.

```go
// internal/state/store_test.go
package state

import (
	"testing"
	"time"

	"github.com/tristanbietsch/rex/internal/protocol"
)

func TestSummaryRoundtripsDescription(t *testing.T) {
	sess := &Session{
		ID:            "id-1",
		ShortID:       "abcd",
		Slug:          "test",
		State:         protocol.StateWorking,
		StartedAt:     time.Now().UTC(),
		LastEventAt:   time.Now().UTC(),
		LastLine:      "raw line",
		Description:   "running pnpm test",
		DescriptionAt: time.Now().UTC(),
	}
	sum := sess.Summary()
	if sum.Description != "running pnpm test" {
		t.Fatalf("Summary.Description: got %q want %q", sum.Description, "running pnpm test")
	}
	back := fromSummary(sum)
	if back.Description != "running pnpm test" {
		t.Fatalf("fromSummary.Description: got %q want %q", back.Description, "running pnpm test")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/state/ -run TestSummaryRoundtripsDescription -v`
Expected: FAIL — `Session.Description` undefined.

- [ ] **Step 3: Add `Description` to `protocol.SessionSummary`**

Edit `internal/protocol/events.go` — inside `SessionSummary`, after `LastLine`:

```go
LastLine    string    `json:"last_line,omitempty"`
Description string    `json:"description,omitempty"`
ExitCode    *int      `json:"exit_code,omitempty"`
```

- [ ] **Step 4: Add fields to `state.Session` and update `Summary` / `fromSummary`**

Edit `internal/state/session.go`. After `LastLine`:

```go
LastLine      string
Description   string
DescriptionAt time.Time
ExitCode      *int
```

In `Summary()`, add inside the returned struct:

```go
LastLine:    s.LastLine,
Description: s.Description,
ExitCode:    s.ExitCode,
```

In `fromSummary` at the bottom of the file, extend the literal:

```go
return &Session{
    ID: sum.ID, ShortID: sum.ShortID, ToolID: sum.ToolID, ModelID: sum.ModelID,
    Effort: sum.Effort, Slug: sum.Slug, Title: sum.Title, CWD: sum.CWD,
    State: sum.State, StartedAt: sum.StartedAt, LastEventAt: sum.LastEventAt,
    LastLine: sum.LastLine, Description: sum.Description, ExitCode: sum.ExitCode,
}
```

`DescriptionAt` is daemon-internal — not on the wire and not persisted; leave it as a zero-value default for restored sessions.

- [ ] **Step 5: Handle `description` in TUI patch**

Edit `internal/tui/model.go:applyPatch`, after the `last_line` block:

```go
if v, ok := patch["last_line"].(string); ok {
    s.LastLine = v
}
if v, ok := patch["description"].(string); ok {
    s.Description = v
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/state/ -run TestSummaryRoundtripsDescription -v`
Expected: PASS.

Also build everything to catch the protocol/TUI churn:
Run: `go build ./...`
Expected: builds clean.

- [ ] **Step 7: Commit**

```bash
git add internal/protocol/events.go internal/state/session.go internal/state/store_test.go internal/tui/model.go
git commit -m "state: Description field on Session + SessionSummary"
```

---

## Task 2: state.Store.UpdateDescription method

**Files:**
- Modify: `internal/state/store.go`
- Test: `internal/state/store_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/state/store_test.go`:

```go
func TestUpdateDescriptionBroadcasts(t *testing.T) {
	s := NewStore()
	sess := &Session{ID: "id-2", ShortID: "ef01", StartedAt: time.Now().UTC()}
	if err := s.Add(sess); err != nil {
		t.Fatalf("Add: %v", err)
	}
	var got string
	var gotKind EventKind
	s.Subscribe(func(e Event) {
		if e.Kind == EventUpdated {
			if v, ok := e.Patch["description"].(string); ok {
				got = v
				gotKind = e.Kind
			}
		}
	})
	if err := s.UpdateDescription("id-2", "rewriting webhook handlers"); err != nil {
		t.Fatalf("UpdateDescription: %v", err)
	}
	if got != "rewriting webhook handlers" {
		t.Fatalf("broadcast description: got %q", got)
	}
	if gotKind != EventUpdated {
		t.Fatalf("event kind: got %v want EventUpdated", gotKind)
	}
	if sess.Description != "rewriting webhook handlers" {
		t.Fatalf("session field: got %q", sess.Description)
	}
	if sess.DescriptionAt.IsZero() {
		t.Fatalf("DescriptionAt not set")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/state/ -run TestUpdateDescriptionBroadcasts -v`
Expected: FAIL — `UpdateDescription` undefined.

- [ ] **Step 3: Implement `UpdateDescription`**

Add to `internal/state/store.go` directly after `UpdateLastLine`:

```go
// UpdateDescription records the AI-generated activity summary for a session.
// Mirrors UpdateLastLine: takes locks, updates the field, and broadcasts a
// SessionUpdated patch.
func (s *Store) UpdateDescription(id, desc string) error {
	s.mu.RLock()
	sess, ok := s.sessions[id]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("session %s not found", id)
	}
	now := time.Now().UTC()
	sess.mu.Lock()
	sess.Description = desc
	sess.DescriptionAt = now
	sess.LastEventAt = now
	sess.mu.Unlock()

	s.broadcast(Event{
		Kind:      EventUpdated,
		SessionID: id,
		Patch:     map[string]any{"description": desc, "last_event_at": now},
	})
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/state/ -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/state/store.go internal/state/store_test.go
git commit -m "state: UpdateDescription method emits SessionUpdated"
```

---

## Task 3: PTY supervisor — SummaryRequest channel + trigger logic

**Files:**
- Modify: `internal/pty/supervisor.go`
- Test: `internal/pty/supervisor_test.go`

The trigger fires inside the existing `ticker.C` branch. No new ticker.

- [ ] **Step 1: Write the failing test**

Append to `internal/pty/supervisor_test.go` (the file already exists for `lastNonEmptyLine` tests):

```go
import (
	"testing"
	"time"
	// existing imports kept
)

// TestSummaryTriggerFunc verifies the standalone trigger predicate used inside
// the supervisor's ticker branch. Kept as a pure function to make the timing
// behavior testable without spawning a child process.
func TestSummaryTriggerFunc(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name        string
		dirty       bool
		idle        time.Duration
		sinceSubmit time.Duration
		want        bool
	}{
		{"clean: never fire", false, 1 * time.Second, 1 * time.Second, false},
		{"dirty + idle below threshold + ceiling cold", true, 200 * time.Millisecond, 1 * time.Second, false},
		{"dirty + idle reached", true, 500 * time.Millisecond, 1 * time.Second, true},
		{"dirty + ceiling reached even though busy", true, 50 * time.Millisecond, 16 * time.Second, true},
		{"dirty but neither idle nor ceiling", true, 200 * time.Millisecond, 5 * time.Second, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lastSubmittedAt := now.Add(-tc.sinceSubmit)
			lastChunk := now.Add(-tc.idle)
			got := shouldEmitSummary(tc.dirty, lastChunk, lastSubmittedAt, now)
			if got != tc.want {
				t.Fatalf("shouldEmitSummary(dirty=%v idle=%v sinceSubmit=%v) = %v want %v",
					tc.dirty, tc.idle, tc.sinceSubmit, got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pty/ -run TestSummaryTriggerFunc -v`
Expected: FAIL — `shouldEmitSummary` undefined.

- [ ] **Step 3: Implement `shouldEmitSummary`**

Add to `internal/pty/supervisor.go` (alongside `lastNonEmptyLine` near the bottom):

```go
// summaryIdleThreshold is the quiet-period after a burst that marks the
// "natural beat" for re-summarizing. 500ms matches a typical agent pause
// between operations and never races the 200ms IdleTick.
const summaryIdleThreshold = 500 * time.Millisecond

// summaryCeiling is the maximum interval between summaries for a never-idle
// (continuously chatty) session.
const summaryCeiling = 15 * time.Second

// shouldEmitSummary is the pure predicate used in the ticker branch.
func shouldEmitSummary(dirty bool, lastChunk, lastSubmittedAt, now time.Time) bool {
	if !dirty {
		return false
	}
	if now.Sub(lastChunk) >= summaryIdleThreshold {
		return true
	}
	if now.Sub(lastSubmittedAt) >= summaryCeiling {
		return true
	}
	return false
}
```

- [ ] **Step 4: Add `SummaryRequest` to `SupervisorConfig`**

Edit `internal/pty/supervisor.go` — inside the `SupervisorConfig` struct, after `OutputSink`:

```go
OutputSink       func(b []byte)  // called with every chunk read from PTY; non-blocking
SummaryRequest   chan<- string   // optional: session IDs needing AI summary; nil disables
```

- [ ] **Step 5: Wire `dirty` + trigger emission**

In the reader goroutine inside `Run()`, around line 184 (existing `visible := hasVisibleText(chunk)` block). Replace this region:

```go
visible := hasVisibleText(chunk)
windowMu.Lock()
window = appendBounded(window, chunk, 8192)
if visible {
    lastChunk = time.Now()
}
```

with:

```go
visible := hasVisibleText(chunk)
windowMu.Lock()
window = appendBounded(window, chunk, 8192)
if visible {
    lastChunk = time.Now()
    dirty = true
}
```

Declare `dirty` and `lastSummaryAt` alongside `windowMu` / `window` / `lastChunk` near the top of `Run()`:

```go
var windowMu sync.Mutex
window := make([]byte, 0, 8192)
lastChunk := time.Now()
dirty := false
lastSummaryAt := time.Now()
errc := make(chan error, 1)
```

In the `case <-ticker.C:` branch, **after** the adapter classification block (so the existing behavior is unchanged), add:

```go
case <-ticker.C:
    if s.cfg.Adapter == nil {
        // existing path
    }
    // ... existing adapter classification ...

    // Summary trigger (additive — independent of adapter).
    if s.cfg.SummaryRequest != nil {
        windowMu.Lock()
        emit := shouldEmitSummary(dirty, lastChunk, lastSummaryAt, time.Now())
        windowMu.Unlock()
        if emit {
            select {
            case s.cfg.SummaryRequest <- sess.ID:
                windowMu.Lock()
                dirty = false
                lastSummaryAt = time.Now()
                windowMu.Unlock()
                slog.Debug("pty: summary signal sent", "session", sess.ID)
            default:
                slog.Debug("pty: summary worker busy, will retry next tick", "session", sess.ID)
            }
        }
    }
```

(Re-read the existing `case <-ticker.C:` block first; insert the summary trigger as a sibling of the adapter block, not nested inside the `if s.cfg.Adapter == nil { continue }` early-return. If the adapter is nil, the summary trigger should still run.)

- [ ] **Step 6: Run unit test to verify**

Run: `go test ./internal/pty/ -run TestSummaryTriggerFunc -v`
Expected: PASS.

Run: `go build ./...`
Expected: builds clean.

- [ ] **Step 7: Commit**

```bash
git add internal/pty/supervisor.go internal/pty/supervisor_test.go
git commit -m "pty: emit summary-request signals on idle-debounce + 15s ceiling"
```

---

## Task 4: summarizer package — Config + Client

**Files:**
- Create: `internal/summarizer/config.go`
- Create: `internal/summarizer/client.go`
- Test: `internal/summarizer/client_test.go`

- [ ] **Step 1: Create `config.go`**

```go
// Package summarizer drives a local Ollama daemon to produce a one-line activity
// description per active session. It consumes session IDs from a channel and
// writes results into state.Store.
package summarizer

import "time"

// Config is the per-process configuration for the summarizer worker.
type Config struct {
	BaseURL        string        // Ollama base URL; default "http://127.0.0.1:11434"
	Model          string        // Ollama model name; default "gemma2:2b"
	RequestTimeout time.Duration // per HTTP call; default 4s
	MinInterval    time.Duration // per-session floor between Ollama calls; default 800ms
	MaxBytes       int           // transcript window size sent to the model; default 2048
}

// Defaults returns a Config populated with library-wide defaults.
func Defaults() Config {
	return Config{
		BaseURL:        "http://127.0.0.1:11434",
		Model:          "gemma2:2b",
		RequestTimeout: 4 * time.Second,
		MinInterval:    800 * time.Millisecond,
		MaxBytes:       2048,
	}
}
```

- [ ] **Step 2: Write the failing client test**

```go
// internal/summarizer/client_test.go
package summarizer

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientGenerateOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Fatalf("path: %s", r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "gemma2:2b" {
			t.Fatalf("model: %v", body["model"])
		}
		if !strings.Contains(body["prompt"].(string), "TRANSCRIPT") {
			t.Fatalf("prompt missing transcript marker: %v", body["prompt"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"response":"running pnpm test"}`)
	}))
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, Model: "gemma2:2b", RequestTimeout: 2 * time.Second})
	got, err := c.Generate(context.Background(), "TRANSCRIPT body")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if got != "running pnpm test" {
		t.Fatalf("got %q", got)
	}
}

func TestClientGenerate500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := NewClient(Config{BaseURL: srv.URL, Model: "gemma2:2b", RequestTimeout: 2 * time.Second})
	_, err := c.Generate(context.Background(), "x")
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestClientTagsContains(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Fatalf("path: %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"models":[{"name":"gemma2:2b"},{"name":"phi3:mini"}]}`)
	}))
	defer srv.Close()
	c := NewClient(Config{BaseURL: srv.URL, RequestTimeout: 2 * time.Second})
	tags, err := c.Tags(context.Background())
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}
	got := map[string]bool{}
	for _, t := range tags {
		got[t] = true
	}
	if !got["gemma2:2b"] {
		t.Fatalf("missing gemma2:2b in tags: %v", tags)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/summarizer/ -v`
Expected: FAIL — `NewClient` / `Client.Generate` / `Client.Tags` undefined.

- [ ] **Step 4: Implement `client.go`**

```go
// internal/summarizer/client.go
package summarizer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client is a thin wrapper over the Ollama HTTP API. Holds a single keep-alive
// connection so we don't repeatedly handshake against the local daemon.
type Client struct {
	baseURL string
	model   string
	http    *http.Client
}

// NewClient builds a Client. Falls back to library defaults when fields are zero.
func NewClient(cfg Config) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://127.0.0.1:11434"
	}
	if cfg.RequestTimeout == 0 {
		cfg.RequestTimeout = 4 * time.Second
	}
	return &Client{
		baseURL: cfg.BaseURL,
		model:   cfg.Model,
		http: &http.Client{
			Timeout: cfg.RequestTimeout,
			Transport: &http.Transport{
				MaxIdleConnsPerHost: 1,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

type generateRequest struct {
	Model   string         `json:"model"`
	Prompt  string         `json:"prompt"`
	Stream  bool           `json:"stream"`
	Options map[string]any `json:"options,omitempty"`
}

type generateResponse struct {
	Response string `json:"response"`
}

// Generate calls /api/generate with the configured model and returns the
// response text trimmed of trailing whitespace. Does not retry; the worker owns
// retry policy.
func (c *Client) Generate(ctx context.Context, prompt string) (string, error) {
	body := generateRequest{
		Model:  c.model,
		Prompt: prompt,
		Stream: false,
		Options: map[string]any{
			"num_predict": 30,
			"temperature": 0.2,
		},
	}
	buf := new(bytes.Buffer)
	if err := json.NewEncoder(buf).Encode(body); err != nil {
		return "", fmt.Errorf("encode: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/generate", buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama status %d", resp.StatusCode)
	}
	var out generateResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}
	return out.Response, nil
}

type tagsResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

// Tags calls /api/tags and returns the list of locally-available model names.
// Used by the daemon's health probe to verify the configured model is pulled.
func (c *Client) Tags(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama status %d", resp.StatusCode)
	}
	var out tagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(out.Models))
	for _, m := range out.Models {
		names = append(names, m.Name)
	}
	return names, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/summarizer/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/summarizer/config.go internal/summarizer/client.go internal/summarizer/client_test.go
git commit -m "summarizer: Config + Ollama HTTP client with keep-alive"
```

---

## Task 5: summarizer prompt builder + response cleaner

**Files:**
- Create: `internal/summarizer/prompt.go`
- Test: `internal/summarizer/prompt_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/summarizer/prompt_test.go
package summarizer

import "testing"

func TestBuildPromptIncludesContext(t *testing.T) {
	p := buildPrompt("codex", "payment-migration", "running pnpm test:billing\n")
	for _, s := range []string{"codex", "payment-migration", "running pnpm test:billing"} {
		if !contains(p, s) {
			t.Fatalf("prompt missing %q. full:\n%s", s, p)
		}
	}
}

func TestCleanResponseStripsArtifacts(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"running pnpm test", "running pnpm test"},
		{`"running pnpm test"`, "running pnpm test"},
		{"Output: running pnpm test", "running pnpm test"},
		{"- running pnpm test", "running pnpm test"},
		{"running pnpm test.", "running pnpm test"},
		{"  running pnpm test  ", "running pnpm test"},
		{"Description: rewriting webhook handlers — 12 of 14", "rewriting webhook handlers — 12 of 14"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := cleanResponse(tc.in)
			if got != tc.want {
				t.Fatalf("cleanResponse(%q) = %q want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestCleanResponseClampsTo60(t *testing.T) {
	long := "this is a description that is definitely longer than sixty characters in total length"
	got := cleanResponse(long)
	if len([]rune(got)) > 60 {
		t.Fatalf("not clamped: len=%d %q", len([]rune(got)), got)
	}
}

// contains is a local helper (avoid importing strings just for tests).
func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && (indexOf(s, sub) >= 0))
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/summarizer/ -run TestBuildPrompt -v`
Expected: FAIL — `buildPrompt` / `cleanResponse` undefined.

- [ ] **Step 3: Implement `prompt.go`**

```go
// internal/summarizer/prompt.go
package summarizer

import (
	"fmt"
	"strings"
)

const maxDescription = 60

// buildPrompt assembles the few-shot prompt for the Ollama call.
func buildPrompt(tool, slug, transcript string) string {
	return fmt.Sprintf(`You are watching a CLI coding agent named %s working on a task slugged "%s".
Below is the recent terminal output from the agent (ANSI stripped).
In ONE line of at most 60 characters, describe what the agent is doing RIGHT NOW.
Use simple verbs. No quotes, no preface, no trailing period.

Examples:
- rewriting webhook handlers — 12 of 14
- running pnpm test:billing
- waiting on user: pick a theme

Transcript:
%s
`, tool, slug, transcript)
}

// cleanResponse normalizes whatever the model produced into the form that
// belongs in the desc column: at most 60 runes, no surrounding quotes, no
// "Output:" / "Description:" / "- " preface, no trailing period.
func cleanResponse(s string) string {
	s = strings.TrimSpace(s)
	// Strip common prefixes the model adds.
	for _, p := range []string{"Output:", "Description:", "Activity:", "Status:"} {
		if strings.HasPrefix(strings.ToLower(s), strings.ToLower(p)) {
			s = strings.TrimSpace(s[len(p):])
			break
		}
	}
	for strings.HasPrefix(s, "- ") || strings.HasPrefix(s, "> ") || strings.HasPrefix(s, "* ") {
		s = strings.TrimSpace(s[2:])
	}
	// Strip surrounding quotes.
	if len(s) >= 2 {
		switch s[0] {
		case '"', '\'', '`':
			if s[len(s)-1] == s[0] {
				s = s[1 : len(s)-1]
			}
		}
	}
	// Drop a trailing period that isn't part of an ellipsis.
	if strings.HasSuffix(s, ".") && !strings.HasSuffix(s, "..") {
		s = strings.TrimRight(s, ".")
	}
	s = strings.TrimSpace(s)
	// Clamp to 60 runes with ellipsis.
	if runes := []rune(s); len(runes) > maxDescription {
		s = string(runes[:maxDescription-1]) + "…"
	}
	return s
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/summarizer/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/summarizer/prompt.go internal/summarizer/prompt_test.go
git commit -m "summarizer: few-shot prompt builder + response cleaner"
```

---

## Task 6: summarizer.Worker — channel consumer with dedup, skip-if-unchanged, health flag

**Files:**
- Create: `internal/summarizer/worker.go`
- Test: `internal/summarizer/worker_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/summarizer/worker_test.go
package summarizer

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tristanbietsch/rex/internal/protocol"
	"github.com/tristanbietsch/rex/internal/state"
)

func newTestStoreWithSession(id string) (*state.Store, *state.Session) {
	st := state.NewStore()
	sess := &state.Session{
		ID:        id,
		ShortID:   id[:4],
		ToolID:    "codex",
		Slug:      "test-task",
		State:     protocol.StateWorking,
		StartedAt: time.Now().UTC(),
		LastLine:  "running pnpm test:billing",
	}
	_ = st.Add(sess)
	return st, sess
}

func TestWorkerCallsOllamaAndWritesDescription(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_, _ = io.WriteString(w, `{"response":"running pnpm test"}`)
	}))
	defer srv.Close()
	st, _ := newTestStoreWithSession("sess-aaaa")

	got := make(chan string, 1)
	st.Subscribe(func(e state.Event) {
		if v, ok := e.Patch["description"].(string); ok {
			select {
			case got <- v:
			default:
			}
		}
	})

	cfg := Defaults()
	cfg.BaseURL = srv.URL
	cfg.MinInterval = 0
	w := New(cfg, st, transcriptStub("recent transcript bytes"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Start(ctx) }()
	w.Channel() <- "sess-aaaa"

	select {
	case desc := <-got:
		if desc != "running pnpm test" {
			t.Fatalf("desc: %q", desc)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("no description delivered")
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
}

func TestWorkerSkipsIfUnchangedHash(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_, _ = io.WriteString(w, `{"response":"running tests"}`)
	}))
	defer srv.Close()
	st, _ := newTestStoreWithSession("sess-bbbb")

	cfg := Defaults()
	cfg.BaseURL = srv.URL
	cfg.MinInterval = 0
	w := New(cfg, st, transcriptStub("identical bytes"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Start(ctx) }()

	w.Channel() <- "sess-bbbb"
	time.Sleep(150 * time.Millisecond)
	w.Channel() <- "sess-bbbb" // second signal with identical transcript → skip
	time.Sleep(150 * time.Millisecond)

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 Ollama call (skip-if-unchanged), got %d", got)
	}
}

func TestWorkerSkipsTerminalState(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_, _ = io.WriteString(w, `{"response":"x"}`)
	}))
	defer srv.Close()
	st, sess := newTestStoreWithSession("sess-cccc")
	sess.State = protocol.StateDone

	cfg := Defaults()
	cfg.BaseURL = srv.URL
	cfg.MinInterval = 0
	w := New(cfg, st, transcriptStub("anything"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Start(ctx) }()

	w.Channel() <- "sess-cccc"
	time.Sleep(150 * time.Millisecond)
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("expected 0 calls (terminal state), got %d", got)
	}
}

func TestWorkerMinIntervalGate(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_, _ = io.WriteString(w, `{"response":"x"}`)
	}))
	defer srv.Close()
	st, _ := newTestStoreWithSession("sess-dddd")

	cfg := Defaults()
	cfg.BaseURL = srv.URL
	cfg.MinInterval = 500 * time.Millisecond
	// Vary transcript per call so skip-if-unchanged doesn't dominate.
	var i int32
	w := New(cfg, st, func(_ string, _ int) []byte {
		v := atomic.AddInt32(&i, 1)
		return []byte("bytes-" + string(rune('0'+v)))
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Start(ctx) }()

	w.Channel() <- "sess-dddd"
	time.Sleep(100 * time.Millisecond)
	w.Channel() <- "sess-dddd" // dropped — within MinInterval
	time.Sleep(100 * time.Millisecond)
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 call (min-interval gate), got %d", got)
	}
}

// transcriptStub is the TranscriptReader fixture used by the tests: returns the
// same bytes regardless of session/max.
func transcriptStub(s string) TranscriptReader {
	return func(_ string, _ int) []byte { return []byte(s) }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/summarizer/ -v`
Expected: FAIL — `Worker` / `New` / `TranscriptReader` / `transcriptStub` undefined.

- [ ] **Step 3: Implement `worker.go`**

```go
// internal/summarizer/worker.go
package summarizer

import (
	"context"
	"hash/fnv"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tristanbietsch/rex/internal/protocol"
	"github.com/tristanbietsch/rex/internal/state"
)

// TranscriptReader returns up to `max` bytes of sanitized transcript tail for
// the given session ID. Dependency-injected so unit tests don't touch disk.
type TranscriptReader func(sessionID string, max int) []byte

// Worker is a single-goroutine consumer of session-summary requests.
type Worker struct {
	cfg        Config
	store      *state.Store
	client     *Client
	transcript TranscriptReader

	ch chan string

	mu       sync.Mutex
	perSess  map[string]*sessionMeta
	failures int32

	// availability: 1 = healthy, 0 = backend unavailable.
	available atomic.Int32
	onHealth  func(available bool, reason string) // optional callback set by daemon
}

type sessionMeta struct {
	lastSubmittedAt time.Time
	lastHash        uint64
}

// New builds a Worker. transcript is the function the worker uses to read the
// sanitized transcript tail from disk (usually state.TranscriptTail).
func New(cfg Config, store *state.Store, transcript TranscriptReader) *Worker {
	if cfg.MaxBytes == 0 {
		cfg.MaxBytes = 2048
	}
	if cfg.MinInterval == 0 {
		cfg.MinInterval = 800 * time.Millisecond
	}
	w := &Worker{
		cfg:        cfg,
		store:      store,
		client:     NewClient(cfg),
		transcript: transcript,
		ch:         make(chan string, 64),
		perSess:    make(map[string]*sessionMeta),
	}
	w.available.Store(1)
	return w
}

// Channel returns the send-side of the request channel. PTY supervisors emit
// session IDs here.
func (w *Worker) Channel() chan<- string { return w.ch }

// SetHealthCallback installs a callback invoked whenever the backend availability
// flips. Called from the worker goroutine.
func (w *Worker) SetHealthCallback(fn func(available bool, reason string)) {
	w.onHealth = fn
}

// BackendAvailable reports whether the worker currently believes Ollama is reachable.
func (w *Worker) BackendAvailable() bool { return w.available.Load() == 1 }

// MarkUnavailable flips the flag to false (used by the daemon's health probe
// before the worker has tried a single call).
func (w *Worker) MarkUnavailable(reason string) {
	if w.available.Swap(0) == 1 {
		slog.Warn("summarizer: backend_unavailable", "reason", reason)
		if w.onHealth != nil {
			w.onHealth(false, reason)
		}
	}
}

// MarkAvailable flips the flag to true (used by the daemon's health probe).
func (w *Worker) MarkAvailable() {
	if w.available.Swap(1) == 0 {
		slog.Info("summarizer: backend_restored")
		if w.onHealth != nil {
			w.onHealth(true, "")
		}
	}
}

// Start launches the worker goroutine. Returns when ctx is canceled.
func (w *Worker) Start(ctx context.Context) error {
	slog.Info("summarizer: started", "model", w.cfg.Model, "base_url", w.cfg.BaseURL, "min_interval_ms", w.cfg.MinInterval.Milliseconds())
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case id := <-w.ch:
			w.handle(ctx, id)
		}
	}
}

func (w *Worker) handle(ctx context.Context, id string) {
	if w.available.Load() == 0 {
		return
	}
	sess, ok := w.store.Get(id)
	if !ok {
		return
	}
	// Skip terminal states.
	st := sess.State
	if st == protocol.StateDone || st == protocol.StateFailed || st == protocol.StateCrashed {
		return
	}

	w.mu.Lock()
	meta := w.perSess[id]
	if meta == nil {
		meta = &sessionMeta{}
		w.perSess[id] = meta
	}
	if time.Since(meta.lastSubmittedAt) < w.cfg.MinInterval {
		w.mu.Unlock()
		return
	}
	w.mu.Unlock()

	tail := w.transcript(id, w.cfg.MaxBytes)
	prompt := buildPrompt(sess.ToolID, sess.Slug, string(tail))

	h := fnv.New64a()
	_, _ = h.Write([]byte(prompt))
	hash := h.Sum64()

	w.mu.Lock()
	if hash == meta.lastHash && meta.lastHash != 0 {
		w.mu.Unlock()
		slog.Debug("summarizer: skipped_unchanged", "session", id)
		return
	}
	meta.lastSubmittedAt = time.Now()
	w.mu.Unlock()

	bytesIn := len(tail)
	slog.Debug("summarizer: request", "session", id, "bytes_in", bytesIn)
	start := time.Now()

	callCtx, cancel := context.WithTimeout(ctx, w.cfg.RequestTimeout+500*time.Millisecond)
	defer cancel()

	resp, err := w.callWithRetry(callCtx, prompt)
	elapsed := time.Since(start)
	if err != nil {
		atomic.AddInt32(&w.failures, 1)
		slog.Warn("summarizer: error", "session", id, "err", err)
		if atomic.LoadInt32(&w.failures) >= 3 {
			w.MarkUnavailable("consecutive call failures")
		}
		return
	}
	atomic.StoreInt32(&w.failures, 0)
	if elapsed > 2*time.Second {
		slog.Info("summarizer: slow_call", "session", id, "duration_ms", elapsed.Milliseconds())
	}

	cleaned := cleanResponse(resp)
	slog.Debug("summarizer: response", "session", id, "duration_ms", elapsed.Milliseconds(), "chars_out", len([]rune(cleaned)))
	if cleaned == "" {
		return
	}

	w.mu.Lock()
	meta.lastHash = hash
	w.mu.Unlock()

	if err := w.store.UpdateDescription(id, cleaned); err != nil {
		slog.Warn("summarizer: update_description failed", "session", id, "err", err)
	}
}

func (w *Worker) callWithRetry(ctx context.Context, prompt string) (string, error) {
	resp, err := w.client.Generate(ctx, prompt)
	if err == nil {
		return resp, nil
	}
	select {
	case <-ctx.Done():
		return "", err
	case <-time.After(500 * time.Millisecond):
	}
	return w.client.Generate(ctx, prompt)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/summarizer/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/summarizer/worker.go internal/summarizer/worker_test.go
git commit -m "summarizer: single-goroutine worker with dedup + skip-if-unchanged"
```

---

## Task 7: Plumb summary channel through server → supervisor

**Files:**
- Modify: `internal/server/server.go` (likely; find the file that defines `server.Config`)
- Modify: `internal/server/client.go` (where `pty.SupervisorConfig` is constructed)
- Test: existing `internal/server/*_test.go` builds must still pass

- [ ] **Step 1: Locate `server.Config`**

Run: `grep -n "type Config" internal/server/*.go`
The struct lives in `server.go`. Read it to confirm the surrounding fields.

- [ ] **Step 2: Add the new field to `server.Config`**

In the `Config` struct, after `MaxConcurrentSessions`, add:

```go
// SummaryRequest is the channel into the summarizer worker; nil disables AI
// description generation for this server.
SummaryRequest chan<- string
```

- [ ] **Step 3: Forward it into `pty.SupervisorConfig`**

In `internal/server/client.go` around line 258 where the `pty.New(pty.SupervisorConfig{...})` literal lives, add:

```go
sup := pty.New(pty.SupervisorConfig{
    StateDir:       cfg.StateDir,
    Store:          cfg.Store,
    Command:        cmdArgs,
    CWD:            p.CWD,
    Adapter:        ad,
    InputCh:        inputCh,
    InitialPrompt:  p.InitialPrompt,
    SummaryRequest: cfg.SummaryRequest,
    OutputSink: func(b []byte) {
        srv.broadcastSessionOutput(sess.ID, b)
    },
    RegisterResize: ...,
    UnregisterResize: ...,
})
```

- [ ] **Step 4: Build & test**

Run: `go build ./... && go test ./internal/server/...`
Expected: builds and tests still pass (existing server tests pass nil for the channel via zero value).

- [ ] **Step 5: Commit**

```bash
git add internal/server/server.go internal/server/client.go
git commit -m "server: thread SummaryRequest channel into supervisor config"
```

---

## Task 8: Settings — new section + three keys

**Files:**
- Modify: `internal/settings/types.go`
- Modify: `internal/settings/registry.go`

- [ ] **Step 1: Add `SectionSummary` section**

Edit `internal/settings/types.go`. In the `Section` const block, add after `SectionOnboarding`:

```go
SectionOnboarding Section = "Onboarding"
SectionSummary    Section = "AI summary"
SectionAdvanced   Section = "Advanced"
```

- [ ] **Step 2: Add the three settings**

Edit `internal/settings/registry.go`. Insert a new block before `// Advanced`:

```go
    // AI summary
    {
        ID: "summary_enabled", Label: "Summary enabled", Section: SectionSummary,
        Type: TypeBool, Default: true,
        Help: "Generate AI activity descriptions via local Ollama. When off, the desc column shows the raw last line.",
    },
    {
        ID: "summary_model", Label: "Summary model", Section: SectionSummary,
        Type: TypeString, Default: "gemma2:2b",
        Options: []string{"gemma2:2b", "llama3.2:1b", "phi3:mini", "qwen2.5:1.5b"},
        Help:    "Ollama model used to summarize sessions. Cycles through known small models; any pulled model works via config.yaml.",
    },
    {
        ID: "desc_animation", Label: "Description animation", Section: SectionSummary,
        Type: TypeEnum, Default: "typewriter",
        Options: []string{"typewriter", "decode", "wipe", "off"},
        Help:    "Animation effect when the AI description changes. Reduce motion overrides this to off.",
    },
```

- [ ] **Step 3: Build & test**

Run: `go test ./internal/settings/... && go build ./...`
Expected: PASS / builds clean.

- [ ] **Step 4: Commit**

```bash
git add internal/settings/types.go internal/settings/registry.go
git commit -m "settings: AI summary section with enabled/model/animation keys"
```

---

## Task 9: SummarizerHealth event type + protocol

**Files:**
- Modify: `internal/protocol/events.go`

- [ ] **Step 1: Add the event constant and payload**

Edit `internal/protocol/events.go`. In the event-types const block:

```go
EventSessionOutput  = "SessionOutput"
EventSummarizerHealth = "SummarizerHealth"
EventError          = "Error"
```

After the other payload structs:

```go
// SummarizerHealth signals AI summary backend availability changes.
type SummarizerHealth struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}
```

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: builds clean.

- [ ] **Step 3: Commit**

```bash
git add internal/protocol/events.go
git commit -m "protocol: SummarizerHealth event for backend availability"
```

---

## Task 10: Wire summarizer into rex-daemon + emit health events through server

**Files:**
- Modify: `cmd/rex-daemon/main.go`
- Modify: `internal/server/server.go` (or wherever the broker lives) — add an exported method to broadcast `SummarizerHealth` envelopes to all subscribers
- Modify: `internal/server/client.go` (or broker file) — hook into the subscription path to emit `SummarizerHealth` envelopes to connected TUI clients
- Test: existing server tests must still pass

This task wires the worker, runs a health probe, and pipes health-flag changes to TUI clients. No new unit tests — covered by integration testing in Task 11.

- [ ] **Step 1: Add a `BroadcastSummarizerHealth` method on the server**

Find the file that owns the subscriber list / broker (probably `internal/server/server.go` or a `broker.go`). Look for the existing `broadcastSessionOutput` method as a pattern. Add an analogous method:

```go
// BroadcastSummarizerHealth fans out a SummarizerHealth event to every connected
// client. Called by main.go when the summarizer health flag flips.
func (s *Server) BroadcastSummarizerHealth(available bool, reason string) {
    s.broadcastEnvelope(protocol.EventSummarizerHealth, "", protocol.SummarizerHealth{
        Available: available, Reason: reason,
    })
}
```

(`broadcastEnvelope` is the existing helper used by `broadcastSessionOutput`; if it has a different name in the file, use that. The point is: route through the same fan-out machinery used for output events.)

- [ ] **Step 2: Build the worker in `cmd/rex-daemon/main.go`**

Edit `cmd/rex-daemon/main.go`. After the existing `store := state.NewStore()` block and before the `srv, err := server.New(...)` call, add:

```go
// Load settings for the daemon — same config the TUI writes to.
settingsStore := settings.NewStore()
_ = settingsStore.Load(settings.DefaultPath()) // missing file is fine, defaults apply

summaryEnabled, _ := settingsStore.Get("summary_enabled").(bool)
summaryModel, _ := settingsStore.Get("summary_model").(string)

var summaryCh chan string
var summaryWorker *summarizer.Worker
if summaryEnabled {
    cfg := summarizer.Defaults()
    cfg.Model = summaryModel
    if env := os.Getenv("OLLAMA_HOST"); env != "" {
        // Allow override; Ollama's own env var convention.
        if !strings.HasPrefix(env, "http") {
            env = "http://" + env
        }
        cfg.BaseURL = env
    }
    summaryWorker = summarizer.New(cfg, store, func(id string, max int) []byte {
        b, _ := state.TranscriptTail(*stateDir, id, max)
        return b
    })
    summaryCh = make(chan string, 64)
    // Pump from the bounded channel into the worker so non-blocking sends from
    // the PTY supervisor don't drop signals as long as the worker can drain.
    go func() {
        for id := range summaryCh {
            summaryWorker.Channel() <- id
        }
    }()
}
```

Add the imports:
```go
"strings"
"github.com/tristanbietsch/rex/internal/settings"
"github.com/tristanbietsch/rex/internal/summarizer"
```

- [ ] **Step 3: Pass the channel into `server.Config`**

In the existing `server.New(server.Config{...})` literal:

```go
srv, err := server.New(server.Config{
    Socket:                *socketPath,
    StateDir:              *stateDir,
    Registry:              reg,
    Store:                 store,
    MaxConcurrentSessions: *maxConcurrent,
    SummaryRequest:        summaryCh,
})
```

- [ ] **Step 4: Run the health probe + start the worker**

After `srv, err := server.New(...)` is built and `ctx, cancel := signal.NotifyContext(...)` has been set up, add:

```go
if summaryWorker != nil {
    summaryWorker.SetHealthCallback(func(available bool, reason string) {
        srv.BroadcastSummarizerHealth(available, reason)
    })
    go func() {
        if err := summaryWorker.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
            slog.Warn("summarizer: worker exited", "err", err)
        }
    }()
    go probeOllamaHealth(ctx, summaryWorker, summaryModel)
}
```

Add the `errors` import if not present.

Then add `probeOllamaHealth` at the bottom of the file:

```go
// probeOllamaHealth runs the initial Ollama reachability + model-presence check,
// then loops every 30s while the backend is marked unavailable, recovering when
// both checks pass.
func probeOllamaHealth(ctx context.Context, w *summarizer.Worker, model string) {
    cfg := summarizer.Defaults()
    cfg.Model = model
    client := summarizer.NewClient(cfg)
    check := func() {
        tCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
        defer cancel()
        tags, err := client.Tags(tCtx)
        if err != nil {
            w.MarkUnavailable("ollama unreachable")
            return
        }
        for _, t := range tags {
            if t == model {
                w.MarkAvailable()
                return
            }
        }
        w.MarkUnavailable("model not pulled: " + model)
    }
    check()
    t := time.NewTicker(30 * time.Second)
    defer t.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-t.C:
            if !w.BackendAvailable() {
                check()
            }
        }
    }
}
```

Add the `time` import if not already there (it isn't — add it).

- [ ] **Step 5: Build everything**

Run: `go build ./...`
Expected: builds clean.

- [ ] **Step 6: Run existing daemon-related tests**

Run: `go test ./internal/server/... ./cmd/rex-daemon/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add cmd/rex-daemon/main.go internal/server/server.go internal/server/client.go
git commit -m "daemon: launch summarizer worker + health probe; broadcast availability"
```

---

## Task 11: Integration test — daemon end-to-end summary delivery

**Files:**
- Create: `cmd/rex-daemon/summarizer_integration_test.go`

This drives the whole pipeline against a fake Ollama HTTP server: emit a PTY-like signal, watch a description land on the session.

- [ ] **Step 1: Write the test**

The store + worker can be exercised directly without a real UDS server. The integration boundary that matters for this test is "PTY-style signal → Ollama call → store update."

```go
// cmd/rex-daemon/summarizer_integration_test.go
package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tristanbietsch/rex/internal/protocol"
	"github.com/tristanbietsch/rex/internal/state"
	"github.com/tristanbietsch/rex/internal/summarizer"
)

func TestEndToEndSummaryDelivery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			_, _ = io.WriteString(w, `{"models":[{"name":"gemma2:2b"}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"response":"running pnpm test:billing"}`)
	}))
	defer srv.Close()

	st := state.NewStore()
	sess := &state.Session{
		ID:        "sess-eeee",
		ShortID:   "eeee",
		ToolID:    "codex",
		Slug:      "test-task",
		State:     protocol.StateWorking,
		StartedAt: time.Now().UTC(),
	}
	if err := st.Add(sess); err != nil {
		t.Fatalf("Add: %v", err)
	}

	cfg := summarizer.Defaults()
	cfg.BaseURL = srv.URL
	cfg.MinInterval = 0
	w := summarizer.New(cfg, st, func(_ string, _ int) []byte {
		return []byte("recent terminal output here")
	})

	got := make(chan string, 1)
	st.Subscribe(func(e state.Event) {
		if v, ok := e.Patch["description"].(string); ok {
			select {
			case got <- v:
			default:
			}
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Start(ctx) }()

	w.Channel() <- "sess-eeee"
	select {
	case desc := <-got:
		if desc != "running pnpm test:billing" {
			t.Fatalf("desc: %q", desc)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("no description delivered within 2s")
	}
}
```

- [ ] **Step 2: Run the test**

Run: `go test ./cmd/rex-daemon/ -run TestEndToEndSummaryDelivery -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add cmd/rex-daemon/summarizer_integration_test.go
git commit -m "daemon: end-to-end summary delivery integration test"
```

---

## Task 12: TUI animation core (anim.go)

**Files:**
- Create: `internal/tui/anim.go`
- Test: `internal/tui/anim_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/tui/anim_test.go
package tui

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestRenderAnimFrameTypewriter(t *testing.T) {
	a := DescAnim{
		From: "old description text",
		To:   "running pnpm test",
		Effect: "typewriter",
		StartedAt: time.Now(),
		Duration: 300 * time.Millisecond,
	}
	// At p=0 → empty + padding to width.
	got := renderAnimFrame(a, 20, a.StartedAt)
	if utf8.RuneCountInString(got) != 20 {
		t.Fatalf("p=0 not width=20: %q (%d runes)", got, utf8.RuneCountInString(got))
	}
	if strings.TrimSpace(got) != "" {
		t.Fatalf("p=0 should be blank, got %q", got)
	}
	// At p=1 → full text + padding.
	got = renderAnimFrame(a, 20, a.StartedAt.Add(a.Duration))
	if utf8.RuneCountInString(got) != 20 {
		t.Fatalf("p=1 not width=20: %q", got)
	}
	if !strings.HasPrefix(got, "running pnpm test") {
		t.Fatalf("p=1 missing target prefix: %q", got)
	}
}

func TestRenderAnimFrameDecodeWidthInvariant(t *testing.T) {
	a := DescAnim{
		From: "",
		To:   "running tests",
		Effect: "decode",
		StartedAt: time.Now(),
		Duration: 400 * time.Millisecond,
	}
	for _, p := range []float64{0, 0.25, 0.5, 0.75, 1.0} {
		at := a.StartedAt.Add(time.Duration(float64(a.Duration) * p))
		got := renderAnimFrame(a, 30, at)
		if utf8.RuneCountInString(got) != 30 {
			t.Fatalf("decode p=%v width: got %d want 30 (%q)", p, utf8.RuneCountInString(got), got)
		}
	}
}

func TestRenderAnimFrameWipeShowsCursor(t *testing.T) {
	a := DescAnim{
		From: "old line",
		To:   "new line",
		Effect: "wipe",
		StartedAt: time.Now(),
		Duration: 250 * time.Millisecond,
	}
	mid := a.StartedAt.Add(125 * time.Millisecond)
	got := renderAnimFrame(a, 20, mid)
	if utf8.RuneCountInString(got) != 20 {
		t.Fatalf("wipe width: %q", got)
	}
	if !strings.ContainsRune(got, '█') {
		t.Fatalf("wipe midway should contain █: %q", got)
	}
}

func TestRenderAnimFrameDecodeNoiseAlphabet(t *testing.T) {
	a := DescAnim{
		From: "",
		To:   "abcdefghij",
		Effect: "decode",
		StartedAt: time.Now(),
		Duration: 400 * time.Millisecond,
	}
	// At p=0.01 essentially nothing has settled.
	got := renderAnimFrame(a, 10, a.StartedAt.Add(time.Millisecond))
	allowed := "!@#$%&*+=?<>~/\\abcdefghij "
	for _, r := range got {
		if !strings.ContainsRune(allowed, r) {
			t.Fatalf("decode unexpected glyph %q in %q", r, got)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestRenderAnimFrame -v`
Expected: FAIL — `DescAnim` / `renderAnimFrame` undefined.

- [ ] **Step 3: Implement `anim.go`**

```go
// internal/tui/anim.go
package tui

import (
	"strings"
	"time"
)

// DescAnim is the active animation for a single session's description cell.
type DescAnim struct {
	From      string
	To        string
	Effect    string // "typewriter" | "decode" | "wipe"
	StartedAt time.Time
	Duration  time.Duration
}

const noiseAlphabet = "!@#$%&*+=?<>~/\\"

// Active reports whether the animation should still render at `now`.
func (a DescAnim) Active(now time.Time) bool {
	return now.Before(a.StartedAt.Add(a.Duration))
}

// renderAnimFrame returns a width-padded rendering of the animation at `now`.
// Output is always exactly `width` runes wide.
func renderAnimFrame(a DescAnim, width int, now time.Time) string {
	if width <= 0 {
		return ""
	}
	p := float64(now.Sub(a.StartedAt)) / float64(a.Duration)
	if p < 0 {
		p = 0
	}
	if p > 1 {
		p = 1
	}
	to := padOrTruncateRunes(a.To, width)
	from := padOrTruncateRunes(a.From, width)
	switch a.Effect {
	case "decode":
		return decodeFrame(to, p, now)
	case "wipe":
		return wipeFrame(to, from, p, width)
	case "typewriter":
		fallthrough
	default:
		return typewriterFrame(to, p, width)
	}
}

func typewriterFrame(to []rune, p float64, width int) string {
	n := int(float64(len(to)) * p)
	if n > len(to) {
		n = len(to)
	}
	var b strings.Builder
	b.Grow(width)
	for i := 0; i < width; i++ {
		if i < n && i < len(to) {
			b.WriteRune(to[i])
		} else {
			b.WriteByte(' ')
		}
	}
	return b.String()
}

func decodeFrame(to []rune, p float64, now time.Time) string {
	noise := []rune(noiseAlphabet)
	var b strings.Builder
	for i, target := range to {
		settle := float64(i+1) / float64(len(to))
		if target == ' ' || p >= settle {
			b.WriteRune(target)
			continue
		}
		idx := int(now.UnixMilli()/40+int64(i)) % len(noise)
		if idx < 0 {
			idx += len(noise)
		}
		b.WriteRune(noise[idx])
	}
	return b.String()
}

func wipeFrame(to, from []rune, p float64, width int) string {
	cursor := int(float64(width) * p)
	if cursor > width {
		cursor = width
	}
	var b strings.Builder
	b.Grow(width)
	for i := 0; i < width; i++ {
		switch {
		case i < cursor:
			b.WriteRune(safeAt(to, i))
		case i == cursor && cursor < width:
			b.WriteRune('█')
		default:
			b.WriteRune(safeAt(from, i))
		}
	}
	return b.String()
}

func padOrTruncateRunes(s string, width int) []rune {
	rs := []rune(s)
	if len(rs) >= width {
		return rs[:width]
	}
	out := make([]rune, width)
	copy(out, rs)
	for i := len(rs); i < width; i++ {
		out[i] = ' '
	}
	return out
}

func safeAt(r []rune, i int) rune {
	if i < 0 || i >= len(r) {
		return ' '
	}
	return r[i]
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run TestRenderAnimFrame -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/anim.go internal/tui/anim_test.go
git commit -m "tui: animation core for description cell (typewriter/decode/wipe)"
```

---

## Task 13: Wire DescAnim into Model + descTickMsg + SessionUpdated handler

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/update.go`
- Modify: `internal/tui/events.go` if `SummarizerHealth` envelope handling lives there
- Test: extend `internal/tui/snapshot_test.go` or add a new test (existing snapshot harness may suffice)

- [ ] **Step 1: Add fields to `Model`**

Edit `internal/tui/model.go` — inside the `Model` struct, after `BlinkUntil`:

```go
// BlinkUntil tracks done-blink expiry per session.
BlinkUntil map[string]time.Time

// DescAnim is the active per-session description animation. Entries are
// pruned by the descTickMsg handler once the animation completes.
DescAnim map[string]DescAnim

// BackendUnavailable mirrors the daemon's summarizer health flag, delivered
// via SummarizerHealth events.
BackendUnavailable       bool
BackendUnavailableReason string
```

- [ ] **Step 2: Add the `descTickMsg` type and helper**

In `internal/tui/update.go` (top of file or near other msg types), add:

```go
// descTickMsg drives description-animation rendering at ~30 FPS while any row
// is mid-animation. It's queued by the SessionUpdated handler when an
// animation is registered, and re-queues itself until the DescAnim map empties.
type descTickMsg struct{}

const descTickInterval = 33 * time.Millisecond

func scheduleDescTick() tea.Cmd {
    return tea.Tick(descTickInterval, func(time.Time) tea.Msg { return descTickMsg{} })
}
```

(`tea` is already imported in `update.go`.)

- [ ] **Step 3: Register animation on SessionUpdated**

Edit `internal/tui/model.go:applyEvent` — find the `case protocol.EventSessionUpdated:` branch. After `applyPatch(&m.Sessions[i], upd.Patch)`, insert animation registration. This requires looking up the previous description before patch and the new description after — easier to capture inline:

Replace the inner block of the `for i := range m.Sessions` loop:

```go
for i := range m.Sessions {
    if m.Sessions[i].ID == upd.SessionID {
        prevDesc := m.Sessions[i].Description
        applyPatch(&m.Sessions[i], upd.Patch)
        if m.Sessions[i].Description != prevDesc {
            m = registerDescAnim(m, m.Sessions[i].ID, prevDesc, m.Sessions[i].Description)
        }
        break
    }
}
```

`applyEvent` returns `Model`, not `(Model, tea.Cmd)`, so it can't itself schedule the tick. The cmd is scheduled in `update.go` after `applyEvent` returns — add the check there.

Add `registerDescAnim` to `model.go` (or move into `update.go` — either is fine; keep with the Model since it mutates only the DescAnim map):

```go
// registerDescAnim records a description-change animation. Respects
// reduce_motion and the desc_animation setting.
func registerDescAnim(m Model, id, from, to string) Model {
    if to == "" {
        return m
    }
    effect, _ := m.Store.Get("desc_animation").(string)
    if rm, _ := m.Store.Get("reduce_motion").(bool); rm {
        effect = "off"
    }
    if effect == "" || effect == "off" {
        return m
    }
    if m.DescAnim == nil {
        m.DescAnim = map[string]DescAnim{}
    }
    if _, replacing := m.DescAnim[id]; replacing {
        slog.Info("desc_anim: replaced_in_flight", "session", id)
    }
    m.DescAnim[id] = DescAnim{
        From: from, To: to, Effect: effect,
        StartedAt: time.Now(),
        Duration:  animDurationFor(effect),
    }
    return m
}

func animDurationFor(effect string) time.Duration {
    switch effect {
    case "decode":
        return 400 * time.Millisecond
    case "wipe":
        return 250 * time.Millisecond
    default:
        return 300 * time.Millisecond
    }
}
```

- [ ] **Step 4: Schedule the tick from `Update`**

Find the `Update` function in `internal/tui/update.go`. After the event-dispatch (or wherever `EventSessionUpdated` arrives — likely in a `case protocol.Envelope:` branch that calls `applyEvent`), schedule a tick when any DescAnim entries are active:

```go
case protocol.Envelope:
    m = m.applyEvent(msg)
    var cmds []tea.Cmd
    if len(m.DescAnim) > 0 && !m.descTickPending {
        m.descTickPending = true
        cmds = append(cmds, scheduleDescTick())
    }
    // ... existing logic continues to append cmds ...
    return m, tea.Batch(cmds...)
```

Add `descTickPending bool` to the `Model` struct (right after `DescAnim`):

```go
DescAnim         map[string]DescAnim
descTickPending  bool
```

- [ ] **Step 5: Handle the tick**

In `Update`, add a new case:

```go
case descTickMsg:
    now := time.Now()
    for id, a := range m.DescAnim {
        if !a.Active(now) {
            delete(m.DescAnim, id)
        }
    }
    m.descTickPending = false
    if len(m.DescAnim) > 0 {
        m.descTickPending = true
        return m, scheduleDescTick()
    }
    return m, nil
```

- [ ] **Step 6: Add slog import to model.go if not already present**

Edit `internal/tui/model.go` imports — add `"log/slog"`.

- [ ] **Step 7: Build & test**

Run: `go test ./internal/tui/... && go build ./...`
Expected: PASS / builds clean.

- [ ] **Step 8: Commit**

```bash
git add internal/tui/model.go internal/tui/update.go
git commit -m "tui: DescAnim wiring + on-demand 33ms tick"
```

---

## Task 14: TUI renderer — read Description, apply animation frame

**Files:**
- Modify: `internal/tui/board.go`
- Test: `internal/tui/snapshot_test.go` (extend)

- [ ] **Step 1: Write a snapshot test for an animating row**

Read the existing snapshot test pattern first:
Run: `head -40 /Users/tristan/Documents/personal/dev/rex/internal/tui/snapshot_test.go`

Add a new test that builds a Model with one working session whose `Description` is non-empty and `DescAnim[id]` is set mid-typewriter, then asserts the rendered row contains the partial text.

```go
func TestBoardRendersDescriptionAnimating(t *testing.T) {
	m := Model{
		Sessions: []protocol.SessionSummary{{
			ID: "sess-x", ShortID: "abcd", Slug: "test", State: protocol.StateWorking,
			Description: "running pnpm test", LastLine: "raw garbage line",
		}},
		Store: settings.NewStore(),
		DescAnim: map[string]DescAnim{
			"sess-x": {
				From: "", To: "running pnpm test", Effect: "typewriter",
				StartedAt: time.Now().Add(-150 * time.Millisecond),
				Duration:  300 * time.Millisecond,
			},
		},
	}
	out := renderBoard(m, 120, 10)
	// Halfway through typewriter on a 17-rune target → expect ~8 runes visible.
	if !strings.Contains(out, "running p") && !strings.Contains(out, "running pn") {
		t.Fatalf("expected partial typewriter output, got:\n%s", out)
	}
	// Raw LastLine must NOT appear — the renderer should prefer Description.
	if strings.Contains(out, "raw garbage line") {
		t.Fatalf("renderer fell back to LastLine when Description is set")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestBoardRendersDescriptionAnimating -v`
Expected: FAIL — renderer still uses `LastLine`.

- [ ] **Step 3: Update `renderRow` in `board.go`**

Edit `internal/tui/board.go`. Replace the `desc :=` line (around line 148):

```go
descSource := s.Description
if descSource == "" {
    descSource = s.LastLine // bootstrap fallback
}
if anim, ok := m.DescAnim[s.ID]; ok && anim.Active(time.Now()) {
    descSource = renderAnimFrame(anim, descW, time.Now())
}
desc := lipgloss.NewStyle().Foreground(descColor).Width(descW).Render(truncate(descSource, descW))
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/board.go internal/tui/snapshot_test.go
git commit -m "tui: render Description in desc cell with animation when active"
```

---

## Task 15: TUI banner — render when BackendUnavailable

**Files:**
- Modify: `internal/tui/header.go`
- Modify: `internal/tui/model.go:applyEvent` (to consume `SummarizerHealth` envelopes)
- Test: extend `internal/tui/snapshot_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestHeaderBannerWhenBackendUnavailable(t *testing.T) {
	m := Model{
		Store: settings.NewStore(),
		BackendUnavailable: true,
		BackendUnavailableReason: "ollama unreachable",
	}
	out := renderHeader(m, 120)
	if !strings.Contains(out, "ollama unreachable") {
		t.Fatalf("banner missing reason: %q", out)
	}
}
```

(Look up the correct header-render entry-point name; if it's not `renderHeader`, use the one in `header.go`. Open `internal/tui/header.go:1-80` to confirm before writing the test.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestHeaderBanner -v`
Expected: FAIL — banner not rendered.

- [ ] **Step 3: Add the banner to header rendering**

Edit `internal/tui/header.go`. After the existing chips row, before returning, append a conditional banner line:

```go
if m.BackendUnavailable {
    reason := m.BackendUnavailableReason
    if reason == "" {
        reason = "ollama unreachable"
    }
    banner := lipgloss.NewStyle().Foreground(colorFgDim).Render(
        "summary backend: " + reason + " — install: https://ollama.com  ·  pull: ollama pull gemma2:2b",
    )
    out = out + "\n" + banner
}
```

(`out` is the existing accumulator; if the function builds with `strings.Builder` or returns inline, adapt the concatenation pattern but preserve indentation conventions in the file.)

- [ ] **Step 4: Consume `SummarizerHealth` envelopes**

Edit `internal/tui/model.go:applyEvent`. Add a new case before the closing brace of the switch:

```go
case protocol.EventSummarizerHealth:
    var h protocol.SummarizerHealth
    if err := json.Unmarshal(env.Data, &h); err == nil {
        m.BackendUnavailable = !h.Available
        m.BackendUnavailableReason = h.Reason
    }
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/tui/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/header.go internal/tui/model.go internal/tui/snapshot_test.go
git commit -m "tui: banner when AI summary backend is unavailable"
```

---

## Task 16: Manual smoke test + final pass

**Files:** none (verification only)

- [ ] **Step 1: Confirm full build**

Run: `go build ./...`
Expected: builds clean.

- [ ] **Step 2: Run all tests**

Run: `go test ./...`
Expected: all PASS.

- [ ] **Step 3: Start Ollama and pull the model**

```bash
ollama pull gemma2:2b
ollama serve  # in another shell if not already running
```

- [ ] **Step 4: Run rex against a real codex session**

```bash
make build  # or ./rex-daemon &
./rex
# Then `n` to open the wizard, pick codex/claude/whatever, give it a real task.
```

Verify:
- Desc column shows AI-generated text within 1–2s of session start.
- Text is calm — no flicker, no garbled fragments.
- When the agent transitions to a new phase, the cell animates (typewriter by default).
- `S` (settings) → "Description animation" cycles through typewriter/decode/wipe/off; each previews correctly on the next description change.
- `reduce_motion` on → animations snap to final text, no transitions.

- [ ] **Step 5: Smoke-test the unavailable path**

```bash
# Stop Ollama:
pkill ollama
```

Verify within 30s:
- A dim banner appears in the rex TUI: `summary backend: ollama unreachable — …`.
- Desc column reverts to the raw `LastLine` for new bytes.
- No crashes; daemon log shows `summarizer: backend_unavailable`.

Restart Ollama (`ollama serve`) and confirm the banner clears and descriptions resume within 30s.

- [ ] **Step 6: Verify logs**

```bash
tail -n 50 ~/.local/state/rex/daemon.log | grep summarizer
tail -n 50 ~/.local/state/rex/tui.log | grep desc_anim
```

Expected:
- `summarizer: started` once at daemon start.
- `summarizer: request` / `response` debug lines per call.
- `summarizer: skipped_unchanged` lines when the transcript hasn't moved.
- `desc_anim: start` / `completed` per animation cycle.

- [ ] **Step 7: No commit needed — verification only.**

---

## Out of scope (per spec)

- Replacing `LastLine` semantics. It remains the source for fail-popup, JSON output, bootstrap fallback.
- Cloud LLM providers.
- Per-tool prompt overrides.
- Multi-line descriptions.
- Streaming summaries.
- Auto-pulling the Ollama model on first run.
