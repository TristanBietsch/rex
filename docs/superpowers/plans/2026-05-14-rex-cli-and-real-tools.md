# Plan B — Rex CLI Surface + Real Tools + Streaming Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. **Important commit hygiene: NEVER add `Co-Authored-By: Claude ...` trailers to commit messages.**

**Goal:** Ship the `rex` CLI binary — a peer of `rex-daemon` — exposing the full Unix-style surface in `docs/cli.md`. Add the `ClaudeStructured` adapter, populate the built-in registry with the real tool set (claude, codex, gemini, ollama, plus opt-in grok/deepseek/kimi), wire `SessionOutput` streaming for live transcripts, and close out the three Plan A follow-ups.

**Architecture:** A second binary `rex` in `cmd/rex/` that talks to `rex-daemon` over the same UDS. A small `internal/client` library wraps the protocol. Each CLI verb lives in its own file under `internal/cli/`. `rex attach` uses raw terminal mode plus `Subscribe(session_id)` for byte streaming.

**Tech Stack:** Go 1.22+, all packages from Plan A, plus `golang.org/x/term` for raw terminal mode and SIGWINCH.

**Out of scope for Plan B (deferred to Plan C):**
- TUI (full Bubble Tea interface)
- Lua / audio / animations / settings page / slash palette interactive UI

---

## File structure

```
rex/
├── cmd/
│   ├── rex/
│   │   └── main.go              — CLI entry point + verb dispatcher
│   └── rex-daemon/
│       └── main.go              — (existing; minor edits to wire semaphore)
├── internal/
│   ├── adapter/
│   │   ├── adapter.go           — (existing; update For() to return ClaudeStructured)
│   │   ├── claude.go            — NEW: ClaudeStructured adapter
│   │   ├── claude_test.go       — NEW
│   │   ├── heuristic.go         — (existing; harden regex compilation)
│   │   └── heuristic_test.go    — (existing; +1 regression test)
│   ├── client/
│   │   ├── client.go            — NEW: Daemon client over UDS
│   │   └── client_test.go       — NEW
│   ├── cli/
│   │   ├── selector.go          — NEW: id/slug/UUID resolution
│   │   ├── selector_test.go     — NEW
│   │   ├── output.go            — NEW: text and JSON output helpers
│   │   ├── output_test.go       — NEW
│   │   ├── status.go            — NEW: rex status
│   │   ├── ls.go                — NEW: rex ls
│   │   ├── new.go               — NEW: rex new
│   │   ├── attach.go            — NEW: rex attach (raw PTY)
│   │   ├── reply.go             — NEW: rex reply
│   │   ├── send.go              — NEW: rex send
│   │   ├── log.go               — NEW: rex log [-f]
│   │   ├── wait.go              — NEW: rex wait
│   │   ├── rm.go                — NEW: rex rm
│   │   ├── rename.go            — NEW: rex rename
│   │   ├── archive.go           — NEW: rex archive
│   │   ├── reload.go            — NEW: rex reload
│   │   ├── daemon.go            — NEW: rex daemon start|stop|status|restart|logs
│   │   ├── completion.go        — NEW: rex completion bash|zsh|fish
│   │   └── version.go           — NEW: rex --version
│   ├── pty/
│   │   └── supervisor.go        — (existing; reorder WriteMeta before Transition(done))
│   ├── registry/
│   │   ├── builtin.yaml         — (existing; replace echo-only with real tool set)
│   │   └── types.go             — (existing; add EnabledByDefault field)
│   └── server/
│       ├── client.go            — (existing; add Subscribe, SendInput, Reply, Rename, FocusFilter handlers)
│       └── ... (existing)
└── Makefile                     — (existing; add `build-all` target)
```

`internal/cli/` ends up with one file per verb so each subagent can own a small focused unit.

---

## Phase 1 — Plan A follow-ups (3 tasks)

These tighten Plan A's known issues so Plan B builds on a clean foundation.

### Task B0: Harden regex compilation in HeuristicCLI

**Files:**
- Modify: `internal/adapter/heuristic.go`
- Modify: `internal/adapter/adapter.go`
- Modify: `internal/adapter/heuristic_test.go`

The current `NewHeuristic` uses `regexp.MustCompile`, which panics on bad user-supplied regex. Make it return an error and surface it through `For()`.

- [ ] **Step 1: Verify branch**

```bash
cd /Users/tristan/Documents/personal/dev/rex
git checkout master
git pull --ff-only
git checkout -b plan-b-cli
```

- [ ] **Step 2: Update `internal/adapter/heuristic.go`**

Replace the file with:

```go
package adapter

import (
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/tristanbietsch/rex/internal/protocol"
)

// ErrUnknownDetect signals an unsupported detect.kind in the registry.
var ErrUnknownDetect = errors.New("unknown detect kind")

// HeuristicCLI is a regex+idle adapter for CLIs without structured output.
type HeuristicCLI struct {
	prompt *regexp.Regexp
	idle   time.Duration
}

// NewHeuristic builds a HeuristicCLI. Returns an error if the regex is invalid.
func NewHeuristic(promptRegex string, idle time.Duration) (*HeuristicCLI, error) {
	re, err := regexp.Compile("(?m)" + promptRegex)
	if err != nil {
		return nil, fmt.Errorf("compile prompt regex %q: %w", promptRegex, err)
	}
	return &HeuristicCLI{prompt: re, idle: idle}, nil
}

// Detect implements Adapter.
func (h *HeuristicCLI) Detect(window []byte, idle time.Duration) protocol.State {
	if idle < h.idle {
		return protocol.StateWorking
	}
	tail := window
	if len(tail) > 4096 {
		tail = tail[len(tail)-4096:]
	}
	if h.prompt.Match(tail) {
		return protocol.StateNeedsInput
	}
	return protocol.StateWorking
}
```

(`ErrStructuredUnsupported` is removed — `adapter.For()` now returns `ClaudeStructured` for structured tools.)

- [ ] **Step 3: Update `internal/adapter/adapter.go`**

Replace the file with:

```go
// Package adapter classifies session state from PTY output.
package adapter

import (
	"fmt"
	"time"

	"github.com/tristanbietsch/rex/internal/protocol"
	"github.com/tristanbietsch/rex/internal/registry"
)

// Adapter classifies output chunks into states.
type Adapter interface {
	Detect(window []byte, idle time.Duration) protocol.State
}

// For builds an adapter for a tool's detection config.
func For(t registry.Tool) (Adapter, error) {
	switch t.Detect.Kind {
	case "heuristic":
		return NewHeuristic(t.Detect.PromptRegex, time.Duration(t.Detect.IdleMs)*time.Millisecond)
	case "structured":
		switch t.Detect.Format {
		case "claude_jsonl":
			return NewClaudeStructured(), nil
		default:
			return nil, fmt.Errorf("unsupported structured format %q", t.Detect.Format)
		}
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownDetect, t.Detect.Kind)
	}
}
```

- [ ] **Step 4: Append regression test to `internal/adapter/heuristic_test.go`**

Add this test function to the existing file:

```go
func TestNewHeuristic_RejectsBadRegex(t *testing.T) {
	_, err := NewHeuristic("[unclosed", 100*time.Millisecond)
	require.Error(t, err)
	require.Contains(t, err.Error(), "compile prompt regex")
}
```

And update the other test calls in the file. Search/replace:
- `NewHeuristic("^awaiting input:", 100*time.Millisecond)` → `h, err := NewHeuristic("^awaiting input:", 100*time.Millisecond); require.NoError(t, err)` and use `h.Detect(...)` thereafter.

Concretely, refactor each of the three existing tests:

```go
func TestHeuristic_NeedsInputWhenIdleAndPromptMatches(t *testing.T) {
	h, err := NewHeuristic("^awaiting input:", 100*time.Millisecond)
	require.NoError(t, err)
	got := h.Detect([]byte("hello\nawaiting input:"), 200*time.Millisecond)
	require.Equal(t, protocol.StateNeedsInput, got)
}

func TestHeuristic_WorkingWhenNotIdle(t *testing.T) {
	h, err := NewHeuristic("^awaiting input:", 100*time.Millisecond)
	require.NoError(t, err)
	got := h.Detect([]byte("doing things..."), 10*time.Millisecond)
	require.Equal(t, protocol.StateWorking, got)
}

func TestHeuristic_WorkingWhenIdleButNoPromptMatch(t *testing.T) {
	h, err := NewHeuristic("^awaiting input:", 100*time.Millisecond)
	require.NoError(t, err)
	got := h.Detect([]byte("doing things"), 5*time.Second)
	require.Equal(t, protocol.StateWorking, got)
}
```

- [ ] **Step 5: Build + test**

```bash
go build ./...
go test ./... -count=1
```

Expected: all packages PASS, including the new `TestNewHeuristic_RejectsBadRegex`.

- [ ] **Step 6: Commit**

```bash
git add internal/adapter/
git commit -m "adapter: return error from NewHeuristic on bad regex; route structured to ClaudeStructured stub"
```

---

### Task B1: Reorder WriteMeta before Transition(done) in supervisor

**Files:**
- Modify: `internal/pty/supervisor.go`
- Modify: `internal/server/e2e_test.go` (drop the polling workaround)

- [ ] **Step 1: Read current supervisor.go to find the exit-handling block**

```bash
cd /Users/tristan/Documents/personal/dev/rex
grep -n "Transition\|WriteMeta" internal/pty/supervisor.go
```

You should see something like:
```
... Transition(sess.ID, final) ...
... WriteMeta(s.cfg.StateDir, sess) ...
```

These are inside the `case rerr := <-errc:` block of the main select loop.

- [ ] **Step 2: Swap the order**

In `internal/pty/supervisor.go`, locate the case branch:

```go
case rerr := <-errc:
    // Child exited.
    waitErr := cmd.Wait()
    final := protocol.StateDone
    if waitErr != nil {
        if ee := new(exec.ExitError); errors.As(waitErr, &ee) {
            code := ee.ExitCode()
            sess.ExitCode = &code
            if code != 0 {
                final = protocol.StateFailed
            }
        } else if rerr != nil {
            final = protocol.StateFailed
        }
    }
    if err := s.cfg.Store.Transition(sess.ID, final); err != nil {
        return err
    }
    if err := state.WriteMeta(s.cfg.StateDir, sess); err != nil {
        return fmt.Errorf("write final meta: %w", err)
    }
    return nil
```

Move `state.WriteMeta` BEFORE `Store.Transition`. Also update the session's State on the in-memory struct before WriteMeta so the persisted meta reflects the terminal state:

```go
case rerr := <-errc:
    waitErr := cmd.Wait()
    final := protocol.StateDone
    if waitErr != nil {
        if ee := new(exec.ExitError); errors.As(waitErr, &ee) {
            code := ee.ExitCode()
            sess.ExitCode = &code
            if code != 0 {
                final = protocol.StateFailed
            }
        } else if rerr != nil {
            final = protocol.StateFailed
        }
    }
    // Update in-memory state first so the persisted meta reflects the terminal state.
    sess.State = final
    sess.LastEventAt = time.Now().UTC()
    if err := state.WriteMeta(s.cfg.StateDir, sess); err != nil {
        return fmt.Errorf("write final meta: %w", err)
    }
    // Now broadcast — subscribers can safely read meta.json after this fires.
    if err := s.cfg.Store.Transition(sess.ID, final); err != nil {
        return err
    }
    return nil
```

- [ ] **Step 3: Simplify the e2e test**

In `internal/server/e2e_test.go`, find the polling loop for meta.json existence and replace with a direct check:

```go
// Verify meta.json on disk (no poll needed — supervisor writes meta before broadcasting done).
meta := filepath.Join(stateDir, "sessions", sessID, "meta.json")
_, err = os.Stat(meta)
require.NoError(t, err, "meta.json must exist on disk after done at %s", meta)
```

Delete the poll loop. Remove the `time.Sleep` calls that wrapped it.

- [ ] **Step 4: Run tests**

```bash
go test ./... -count=1 -race
```

Expected: all packages PASS, including the simplified e2e test.

- [ ] **Step 5: Commit**

```bash
git add internal/pty/supervisor.go internal/server/e2e_test.go
git commit -m "pty: write meta before broadcasting done; simplify e2e test"
```

---

### Task B2: Wire max-concurrent-sessions semaphore

**Files:**
- Modify: `internal/server/server.go`
- Modify: `internal/server/client.go`
- Modify: `cmd/rex-daemon/main.go`
- Create: `internal/server/concurrency_test.go`

The `--max-concurrent-sessions` flag exists on `rex-daemon` but isn't enforced.

- [ ] **Step 1: Add the semaphore to Config + Server**

In `internal/server/server.go`, update `Config`:

```go
type Config struct {
	Socket               string
	StateDir             string
	Registry             *registry.Registry
	Store                *state.Store
	MaxConcurrentSessions int
}
```

Add a semaphore field to `Server`:

```go
import (
	// ... existing imports
	"golang.org/x/sync/semaphore"
)

type Server struct {
	cfg    Config
	sem    *semaphore.Weighted // nil = no cap
	wg     sync.WaitGroup
	once   sync.Once  //nolint:unused // placeholder for Plan B graceful drain
	closed bool       //nolint:unused // placeholder for Plan B graceful drain
}
```

Update `New`:

```go
func New(cfg Config) (*Server, error) {
	if cfg.Socket == "" {
		return nil, errors.New("server: empty socket path")
	}
	_ = os.Remove(cfg.Socket)
	s := &Server{cfg: cfg}
	if cfg.MaxConcurrentSessions > 0 {
		s.sem = semaphore.NewWeighted(int64(cfg.MaxConcurrentSessions))
	}
	return s, nil
}
```

- [ ] **Step 2: Expose semaphore acquire/release to the client handler**

Add a method on `Server`:

```go
// TryAcquireSession reserves a session slot. Returns false when the cap is reached.
func (s *Server) TryAcquireSession() bool {
	if s.sem == nil {
		return true
	}
	return s.sem.TryAcquire(1)
}

// ReleaseSession returns a session slot.
func (s *Server) ReleaseSession() {
	if s.sem == nil {
		return
	}
	s.sem.Release(1)
}
```

- [ ] **Step 3: Plumb Server reference into the client handler**

Change `handleClient` to take `*Server`:

In `server.go`, `Serve`'s goroutine dispatch:

```go
go func() {
    defer s.wg.Done()
    handleClient(ctx, conn, s)
}()
```

In `client.go`, change the signature:

```go
func handleClient(ctx context.Context, conn net.Conn, srv *Server) {
    cfg := srv.cfg
    // ... existing setup
```

Replace `cfg` accesses with `srv.cfg` where appropriate.

- [ ] **Step 4: Use the semaphore on NewSession**

Inside `handleNewSession`, before `cfg.Store.Add(sess)`:

```go
if !srv.TryAcquireSession() {
    return fmt.Errorf("%w: cap=%d", errTooManySessions, srv.cfg.MaxConcurrentSessions)
}
```

Wait — `handleNewSession` doesn't take `srv`. Update the call site and signature accordingly. Final signature:

```go
func handleNewSession(ctx context.Context, intentID string, p protocol.NewSession, srv *Server, w *protocol.Writer) error {
    cfg := srv.cfg
    // ...
    if !srv.TryAcquireSession() {
        return fmt.Errorf("too many concurrent sessions (cap=%d)", cfg.MaxConcurrentSessions)
    }
    // ... existing session creation ...
    
    go func() {
        defer srv.ReleaseSession()
        _ = sup.Run(ctx, sess)
    }()
    return nil
}
```

Update the `writeError` call to use `protocol.ErrCodeTooManySessions` when the error message contains "too many concurrent sessions". Or simpler: add an explicit check before the call:

```go
case protocol.IntentNewSession:
    var p protocol.NewSession
    if err := json.Unmarshal(env.Data, &p); err != nil {
        writeError(w, env.ID, protocol.ErrCodeBadIntent, err.Error())
        continue
    }
    if err := handleNewSession(ctx, env.ID, p, srv, w); err != nil {
        code := protocol.ErrCodeSpawn
        if strings.Contains(err.Error(), "too many concurrent sessions") {
            code = protocol.ErrCodeTooManySessions
        }
        writeError(w, env.ID, code, err.Error())
    }
```

(Add `"strings"` to imports if missing.)

- [ ] **Step 5: Wire the flag in cmd/rex-daemon/main.go**

In `run()`, add a flag and pass it through:

```go
maxConcurrent := fs.Int("max-concurrent-sessions", 16, "cap on live PTY sessions")
```

In the server.Config construction:

```go
srv, err := server.New(server.Config{
    Socket:                *socketPath,
    StateDir:              *stateDir,
    Registry:              reg,
    Store:                 store,
    MaxConcurrentSessions: *maxConcurrent,
})
```

- [ ] **Step 6: Write a concurrency test at `internal/server/concurrency_test.go`**

```go
package server

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/protocol"
	"github.com/tristanbietsch/rex/internal/registry"
	"github.com/tristanbietsch/rex/internal/state"
)

func TestMaxConcurrentSessions_RejectsOverflow(t *testing.T) {
	dir, err := os.MkdirTemp("", "rex-conc")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	sock := filepath.Join(dir, "rex.sock")
	reg, err := registry.Load("")
	require.NoError(t, err)
	srv, err := New(Config{
		Socket:                sock,
		StateDir:              dir,
		Registry:              reg,
		Store:                 state.NewStore(),
		MaxConcurrentSessions: 1,
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.Serve(ctx) }()

	for i := 0; i < 100; i++ {
		if _, err := net.Dial("unix", sock); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	conn, err := net.Dial("unix", sock)
	require.NoError(t, err)
	defer conn.Close()
	w := protocol.NewWriter(conn)
	r := protocol.NewReader(conn)

	require.NoError(t, w.WriteIntent(protocol.IntentHello, "h", protocol.Hello{}))
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	env, err := r.Read()
	require.NoError(t, err)
	require.Equal(t, protocol.EventSnapshot, env.Type)

	// Spawn the first (long-running): should succeed.
	require.NoError(t, w.WriteIntent(protocol.IntentNewSession, "n1", protocol.NewSession{
		ToolID: "echo", ModelID: "long", Slug: "s1", CWD: dir,
	}))
	// Wait for SessionAdded.
	for {
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		env, err := r.Read()
		require.NoError(t, err)
		if env.Type == protocol.EventSessionAdded {
			break
		}
	}

	// Second NewSession should hit the cap and emit Error with code "too_many_sessions".
	require.NoError(t, w.WriteIntent(protocol.IntentNewSession, "n2", protocol.NewSession{
		ToolID: "echo", ModelID: "short", Slug: "s2", CWD: dir,
	}))
	gotError := false
	deadline := time.Now().Add(3 * time.Second)
	for !gotError && time.Now().Before(deadline) {
		conn.SetReadDeadline(deadline)
		env, err := r.Read()
		require.NoError(t, err)
		if env.Type == protocol.EventError {
			var ee protocol.ErrorEvent
			require.NoError(t, json.Unmarshal(env.Data, &ee))
			if ee.Code == protocol.ErrCodeTooManySessions {
				gotError = true
			}
		}
	}
	require.True(t, gotError, "second NewSession should have errored with too_many_sessions")
}
```

(Add `"os"` to imports.)

- [ ] **Step 7: Run tests**

```bash
go get golang.org/x/sync/semaphore
go build ./...
go test ./... -count=1 -race
```

Expected: all PASS including the new concurrency test.

- [ ] **Step 8: Commit**

```bash
git add internal/server/ cmd/rex-daemon/main.go go.mod go.sum
git commit -m "server: enforce max-concurrent-sessions semaphore; emit too_many_sessions error"
```

---

## Phase 2 — Client library (3 tasks)

### Task B3: Daemon client library

**Files:**
- Create: `internal/client/client.go`
- Create: `internal/client/client_test.go`

A small library wrapping the protocol so each CLI verb doesn't reimplement connection management.

- [ ] **Step 1: Write the failing test**

Create `internal/client/client_test.go`:

```go
package client_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/client"
	"github.com/tristanbietsch/rex/internal/protocol"
	"github.com/tristanbietsch/rex/internal/registry"
	"github.com/tristanbietsch/rex/internal/server"
	"github.com/tristanbietsch/rex/internal/state"
)

func TestClient_DialHelloSnapshot(t *testing.T) {
	dir, err := os.MkdirTemp("", "rex-cli")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	sock := filepath.Join(dir, "rex.sock")
	reg, err := registry.Load("")
	require.NoError(t, err)
	srv, err := server.New(server.Config{Socket: sock, StateDir: dir, Registry: reg, Store: state.NewStore()})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.Serve(ctx) }()

	// Wait for socket.
	for i := 0; i < 100; i++ {
		if _, err := net.Dial("unix", sock); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	c, err := client.Dial(sock)
	require.NoError(t, err)
	defer c.Close()

	snap, err := c.Hello("test")
	require.NoError(t, err)
	require.Equal(t, "all", snap.Filter)
	require.Empty(t, snap.Sessions)
}
```

- [ ] **Step 2: Implement** `internal/client/client.go`

```go
// Package client is a Go SDK for rex-daemon's UDS protocol.
package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/tristanbietsch/rex/internal/protocol"
)

// Client wraps a UDS connection with reader/writer helpers.
type Client struct {
	conn net.Conn
	r    *protocol.Reader
	w    *protocol.Writer
}

// Dial opens a UDS connection.
func Dial(socket string) (*Client, error) {
	conn, err := net.Dial("unix", socket)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", socket, err)
	}
	return &Client{
		conn: conn,
		r:    protocol.NewReader(conn),
		w:    protocol.NewWriter(conn),
	}, nil
}

// Close drops the connection.
func (c *Client) Close() error { return c.conn.Close() }

// SetReadDeadline forwards to the underlying conn.
func (c *Client) SetReadDeadline(t time.Time) error { return c.conn.SetReadDeadline(t) }

// Hello sends a Hello intent and decodes the Snapshot response.
func (c *Client) Hello(clientVersion string) (*protocol.Snapshot, error) {
	if err := c.w.WriteIntent(protocol.IntentHello, "h", protocol.Hello{ClientVersion: clientVersion}); err != nil {
		return nil, err
	}
	env, err := c.r.Read()
	if err != nil {
		return nil, err
	}
	if env.Kind != protocol.KindEvent || env.Type != protocol.EventSnapshot {
		return nil, fmt.Errorf("expected Snapshot, got %s/%s", env.Kind, env.Type)
	}
	var snap protocol.Snapshot
	if err := json.Unmarshal(env.Data, &snap); err != nil {
		return nil, fmt.Errorf("decode snapshot: %w", err)
	}
	return &snap, nil
}

// Subscribe pins this connection to a session's output stream. Use sessionID="" for board-wide only.
func (c *Client) Subscribe(sessionID string) error {
	return c.w.WriteIntent(protocol.IntentSubscribe, "", protocol.Subscribe{SessionID: sessionID})
}

// NewSession submits a NewSession intent.
func (c *Client) NewSession(req protocol.NewSession) error {
	return c.w.WriteIntent(protocol.IntentNewSession, "", req)
}

// SendInput forwards raw bytes to the session's PTY.
func (c *Client) SendInput(sessionID string, b []byte) error {
	return c.w.WriteIntent(protocol.IntentSendInput, "", protocol.SendInput{SessionID: sessionID, Bytes: b})
}

// Reply sends an inline text reply.
func (c *Client) Reply(sessionID, text string) error {
	return c.w.WriteIntent(protocol.IntentReply, "", protocol.Reply{SessionID: sessionID, Text: text})
}

// Rename changes a session's slug or title.
func (c *Client) Rename(sessionID, slug, title string) error {
	return c.w.WriteIntent(protocol.IntentRename, "", protocol.Rename{SessionID: sessionID, Slug: slug, Title: title})
}

// Delete removes a session.
func (c *Client) Delete(sessionID string) error {
	return c.w.WriteIntent(protocol.IntentDelete, "", protocol.Delete{SessionID: sessionID})
}

// FocusFilter sets the cosmetic tool filter.
func (c *Client) FocusFilter(toolID string) error {
	return c.w.WriteIntent(protocol.IntentFocusFilter, "", protocol.FocusFilter{ToolID: toolID})
}

// NextEvent reads one envelope from the stream.
func (c *Client) NextEvent() (protocol.Envelope, error) {
	return c.r.Read()
}

// Drain reads events until EOF or ctx-canceled.
func (c *Client) Drain(handler func(protocol.Envelope) bool) error {
	for {
		env, err := c.r.Read()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if !handler(env) {
			return nil
		}
	}
}
```

- [ ] **Step 3: Build + test**

```bash
go build ./...
go test ./internal/client/... -v -count=1
go test ./... -count=1
```

Expected: client test PASS; all packages still PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/client/
git commit -m "client: Go SDK wrapping the UDS protocol"
```

---

### Task B4: `rex` binary scaffold + verb dispatcher

**Files:**
- Create: `cmd/rex/main.go`
- Create: `internal/cli/version.go`
- Modify: `Makefile` to add a `build-all` target

- [ ] **Step 1: Create `cmd/rex/main.go`** — the dispatcher:

```go
// Package main is the rex CLI entry point.
package main

import (
	"fmt"
	"os"

	"github.com/tristanbietsch/rex/internal/cli"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "rex:", err)
		os.Exit(exitCodeFor(err))
	}
}

func run(args []string) error {
	if len(args) == 0 {
		// No args = open the TUI (Plan C). For now, print a friendly message.
		fmt.Fprintln(os.Stderr, "rex: TUI not built yet (lands in Plan C). Use a subcommand:")
		fmt.Fprintln(os.Stderr, "  rex status | ls | new | attach | reply | send | log | wait | rm | rename | archive | reload | daemon | slash | config | completion")
		return nil
	}
	switch args[0] {
	case "--version", "-v", "version":
		return cli.RunVersion()
	case "status":
		return cli.RunStatus(args[1:])
	case "ls":
		return cli.RunLs(args[1:])
	case "new":
		return cli.RunNew(args[1:])
	case "attach":
		return cli.RunAttach(args[1:])
	case "reply":
		return cli.RunReply(args[1:])
	case "send":
		return cli.RunSend(args[1:])
	case "log":
		return cli.RunLog(args[1:])
	case "wait":
		return cli.RunWait(args[1:])
	case "rm":
		return cli.RunRm(args[1:])
	case "rename":
		return cli.RunRename(args[1:])
	case "archive":
		return cli.RunArchive(args[1:])
	case "reload":
		return cli.RunReload(args[1:])
	case "daemon":
		return cli.RunDaemon(args[1:])
	case "completion":
		return cli.RunCompletion(args[1:])
	default:
		return fmt.Errorf("unknown command %q (try `rex --version`)", args[0])
	}
}

func exitCodeFor(err error) int {
	if e, ok := err.(cli.ExitCoder); ok {
		return e.ExitCode()
	}
	return 1
}
```

- [ ] **Step 2: Create the `ExitCoder` interface + `RunVersion`** in `internal/cli/version.go`:

```go
// Package cli implements the rex CLI subcommands.
package cli

import "fmt"

const version = "0.0.2-plan-b"

// ExitCoder is any error that carries a specific exit code.
type ExitCoder interface {
	error
	ExitCode() int
}

// ExitError carries a numeric exit code alongside an error message.
type ExitError struct {
	Code int
	Msg  string
}

func (e ExitError) Error() string  { return e.Msg }
func (e ExitError) ExitCode() int  { return e.Code }

// NewExitError wraps a message with an exit code.
func NewExitError(code int, msg string) ExitError { return ExitError{Code: code, Msg: msg} }

// Exit code constants (mirrors docs/cli.md exit-codes section).
const (
	ExitOK                    = 0
	ExitGeneric               = 1
	ExitSelectorNotFound      = 2
	ExitAmbiguousSelector     = 3
	ExitDaemonUnreachable     = 4
	ExitInvalidArgs           = 5
	ExitOperationRefused      = 6
	ExitWaitTimedOut          = 7
)

// RunVersion prints the binary version.
func RunVersion() error {
	fmt.Println(version)
	return nil
}

// Run* stubs for sibling commands.
func notImplemented(verb string) error {
	return NewExitError(ExitGeneric, fmt.Sprintf("%s: not implemented yet", verb))
}

func RunStatus(args []string) error     { return notImplemented("status") }
func RunLs(args []string) error         { return notImplemented("ls") }
func RunNew(args []string) error        { return notImplemented("new") }
func RunAttach(args []string) error     { return notImplemented("attach") }
func RunReply(args []string) error      { return notImplemented("reply") }
func RunSend(args []string) error       { return notImplemented("send") }
func RunLog(args []string) error        { return notImplemented("log") }
func RunWait(args []string) error       { return notImplemented("wait") }
func RunRm(args []string) error         { return notImplemented("rm") }
func RunRename(args []string) error     { return notImplemented("rename") }
func RunArchive(args []string) error    { return notImplemented("archive") }
func RunReload(args []string) error     { return notImplemented("reload") }
func RunDaemon(args []string) error     { return notImplemented("daemon") }
func RunCompletion(args []string) error { return notImplemented("completion") }
```

Each subcommand task below will REPLACE its stub with the real implementation.

- [ ] **Step 3: Update Makefile**

Replace the Makefile contents with:

```makefile
.PHONY: build build-all test lint clean

build: build-all

build-all:
	go build -o rex-daemon ./cmd/rex-daemon
	go build -o rex ./cmd/rex

test:
	go test ./...

lint:
	golangci-lint run

clean:
	rm -f rex-daemon rex
```

(Recipes use TAB indentation.)

- [ ] **Step 4: Build + smoke**

```bash
make build-all
./rex --version
./rex-daemon --version
./rex status
```

Expected:
```
0.0.2-plan-b
0.0.1-plan-a
rex: status: not implemented yet
```

(Exit code on the last call is 1.)

- [ ] **Step 5: Commit**

```bash
git add cmd/rex/ internal/cli/version.go Makefile
git commit -m "rex: CLI binary scaffold + verb dispatcher + version command"
```

---

### Task B5: Selector resolution + output helpers

**Files:**
- Create: `internal/cli/selector.go`
- Create: `internal/cli/selector_test.go`
- Create: `internal/cli/output.go`
- Create: `internal/cli/output_test.go`
- Create: `internal/cli/socket.go`

- [ ] **Step 1: Selector** at `internal/cli/selector.go`:

```go
package cli

import (
	"fmt"
	"regexp"

	"github.com/tristanbietsch/rex/internal/client"
	"github.com/tristanbietsch/rex/internal/protocol"
)

var hexShortID = regexp.MustCompile(`^[0-9a-f]{4,}$`)
var fullUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// ResolveSelector finds a session by id, slug, or short_id from the daemon's snapshot.
// Returns ExitSelectorNotFound or ExitAmbiguousSelector on failure.
func ResolveSelector(c *client.Client, sel string) (protocol.SessionSummary, error) {
	snap, err := c.Hello("rex-cli")
	if err != nil {
		return protocol.SessionSummary{}, err
	}
	return ResolveInSnapshot(snap.Sessions, sel)
}

// ResolveInSnapshot is the testable core.
func ResolveInSnapshot(sessions []protocol.SessionSummary, sel string) (protocol.SessionSummary, error) {
	if sel == "" {
		return protocol.SessionSummary{}, NewExitError(ExitInvalidArgs, "empty selector")
	}
	matches := matchAll(sessions, sel)
	switch len(matches) {
	case 0:
		return protocol.SessionSummary{}, NewExitError(ExitSelectorNotFound, fmt.Sprintf("no session matches %q", sel))
	case 1:
		return matches[0], nil
	default:
		return protocol.SessionSummary{}, NewExitError(ExitAmbiguousSelector,
			fmt.Sprintf("selector %q matches %d sessions; be more specific", sel, len(matches)))
	}
}

func matchAll(sessions []protocol.SessionSummary, sel string) []protocol.SessionSummary {
	// Exact full UUID match wins outright.
	if fullUUID.MatchString(sel) {
		for _, s := range sessions {
			if s.ID == sel {
				return []protocol.SessionSummary{s}
			}
		}
		return nil
	}
	var out []protocol.SessionSummary
	// Exact slug match wins outright.
	for _, s := range sessions {
		if s.Slug == sel {
			return []protocol.SessionSummary{s}
		}
	}
	// Short-id (hex) prefix match — must be exact short_id or a longer hex prefix of the full id.
	if hexShortID.MatchString(sel) {
		for _, s := range sessions {
			if s.ShortID == sel || (len(s.ID) >= len(sel) && s.ID[:len(sel)] == sel) {
				out = append(out, s)
			}
		}
	}
	return out
}
```

- [ ] **Step 2: Selector test** at `internal/cli/selector_test.go`:

```go
package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/protocol"
)

func TestResolveInSnapshot(t *testing.T) {
	sessions := []protocol.SessionSummary{
		{ID: "7d4f3c8a-1234-4abc-89ab-cdefabcdef01", ShortID: "7d4f", Slug: "alpha"},
		{ID: "b203a999-1234-4abc-89ab-cdefabcdef02", ShortID: "b203", Slug: "beta"},
	}

	got, err := ResolveInSnapshot(sessions, "alpha")
	require.NoError(t, err)
	require.Equal(t, "7d4f3c8a-1234-4abc-89ab-cdefabcdef01", got.ID)

	got, err = ResolveInSnapshot(sessions, "b203")
	require.NoError(t, err)
	require.Equal(t, "beta", got.Slug)

	got, err = ResolveInSnapshot(sessions, "7d4f3c8a-1234-4abc-89ab-cdefabcdef01")
	require.NoError(t, err)
	require.Equal(t, "alpha", got.Slug)

	_, err = ResolveInSnapshot(sessions, "nope")
	require.Error(t, err)
	require.IsType(t, ExitError{}, err)
}
```

- [ ] **Step 3: Output helpers** at `internal/cli/output.go`:

```go
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/tristanbietsch/rex/internal/protocol"
)

// WriteSessionsTable prints sessions as a fixed-width table to w.
func WriteSessionsTable(w io.Writer, sessions []protocol.SessionSummary) error {
	if len(sessions) == 0 {
		_, err := fmt.Fprintln(w, "(no sessions)")
		return err
	}
	hdr := fmt.Sprintf("%-5s  %-12s  %-9s  %-20s  %-24s  %s",
		"ID", "STATE", "TOOL", "MODEL", "SLUG", "LAST EVENT")
	if _, err := fmt.Fprintln(w, hdr); err != nil {
		return err
	}
	for _, s := range sessions {
		ago := durationAgo(s.LastEventAt)
		model := s.ModelID
		if s.Effort != "" {
			model = model + " · " + s.Effort
		}
		row := fmt.Sprintf("%-5s  %-12s  %-9s  %-20s  %-24s  %s",
			s.ShortID, s.State, s.ToolID, truncate(model, 20), truncate(s.Slug, 24), ago)
		if _, err := fmt.Fprintln(w, row); err != nil {
			return err
		}
	}
	return nil
}

// WriteSessionsJSONL prints one JSON object per session per line.
func WriteSessionsJSONL(w io.Writer, sessions []protocol.SessionSummary) error {
	for _, s := range sessions {
		b, err := json.Marshal(s)
		if err != nil {
			return err
		}
		if _, err := w.Write(append(b, '\n')); err != nil {
			return err
		}
	}
	return nil
}

// WriteAggregateLine prints "N awaiting input · N working · N completed".
func WriteAggregateLine(w io.Writer, sessions []protocol.SessionSummary) error {
	var working, needsInput, done, failed, crashed int
	for _, s := range sessions {
		switch s.State {
		case protocol.StateWorking:
			working++
		case protocol.StateNeedsInput:
			needsInput++
		case protocol.StateDone:
			done++
		case protocol.StateFailed:
			failed++
		case protocol.StateCrashed:
			crashed++
		}
	}
	parts := []string{
		fmt.Sprintf("%d awaiting input", needsInput),
		fmt.Sprintf("%d working", working),
		fmt.Sprintf("%d completed", done),
	}
	if failed+crashed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", failed+crashed))
	}
	_, err := fmt.Fprintln(w, strings.Join(parts, " · "))
	return err
}

// WriteAggregateJSON prints the same counts as a single JSON object.
func WriteAggregateJSON(w io.Writer, sessions []protocol.SessionSummary) error {
	counts := map[string]int{
		"awaiting_input": 0, "working": 0, "completed": 0, "failed": 0, "crashed": 0,
	}
	for _, s := range sessions {
		switch s.State {
		case protocol.StateWorking:
			counts["working"]++
		case protocol.StateNeedsInput:
			counts["awaiting_input"]++
		case protocol.StateDone:
			counts["completed"]++
		case protocol.StateFailed:
			counts["failed"]++
		case protocol.StateCrashed:
			counts["crashed"]++
		}
	}
	b, err := json.Marshal(counts)
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

func durationAgo(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours())/24)
	}
}
```

- [ ] **Step 4: Output test** at `internal/cli/output_test.go`:

```go
package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/protocol"
)

func TestWriteSessionsTable_Empty(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, WriteSessionsTable(&buf, nil))
	require.Contains(t, buf.String(), "no sessions")
}

func TestWriteSessionsTable_Rows(t *testing.T) {
	sessions := []protocol.SessionSummary{
		{ShortID: "7d4f", State: protocol.StateWorking, ToolID: "claude", ModelID: "opus", Effort: "high", Slug: "demo", LastEventAt: time.Now().Add(-2 * time.Minute)},
	}
	var buf bytes.Buffer
	require.NoError(t, WriteSessionsTable(&buf, sessions))
	out := buf.String()
	require.Contains(t, out, "7d4f")
	require.Contains(t, out, "working")
	require.Contains(t, out, "demo")
	require.Contains(t, out, "opus · high")
}

func TestWriteAggregateLine(t *testing.T) {
	sessions := []protocol.SessionSummary{
		{State: protocol.StateWorking},
		{State: protocol.StateWorking},
		{State: protocol.StateNeedsInput},
		{State: protocol.StateDone},
	}
	var buf bytes.Buffer
	require.NoError(t, WriteAggregateLine(&buf, sessions))
	out := buf.String()
	require.True(t, strings.Contains(out, "1 awaiting input · 2 working · 1 completed"))
}
```

- [ ] **Step 5: Common socket helper** at `internal/cli/socket.go`:

```go
package cli

import (
	"os"
	"path/filepath"
)

// DefaultSocket returns the default UDS path, matching rex-daemon's logic.
func DefaultSocket() string {
	if r := os.Getenv("XDG_RUNTIME_DIR"); r != "" {
		return filepath.Join(r, "rex.sock")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "rex", "rex.sock")
}
```

- [ ] **Step 6: Build + test**

```bash
go build ./...
go test ./internal/cli/... -v -count=1
go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/
git commit -m "cli: selector resolution, output helpers, default socket"
```

---

## Phase 3 — Read commands (3 tasks)

### Task B6: `rex status`

**File:** Modify `internal/cli/status.go` (replace stub) — actually we put the stub in `version.go`; create a new file:

- [ ] **Step 1: Move `RunStatus` out of `version.go` into `internal/cli/status.go`:**

In `internal/cli/version.go`, delete the line `func RunStatus(args []string) error { return notImplemented("status") }`.

Create `internal/cli/status.go`:

```go
package cli

import (
	"flag"
	"os"

	"github.com/tristanbietsch/rex/internal/client"
	"github.com/tristanbietsch/rex/internal/protocol"
)

// RunStatus prints an aggregate one-liner of session counts.
func RunStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	socket := fs.String("socket", DefaultSocket(), "UDS path")
	asJSON := fs.Bool("json", false, "output JSON")
	if err := fs.Parse(args); err != nil {
		return NewExitError(ExitInvalidArgs, err.Error())
	}

	c, err := client.Dial(*socket)
	if err != nil {
		return NewExitError(ExitDaemonUnreachable, err.Error())
	}
	defer c.Close()
	snap, err := c.Hello("rex-cli")
	if err != nil {
		return NewExitError(ExitDaemonUnreachable, err.Error())
	}

	if *asJSON {
		if err := WriteAggregateJSON(os.Stdout, snap.Sessions); err != nil {
			return err
		}
	} else {
		if err := WriteAggregateLine(os.Stdout, snap.Sessions); err != nil {
			return err
		}
	}

	// Exit code 1 if anything needs input.
	for _, s := range snap.Sessions {
		if s.State == protocol.StateNeedsInput {
			return NewExitError(ExitGeneric, "")
		}
	}
	return nil
}
```

- [ ] **Step 2: Test against the real daemon** in a temp script:

```bash
make build-all
DIR=$(mktemp -d)
./rex-daemon --socket $DIR/rex.sock --state-dir $DIR &
DAEMON=$!
sleep 0.5

./rex status --socket $DIR/rex.sock

kill $DAEMON
wait $DAEMON 2>/dev/null || true
```

Expected output: `0 awaiting input · 0 working · 0 completed`. Exit code 0.

- [ ] **Step 3: Build + test**

```bash
go build ./...
go test ./... -count=1
```

- [ ] **Step 4: Commit**

```bash
git add internal/cli/status.go internal/cli/version.go
git commit -m "cli: rex status — aggregate one-liner with JSON mode"
```

---

### Task B7: `rex ls`

**Files:**
- Create: `internal/cli/ls.go` (delete the stub in `version.go` for `RunLs` first)
- Create: `internal/cli/ls_test.go`

- [ ] **Step 1: Remove the `RunLs` stub from `internal/cli/version.go`.**

- [ ] **Step 2: Create `internal/cli/ls.go`:**

```go
package cli

import (
	"flag"
	"os"

	"github.com/tristanbietsch/rex/internal/client"
	"github.com/tristanbietsch/rex/internal/protocol"
)

// RunLs prints session table or JSONL.
func RunLs(args []string) error {
	fs := flag.NewFlagSet("ls", flag.ContinueOnError)
	socket := fs.String("socket", DefaultSocket(), "UDS path")
	stateFilter := fs.String("state", "", "filter by state")
	toolFilter := fs.String("tool", "", "filter by tool id")
	modelFilter := fs.String("model", "", "filter by model id")
	showArchived := fs.Bool("show-archived", false, "include archived sessions")
	short := fs.Bool("short", false, "compact one-line-per-session mode (default false)")
	asJSON := fs.Bool("json", false, "output JSONL")
	if err := fs.Parse(args); err != nil {
		return NewExitError(ExitInvalidArgs, err.Error())
	}
	_ = showArchived // Plan A doesn't expose archive state on the wire; Plan B/C extension point.
	_ = short        // text mode is single-line per session already.

	c, err := client.Dial(*socket)
	if err != nil {
		return NewExitError(ExitDaemonUnreachable, err.Error())
	}
	defer c.Close()
	snap, err := c.Hello("rex-cli")
	if err != nil {
		return NewExitError(ExitDaemonUnreachable, err.Error())
	}

	filtered := snap.Sessions[:0]
	for _, s := range snap.Sessions {
		if *stateFilter != "" && string(s.State) != *stateFilter {
			continue
		}
		if *toolFilter != "" && s.ToolID != *toolFilter {
			continue
		}
		if *modelFilter != "" && s.ModelID != *modelFilter {
			continue
		}
		filtered = append(filtered, s)
	}

	_ = protocol.SessionSummary{} // keep import live
	if *asJSON {
		return WriteSessionsJSONL(os.Stdout, filtered)
	}
	return WriteSessionsTable(os.Stdout, filtered)
}
```

- [ ] **Step 3: Build + test**

```bash
go build ./...
make build-all

DIR=$(mktemp -d)
./rex-daemon --socket $DIR/rex.sock --state-dir $DIR &
DAEMON=$!
sleep 0.5
./rex ls --socket $DIR/rex.sock
./rex ls --socket $DIR/rex.sock --json
kill $DAEMON; wait $DAEMON 2>/dev/null || true
```

Expected: empty table headers + "(no sessions)". `--json` produces no output (empty JSONL).

- [ ] **Step 4: Commit**

```bash
git add internal/cli/
git commit -m "cli: rex ls — list sessions with state/tool/model filters and JSON mode"
```

---

### Task B8: `rex log`

**Files:**
- Create: `internal/cli/log.go` (delete RunLog stub first)
- Create: `internal/cli/log_test.go`

- [ ] **Step 1: Delete `RunLog` stub from `internal/cli/version.go`.**

- [ ] **Step 2: Create `internal/cli/log.go`:**

```go
package cli

import (
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/tristanbietsch/rex/internal/client"
)

// RunLog prints (or tails) the transcript for a session.
//
// Plan B reads the transcript file directly from disk. Plan C wires this into
// the daemon's SessionOutput stream for live tailing of in-memory output.
func RunLog(args []string) error {
	fs := flag.NewFlagSet("log", flag.ContinueOnError)
	socket := fs.String("socket", DefaultSocket(), "UDS path")
	stateDir := fs.String("state-dir", defaultStateDir(), "state dir (rarely needed)")
	follow := fs.Bool("f", false, "follow the transcript (like tail -f)")
	tailN := fs.Int("n", 0, "show only the last N lines (0 = all)")
	asBytes := fs.Bool("bytes", false, "print raw bytes (including ANSI)")
	if err := fs.Parse(args); err != nil {
		return NewExitError(ExitInvalidArgs, err.Error())
	}
	if fs.NArg() != 1 {
		return NewExitError(ExitInvalidArgs, "log: exactly one selector required")
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

	path := filepath.Join(*stateDir, "sessions", sess.ID, "transcript.log")
	if err := streamFile(path, os.Stdout, *follow, *tailN, *asBytes); err != nil {
		return NewExitError(ExitGeneric, err.Error())
	}
	return nil
}

func defaultStateDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "rex")
}

func streamFile(path string, out io.Writer, follow bool, tailN int, raw bool) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	if tailN > 0 {
		if err := emitTail(f, out, tailN, raw); err != nil {
			return err
		}
	} else {
		if err := emitAll(f, out, raw); err != nil {
			return err
		}
	}
	if !follow {
		return nil
	}
	// Follow mode: poll for new bytes every 100ms.
	for {
		_, err := io.Copy(out, f)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func emitAll(r io.Reader, w io.Writer, raw bool) error {
	if raw {
		_, err := io.Copy(w, r)
		return err
	}
	return copyStripped(r, w)
}

func emitTail(f *os.File, w io.Writer, n int, raw bool) error {
	// Read whole file into memory; for v0 transcript sizes (≤16MB) this is fine.
	data, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	lines := splitLastN(data, n)
	if raw {
		_, err := w.Write(lines)
		return err
	}
	return copyStripped(stringsReader(lines), w)
}

func copyStripped(r io.Reader, w io.Writer) error {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			cleaned := stripANSI(buf[:n])
			if _, werr := w.Write(cleaned); werr != nil {
				return werr
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func stringsReader(b []byte) io.Reader { return bytesReaderImpl(b) }

type bytesReaderImpl []byte

func (b bytesReaderImpl) Read(p []byte) (int, error) {
	if len(b) == 0 {
		return 0, io.EOF
	}
	n := copy(p, b)
	if n < len(b) {
		// caller didn't consume all; we return io.EOF anyway on next call
		return n, nil
	}
	return n, io.EOF
}

func splitLastN(b []byte, n int) []byte {
	if n <= 0 || len(b) == 0 {
		return b
	}
	count := 0
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] == '\n' {
			count++
			if count == n+1 {
				return b[i+1:]
			}
		}
	}
	return b
}

// Crude ANSI control-sequence stripper. Removes CSI escapes (most common color/cursor codes).
func stripANSI(b []byte) []byte {
	out := make([]byte, 0, len(b))
	i := 0
	for i < len(b) {
		if b[i] == 0x1b && i+1 < len(b) && b[i+1] == '[' {
			// CSI: ESC [ ... <final-byte>
			j := i + 2
			for j < len(b) && (b[j] < 0x40 || b[j] > 0x7e) {
				j++
			}
			if j < len(b) {
				j++ // skip the final byte
			}
			i = j
			continue
		}
		out = append(out, b[i])
		i++
	}
	return out
}
```

- [ ] **Step 3: Quick smoke**

```bash
make build-all
DIR=$(mktemp -d)
./rex-daemon --socket $DIR/rex.sock --state-dir $DIR &
DAEMON=$!
sleep 0.5

# Spawn an echo session and let it complete
printf '{"v":1,"kind":"Intent","type":"Hello","id":"h","data":{}}\n{"v":1,"kind":"Intent","type":"NewSession","id":"n","data":{"tool_id":"echo","model_id":"short","slug":"logtest","cwd":"/tmp"}}\n' | nc -U -q3 $DIR/rex.sock | head -1

# Find the slug and use it as a selector
sleep 3
./rex log --socket $DIR/rex.sock --state-dir $DIR logtest

kill $DAEMON; wait $DAEMON 2>/dev/null || true
```

Expected: output of the echo session transcript ("hello from rex" ... "work in progress" ... "done").

- [ ] **Step 4: Build + tests**

```bash
go build ./...
go test ./... -count=1
```

- [ ] **Step 5: Commit**

```bash
git add internal/cli/
git commit -m "cli: rex log [-f] [-n N] [--bytes] — print or tail a session transcript"
```

---

## Phase 4 — Write commands (4 tasks)

### Task B9: `rex new`

**File:** Create `internal/cli/new.go`; delete the `RunNew` stub.

- [ ] **Step 1: Delete `RunNew` stub from `version.go`.**

- [ ] **Step 2: Create `internal/cli/new.go`:**

```go
package cli

import (
	"flag"
	"io"
	"os"
	"strings"

	"github.com/tristanbietsch/rex/internal/client"
	"github.com/tristanbietsch/rex/internal/protocol"
)

// RunNew spawns a new agent session non-interactively.
func RunNew(args []string) error {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	socket := fs.String("socket", DefaultSocket(), "UDS path")
	tool := fs.String("tool", "echo", "tool id")
	model := fs.String("model", "short", "model id within tool")
	effort := fs.String("effort", "", "reasoning effort")
	slug := fs.String("slug", "", "session slug (defaults to derived from prompt)")
	cwd := fs.String("cwd", "", "working directory for the agent (defaults to $PWD)")
	noAttach := fs.Bool("no-attach", true, "spawn and exit (Plan B default; Plan C may attach by default)")
	if err := fs.Parse(args); err != nil {
		return NewExitError(ExitInvalidArgs, err.Error())
	}
	_ = noAttach // attach handled by `rex attach` separately in Plan B

	// Resolve cwd
	if *cwd == "" {
		wd, err := os.Getwd()
		if err != nil {
			return NewExitError(ExitGeneric, err.Error())
		}
		*cwd = wd
	}

	// Prompt = positional args joined, or stdin if no positional args and stdin not a TTY
	prompt := strings.Join(fs.Args(), " ")
	if prompt == "" {
		fi, _ := os.Stdin.Stat()
		if (fi.Mode() & os.ModeCharDevice) == 0 {
			b, err := io.ReadAll(os.Stdin)
			if err != nil {
				return NewExitError(ExitGeneric, err.Error())
			}
			prompt = string(b)
		}
	}

	// Derive slug if not set
	if *slug == "" {
		if prompt != "" {
			*slug = deriveSlug(prompt)
		} else {
			*slug = "session"
		}
	}

	c, err := client.Dial(*socket)
	if err != nil {
		return NewExitError(ExitDaemonUnreachable, err.Error())
	}
	defer c.Close()
	if _, err := c.Hello("rex-cli"); err != nil {
		return NewExitError(ExitDaemonUnreachable, err.Error())
	}

	req := protocol.NewSession{
		ToolID: *tool, ModelID: *model, Effort: *effort,
		Slug: *slug, CWD: *cwd, InitialPrompt: prompt,
	}
	if err := c.NewSession(req); err != nil {
		return NewExitError(ExitGeneric, err.Error())
	}

	// Wait for SessionAdded to surface the new id.
	if err := c.SetReadDeadline(deadlineSeconds(5)); err != nil {
		return NewExitError(ExitGeneric, err.Error())
	}
	for {
		env, err := c.NextEvent()
		if err != nil {
			return NewExitError(ExitGeneric, err.Error())
		}
		if env.Type == protocol.EventSessionAdded {
			var sum protocol.SessionSummary
			if err := jsonUnmarshal(env.Data, &sum); err != nil {
				return NewExitError(ExitGeneric, err.Error())
			}
			os.Stdout.WriteString(sum.ShortID + "\t" + sum.Slug + "\n")
			return nil
		}
		if env.Type == protocol.EventError {
			return NewExitError(ExitGeneric, "spawn failed")
		}
	}
}

func deriveSlug(prompt string) string {
	// First 32 chars, lowercased, non-word -> "-"
	s := prompt
	if len(s) > 32 {
		s = s[:32]
	}
	s = strings.ToLower(s)
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.TrimRight(b.String(), "-")
}
```

- [ ] **Step 3: Add tiny helpers** — append to `internal/cli/output.go`:

```go
// jsonUnmarshal is encoding/json.Unmarshal alias to avoid importing encoding/json in every cli file.
import "encoding/json"

// (Actually no — let's just import encoding/json where needed. Remove this block — it's vestigial.)
```

Actually skip the helper. Modify `new.go` to import `encoding/json` directly:

```go
import (
    "encoding/json"
    ...
)
```

And call `json.Unmarshal(env.Data, &sum)` directly. Replace `jsonUnmarshal` with `json.Unmarshal`.

Also need `deadlineSeconds`:

```go
import "time"

func deadlineSeconds(n int) time.Time {
    return time.Now().Add(time.Duration(n) * time.Second)
}
```

Add `deadlineSeconds` to `socket.go` so all CLI files can share it.

- [ ] **Step 4: Update internal/cli/socket.go** to add:

```go
import "time"

func deadlineSeconds(n int) time.Time {
    return time.Now().Add(time.Duration(n) * time.Second)
}
```

- [ ] **Step 5: Build + smoke**

```bash
make build-all
DIR=$(mktemp -d)
./rex-daemon --socket $DIR/rex.sock --state-dir $DIR &
DAEMON=$!
sleep 0.5
./rex new --socket $DIR/rex.sock --tool echo --model short --slug foo
sleep 3
./rex ls --socket $DIR/rex.sock
kill $DAEMON; wait $DAEMON 2>/dev/null || true
```

Expected: `rex new` prints `<short-id>\tfoo`, then `rex ls` shows the foo session (likely `done` by then).

- [ ] **Step 6: Build + test + commit**

```bash
go build ./...
go test ./... -count=1
git add internal/cli/
git commit -m "cli: rex new — spawn session non-interactively, prompt via positional/stdin"
```

---

### Task B10: `rex rm` and `rex rename` and `rex archive`

(One task because all three are tiny send-intent commands.)

**Files:** Create `internal/cli/rm.go`, `internal/cli/rename.go`, `internal/cli/archive.go`. Delete the three stubs.

- [ ] **Step 1: Delete the three stubs (`RunRm`, `RunRename`, `RunArchive`) from `version.go`.**

- [ ] **Step 2: Create `internal/cli/rm.go`:**

```go
package cli

import (
	"flag"
	"fmt"
	"os"

	"github.com/tristanbietsch/rex/internal/client"
)

// RunRm deletes a session.
func RunRm(args []string) error {
	fs := flag.NewFlagSet("rm", flag.ContinueOnError)
	socket := fs.String("socket", DefaultSocket(), "UDS path")
	force := fs.Bool("force", false, "skip confirmation when stdin is a TTY")
	if err := fs.Parse(args); err != nil {
		return NewExitError(ExitInvalidArgs, err.Error())
	}
	if fs.NArg() != 1 {
		return NewExitError(ExitInvalidArgs, "rm: exactly one selector required")
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

	// Confirm if stdin is a TTY and not --force.
	fi, _ := os.Stdin.Stat()
	if !*force && (fi.Mode()&os.ModeCharDevice) != 0 {
		fmt.Fprintf(os.Stderr, "delete %s (%s)? [y/N] ", sess.ShortID, sess.Slug)
		var ans string
		_, _ = fmt.Fscanln(os.Stdin, &ans)
		if ans != "y" && ans != "Y" && ans != "yes" {
			return NewExitError(ExitGeneric, "aborted")
		}
	}

	if err := c.Delete(sess.ID); err != nil {
		return NewExitError(ExitGeneric, err.Error())
	}
	return nil
}
```

- [ ] **Step 3: Create `internal/cli/rename.go`:**

```go
package cli

import (
	"flag"

	"github.com/tristanbietsch/rex/internal/client"
)

// RunRename changes a session's slug.
func RunRename(args []string) error {
	fs := flag.NewFlagSet("rename", flag.ContinueOnError)
	socket := fs.String("socket", DefaultSocket(), "UDS path")
	if err := fs.Parse(args); err != nil {
		return NewExitError(ExitInvalidArgs, err.Error())
	}
	if fs.NArg() != 2 {
		return NewExitError(ExitInvalidArgs, "rename: <selector> <new-slug>")
	}
	sel := fs.Arg(0)
	newSlug := fs.Arg(1)

	c, err := client.Dial(*socket)
	if err != nil {
		return NewExitError(ExitDaemonUnreachable, err.Error())
	}
	defer c.Close()
	sess, err := ResolveSelector(c, sel)
	if err != nil {
		return err
	}
	if err := c.Rename(sess.ID, newSlug, ""); err != nil {
		return NewExitError(ExitGeneric, err.Error())
	}
	return nil
}
```

- [ ] **Step 4: Create `internal/cli/archive.go`:**

```go
package cli

import (
	"flag"

	"github.com/tristanbietsch/rex/internal/client"
)

// RunArchive moves a completed session out of the active board. Plan B sends Rename with a marker
// title; full archive semantics land when the daemon exposes an archive intent.
func RunArchive(args []string) error {
	fs := flag.NewFlagSet("archive", flag.ContinueOnError)
	socket := fs.String("socket", DefaultSocket(), "UDS path")
	if err := fs.Parse(args); err != nil {
		return NewExitError(ExitInvalidArgs, err.Error())
	}
	if fs.NArg() != 1 {
		return NewExitError(ExitInvalidArgs, "archive: exactly one selector required")
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
	// Plan B: mark archived via Title prefix. Plan C: replace with explicit Archive intent.
	newTitle := "[archived] " + sess.Title
	if err := c.Rename(sess.ID, "", newTitle); err != nil {
		return NewExitError(ExitGeneric, err.Error())
	}
	return nil
}
```

- [ ] **Step 5: Smoke + commit**

```bash
go build ./...
go test ./... -count=1
git add internal/cli/
git commit -m "cli: rex rm / rename / archive — destructive ops with selector resolution"
```

---

### Task B11: `rex reply` and `rex send`

**Files:** Create `internal/cli/reply.go` and `internal/cli/send.go`. Delete the two stubs.

- [ ] **Step 1: Delete `RunReply` and `RunSend` stubs from `version.go`.**

- [ ] **Step 2: `internal/cli/reply.go`:**

```go
package cli

import (
	"flag"
	"io"
	"os"
	"strings"

	"github.com/tristanbietsch/rex/internal/client"
)

// RunReply sends a one-shot text reply (newline-terminated) to a session.
func RunReply(args []string) error {
	fs := flag.NewFlagSet("reply", flag.ContinueOnError)
	socket := fs.String("socket", DefaultSocket(), "UDS path")
	raw := fs.Bool("raw", false, "do not append newline")
	if err := fs.Parse(args); err != nil {
		return NewExitError(ExitInvalidArgs, err.Error())
	}
	if fs.NArg() < 1 {
		return NewExitError(ExitInvalidArgs, "reply: selector required")
	}
	sel := fs.Arg(0)

	var text string
	if fs.NArg() > 1 {
		text = strings.Join(fs.Args()[1:], " ")
	} else {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return NewExitError(ExitGeneric, err.Error())
		}
		text = string(b)
	}

	c, err := client.Dial(*socket)
	if err != nil {
		return NewExitError(ExitDaemonUnreachable, err.Error())
	}
	defer c.Close()
	sess, err := ResolveSelector(c, sel)
	if err != nil {
		return err
	}

	if *raw {
		return c.SendInput(sess.ID, []byte(text))
	}
	return c.Reply(sess.ID, text)
}
```

- [ ] **Step 3: `internal/cli/send.go`:**

```go
package cli

import (
	"flag"
	"io"
	"os"

	"github.com/tristanbietsch/rex/internal/client"
)

// RunSend forwards raw stdin bytes to a session's PTY.
func RunSend(args []string) error {
	fs := flag.NewFlagSet("send", flag.ContinueOnError)
	socket := fs.String("socket", DefaultSocket(), "UDS path")
	if err := fs.Parse(args); err != nil {
		return NewExitError(ExitInvalidArgs, err.Error())
	}
	if fs.NArg() != 1 {
		return NewExitError(ExitInvalidArgs, "send: exactly one selector required")
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

	buf := make([]byte, 4096)
	for {
		n, rerr := os.Stdin.Read(buf)
		if n > 0 {
			if err := c.SendInput(sess.ID, buf[:n]); err != nil {
				return NewExitError(ExitGeneric, err.Error())
			}
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return NewExitError(ExitGeneric, rerr.Error())
		}
	}
}
```

- [ ] **Step 4: Build + commit**

```bash
go build ./...
go test ./... -count=1
git add internal/cli/
git commit -m "cli: rex reply (text+newline) and rex send (raw stdin)"
```

---

## Phase 5 — Daemon-side streaming + attach (3 tasks)

### Task B12: Daemon Subscribe / SendInput / Reply / Rename / FocusFilter handlers

**Files:**
- Modify: `internal/server/client.go`

The Plan A handler only routes Hello, NewSession, Delete. Plan B adds the rest plus per-client subscription state.

- [ ] **Step 1: Locate the switch in `handleClient` and add cases.**

After the `IntentDelete` case, add:

```go
case protocol.IntentSubscribe:
    var p protocol.Subscribe
    if err := json.Unmarshal(env.Data, &p); err != nil {
        writeError(w, env.ID, protocol.ErrCodeBadIntent, err.Error())
        continue
    }
    subscribedSession.Store(p.SessionID)

case protocol.IntentSendInput:
    var p protocol.SendInput
    if err := json.Unmarshal(env.Data, &p); err != nil {
        writeError(w, env.ID, protocol.ErrCodeBadIntent, err.Error())
        continue
    }
    if err := forwardInput(srv, p.SessionID, p.Bytes); err != nil {
        writeError(w, env.ID, protocol.ErrCodeUnknownSession, err.Error())
    }

case protocol.IntentReply:
    var p protocol.Reply
    if err := json.Unmarshal(env.Data, &p); err != nil {
        writeError(w, env.ID, protocol.ErrCodeBadIntent, err.Error())
        continue
    }
    if err := forwardInput(srv, p.SessionID, []byte(p.Text+"\n")); err != nil {
        writeError(w, env.ID, protocol.ErrCodeUnknownSession, err.Error())
    }

case protocol.IntentRename:
    var p protocol.Rename
    if err := json.Unmarshal(env.Data, &p); err != nil {
        writeError(w, env.ID, protocol.ErrCodeBadIntent, err.Error())
        continue
    }
    if err := renameSession(srv, p); err != nil {
        writeError(w, env.ID, protocol.ErrCodeUnknownSession, err.Error())
    }

case protocol.IntentFocusFilter:
    // Cosmetic — silently accept.
    var p protocol.FocusFilter
    _ = json.Unmarshal(env.Data, &p)
```

- [ ] **Step 2: Add subscribedSession to `handleClient`'s state.**

Replace:
```go
subscribed := false
cancel := cfg.Store.Subscribe(func(e state.Event) {
    if !subscribed {
        return
    }
    emitEvent(w, e)
})
```

With:
```go
var subscribed atomic.Bool
var subscribedSession atomic.Value // string
subscribedSession.Store("")
cancel := srv.cfg.Store.Subscribe(func(e state.Event) {
    if !subscribed.Load() {
        return
    }
    emitEvent(w, e)
})
```

(Note: `subscribed` was already changed to `atomic.Bool` in Task 13.)

- [ ] **Step 3: Add forwardInput and renameSession helpers** in the same file:

```go
// pendingInputs maps session id -> pending bytes waiting to be written to PTY.
// Plan B's simplification: stash in the Session via a hooks API. For now, route
// through a package-level mutex-protected map keyed by session id. Plan C may
// promote this to a per-session input channel on the supervisor.

// forwardInput pushes bytes to the session's PTY. Plan B keeps the actual PTY
// handle inside the supervisor goroutine; we use an input channel registered
// on the supervisor at spawn time.

// (See pty/supervisor.go for the channel.)

func forwardInput(srv *Server, sessionID string, b []byte) error {
    ch := srv.InputChannel(sessionID)
    if ch == nil {
        return fmt.Errorf("session %s not running", sessionID)
    }
    select {
    case ch <- b:
        return nil
    default:
        return fmt.Errorf("input buffer full for %s", sessionID)
    }
}

func renameSession(srv *Server, p protocol.Rename) error {
    s, ok := srv.cfg.Store.Get(p.SessionID)
    if !ok {
        return fmt.Errorf("session %s not found", p.SessionID)
    }
    patch := map[string]any{}
    if p.Slug != "" {
        s.Slug = p.Slug
        patch["slug"] = p.Slug
    }
    if p.Title != "" {
        s.Title = p.Title
        patch["title"] = p.Title
    }
    return srv.cfg.Store.UpdateLastLine(p.SessionID, s.LastLine) // triggers an updated event
}
```

(Imports: add `"sync/atomic"` if not already; `"fmt"` if needed.)

- [ ] **Step 4: Add an input channel registry to Server.** In `server.go`:

```go
type Server struct {
    // ... existing fields
    inputMu       sync.Mutex
    inputChannels map[string]chan []byte
}

// RegisterInputChannel attaches a channel for forwarding raw bytes to a session.
// Called by the supervisor at spawn time.
func (s *Server) RegisterInputChannel(sessionID string, ch chan []byte) {
    s.inputMu.Lock()
    defer s.inputMu.Unlock()
    if s.inputChannels == nil {
        s.inputChannels = make(map[string]chan []byte)
    }
    s.inputChannels[sessionID] = ch
}

// UnregisterInputChannel removes the channel after the session exits.
func (s *Server) UnregisterInputChannel(sessionID string) {
    s.inputMu.Lock()
    defer s.inputMu.Unlock()
    delete(s.inputChannels, sessionID)
}

// InputChannel returns the channel for a session, or nil if none registered.
func (s *Server) InputChannel(sessionID string) chan []byte {
    s.inputMu.Lock()
    defer s.inputMu.Unlock()
    return s.inputChannels[sessionID]
}
```

- [ ] **Step 5: Wire the channel into the supervisor spawn.** In `handleNewSession`:

```go
inputCh := make(chan []byte, 16)
srv.RegisterInputChannel(sess.ID, inputCh)

sup := pty.New(pty.SupervisorConfig{
    StateDir:   cfg.StateDir,
    Store:      cfg.Store,
    Command:    cmdArgs,
    CWD:        p.CWD,
    Adapter:    ad,
    InputCh:    inputCh,
    OutputSink: makeOutputSink(srv, sess.ID),
})

go func() {
    defer srv.UnregisterInputChannel(sess.ID)
    defer srv.ReleaseSession()
    _ = sup.Run(ctx, sess)
}()
```

- [ ] **Step 6: Modify `pty.SupervisorConfig` to accept InputCh and wire it.**

In `internal/pty/supervisor.go`:

```go
type SupervisorConfig struct {
    // ... existing fields ...
    InputCh chan []byte // optional: stdin bytes from clients
}
```

In `Run`, after `pty.Start`:

```go
// Input forwarder goroutine.
if s.cfg.InputCh != nil {
    go func() {
        for b := range s.cfg.InputCh {
            _, _ = f.Write(b)
        }
    }()
}
```

(The goroutine exits when InputCh is closed. Caller in server.go should close the channel on UnregisterInputChannel — actually safer to leave open and let the channel be garbage-collected with the supervisor.)

- [ ] **Step 7: Wire OutputSink to broadcast SessionOutput events.**

Add `makeOutputSink` in `server/client.go`:

```go
func makeOutputSink(srv *Server, sessionID string) func([]byte) {
    return func(b []byte) {
        // Broadcast a SessionOutput event to subscribers that are following this session.
        srv.broadcastSessionOutput(sessionID, b)
    }
}
```

And the broadcast on `Server`:

```go
type Server struct {
    // ... existing ...
    outputSubsMu sync.RWMutex
    outputSubs   map[string][]func([]byte) // sessionID -> callbacks
}

// SubscribeSessionOutput registers a per-session output callback.
func (s *Server) SubscribeSessionOutput(sessionID string, fn func([]byte)) func() {
    s.outputSubsMu.Lock()
    defer s.outputSubsMu.Unlock()
    if s.outputSubs == nil {
        s.outputSubs = make(map[string][]func([]byte))
    }
    idx := len(s.outputSubs[sessionID])
    s.outputSubs[sessionID] = append(s.outputSubs[sessionID], fn)
    return func() {
        s.outputSubsMu.Lock()
        defer s.outputSubsMu.Unlock()
        if idx < len(s.outputSubs[sessionID]) {
            s.outputSubs[sessionID][idx] = nil
        }
    }
}

func (s *Server) broadcastSessionOutput(sessionID string, b []byte) {
    s.outputSubsMu.RLock()
    defer s.outputSubsMu.RUnlock()
    for _, fn := range s.outputSubs[sessionID] {
        if fn != nil {
            fn(b)
        }
    }
}
```

In `handleClient`, when Subscribe is received for a specific session id, register a sink that emits `SessionOutput`:

```go
case protocol.IntentSubscribe:
    var p protocol.Subscribe
    if err := json.Unmarshal(env.Data, &p); err != nil {
        writeError(w, env.ID, protocol.ErrCodeBadIntent, err.Error())
        continue
    }
    if p.SessionID != "" {
        outCancel := srv.SubscribeSessionOutput(p.SessionID, func(b []byte) {
            _ = w.WriteEvent(protocol.EventSessionOutput, "", protocol.SessionOutput{
                SessionID: p.SessionID, Bytes: b,
            })
        })
        defer outCancel()
    }
```

(The local `outCancel` deferred inside the for loop runs at function exit — fine for v0.)

- [ ] **Step 8: Build + test**

```bash
go build ./...
go test ./... -count=1 -race
```

Expected: PASS. Existing tests still green.

- [ ] **Step 9: Commit**

```bash
git add internal/server/ internal/pty/
git commit -m "server: handlers for Subscribe/SendInput/Reply/Rename/FocusFilter; per-session output streaming"
```

---

### Task B13: `rex attach` — raw terminal PTY attach

**Files:**
- Create: `internal/cli/attach.go` (delete RunAttach stub)
- Modify: `go.mod` (add golang.org/x/term)

This is the most complex CLI command. It puts the terminal in raw mode, subscribes to a session's output stream, forwards stdout from the daemon to the user's terminal, and forwards stdin from the user's terminal to the session's PTY. Detach via `ctrl+a d`.

- [ ] **Step 1: Add the dep**

```bash
cd /Users/tristan/Documents/personal/dev/rex
go get golang.org/x/term
```

- [ ] **Step 2: Delete the `RunAttach` stub from `version.go`.**

- [ ] **Step 3: Create `internal/cli/attach.go`:**

```go
package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"golang.org/x/term"

	"github.com/tristanbietsch/rex/internal/client"
	"github.com/tristanbietsch/rex/internal/protocol"
)

// detachSequence is ctrl+a followed by d.
var detachSequence = []byte{0x01, 'd'}

// RunAttach connects the user's terminal to a session's PTY.
func RunAttach(args []string) error {
	fs := flag.NewFlagSet("attach", flag.ContinueOnError)
	socket := fs.String("socket", DefaultSocket(), "UDS path")
	readOnly := fs.Bool("read-only", false, "attach as observer (no input forwarding)")
	if err := fs.Parse(args); err != nil {
		return NewExitError(ExitInvalidArgs, err.Error())
	}
	if fs.NArg() != 1 {
		return NewExitError(ExitInvalidArgs, "attach: exactly one selector required")
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

	if err := c.Subscribe(sess.ID); err != nil {
		return NewExitError(ExitGeneric, err.Error())
	}

	// Put terminal in raw mode if stdin is a TTY.
	fd := int(os.Stdin.Fd())
	var oldState *term.State
	if term.IsTerminal(fd) {
		oldState, err = term.MakeRaw(fd)
		if err != nil {
			return NewExitError(ExitGeneric, fmt.Sprintf("raw mode: %v", err))
		}
		defer func() { _ = term.Restore(fd, oldState) }()
	}

	// Print a banner.
	fmt.Fprintf(os.Stderr, "\r\n[attached to %s — ctrl+a d to detach]\r\n", sess.Slug)

	// stdin -> SendInput, watching for detach sequence.
	stdinDone := make(chan struct{})
	if !*readOnly {
		go func() {
			defer close(stdinDone)
			buf := make([]byte, 1024)
			var prev byte
			for {
				n, rerr := os.Stdin.Read(buf)
				if n > 0 {
					chunk := buf[:n]
					if detached := containsDetachSeq(chunk, prev); detached {
						return
					}
					if n > 0 {
						prev = chunk[n-1]
					}
					_ = c.SendInput(sess.ID, chunk)
				}
				if rerr != nil {
					return
				}
			}
		}()
	} else {
		close(stdinDone)
	}

	// daemon events -> stdout.
	for {
		select {
		case <-stdinDone:
			fmt.Fprintf(os.Stderr, "\r\n[detached]\r\n")
			return nil
		default:
		}
		env, err := c.NextEvent()
		if err != nil {
			fmt.Fprintf(os.Stderr, "\r\n[connection closed]\r\n")
			return nil
		}
		switch env.Type {
		case protocol.EventSessionOutput:
			var so protocol.SessionOutput
			if err := json.Unmarshal(env.Data, &so); err == nil {
				if so.SessionID == sess.ID {
					_, _ = os.Stdout.Write(so.Bytes)
				}
			}
		case protocol.EventSessionUpdated:
			// Plan B: ignore. Plan C TUI would render this.
		}
	}
}

func containsDetachSeq(chunk []byte, prevByte byte) bool {
	if len(chunk) == 0 {
		return false
	}
	if prevByte == 0x01 && chunk[0] == 'd' {
		return true
	}
	for i := 0; i+1 < len(chunk); i++ {
		if chunk[i] == 0x01 && chunk[i+1] == 'd' {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Smoke**

```bash
make build-all
DIR=$(mktemp -d)
./rex-daemon --socket $DIR/rex.sock --state-dir $DIR &
DAEMON=$!
sleep 0.5

# Spawn a long-running session
./rex new --socket $DIR/rex.sock --tool echo --model long --slug attach-test --cwd /tmp

# Attach — you should see "step 1 ... step 5 ... done" stream by, then Ctrl+A D to detach
echo "smoke: starting attach. Press ctrl+a d to detach within 10 seconds."
timeout 15 ./rex attach --socket $DIR/rex.sock attach-test || true

kill $DAEMON; wait $DAEMON 2>/dev/null || true
```

Expected: the long script's output streams to the terminal. After ctrl+a d (or the timeout), the attach exits.

- [ ] **Step 5: Build + tests**

```bash
go build ./...
go test ./... -count=1 -race
```

- [ ] **Step 6: Commit**

```bash
git add internal/cli/ go.mod go.sum
git commit -m "cli: rex attach — raw PTY attach with ctrl+a d detach"
```

---

### Task B14: `rex wait`

**Files:** Create `internal/cli/wait.go`. Delete stub.

- [ ] **Step 1: Delete `RunWait` stub.**

- [ ] **Step 2: `internal/cli/wait.go`:**

```go
package cli

import (
	"encoding/json"
	"flag"
	"strings"
	"time"

	"github.com/tristanbietsch/rex/internal/client"
	"github.com/tristanbietsch/rex/internal/protocol"
)

// RunWait blocks until a session reaches a target state (or any terminal state).
func RunWait(args []string) error {
	fs := flag.NewFlagSet("wait", flag.ContinueOnError)
	socket := fs.String("socket", DefaultSocket(), "UDS path")
	until := fs.String("until", "done", "target state: working | needs_input | done | failed | any")
	timeout := fs.Duration("timeout", 0, "max wait duration (0 = no timeout)")
	if err := fs.Parse(args); err != nil {
		return NewExitError(ExitInvalidArgs, err.Error())
	}
	if fs.NArg() != 1 {
		return NewExitError(ExitInvalidArgs, "wait: exactly one selector required")
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

	// If the session is already in the target state, return.
	if matchesState(sess.State, *until) {
		return nil
	}

	deadline := time.Time{}
	if *timeout > 0 {
		deadline = time.Now().Add(*timeout)
	}

	for {
		if !deadline.IsZero() {
			if err := c.SetReadDeadline(deadline); err != nil {
				return NewExitError(ExitGeneric, err.Error())
			}
		}
		env, err := c.NextEvent()
		if err != nil {
			if strings.Contains(err.Error(), "deadline") || strings.Contains(err.Error(), "i/o timeout") {
				return NewExitError(ExitWaitTimedOut, "wait timed out")
			}
			return NewExitError(ExitGeneric, err.Error())
		}
		if env.Type != protocol.EventSessionUpdated {
			continue
		}
		var upd protocol.SessionUpdated
		if err := json.Unmarshal(env.Data, &upd); err != nil {
			continue
		}
		if upd.SessionID != sess.ID {
			continue
		}
		state, _ := upd.Patch["state"].(string)
		if state == "" {
			continue
		}
		if matchesState(protocol.State(state), *until) {
			return nil
		}
	}
}

func matchesState(actual protocol.State, target string) bool {
	if target == "any" {
		switch actual {
		case protocol.StateDone, protocol.StateFailed, protocol.StateCrashed:
			return true
		}
		return false
	}
	return string(actual) == target
}
```

- [ ] **Step 3: Build + commit**

```bash
go build ./...
go test ./... -count=1
git add internal/cli/
git commit -m "cli: rex wait — block until session reaches target state, with optional timeout"
```

---

## Phase 6 — Reload, daemon control, completion (3 tasks)

### Task B15: `rex reload`

**File:** Create `internal/cli/reload.go`. Delete stub.

- [ ] **Step 1: Delete `RunReload` stub.**

- [ ] **Step 2: `internal/cli/reload.go`:**

```go
package cli

import (
	"flag"
	"fmt"
	"os/exec"
	"strings"
)

// RunReload sends SIGHUP to the running rex-daemon.
func RunReload(args []string) error {
	fs := flag.NewFlagSet("reload", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return NewExitError(ExitInvalidArgs, err.Error())
	}
	out, err := exec.Command("pgrep", "rex-daemon").Output()
	if err != nil {
		return NewExitError(ExitDaemonUnreachable, "no rex-daemon process found")
	}
	pid := strings.TrimSpace(strings.Split(string(out), "\n")[0])
	if err := exec.Command("kill", "-HUP", pid).Run(); err != nil {
		return NewExitError(ExitGeneric, fmt.Sprintf("kill -HUP %s: %v", pid, err))
	}
	return nil
}
```

(Note: actually consuming SIGHUP in the daemon to re-read tools.yaml is Plan C work. For Plan B, sending the signal is enough — the daemon will exit cleanly until SIGHUP handling is wired. Document this in the commit message.)

- [ ] **Step 3: Build + commit**

```bash
go build ./...
git add internal/cli/
git commit -m "cli: rex reload — send SIGHUP to rex-daemon (handler lands in Plan C)"
```

---

### Task B16: `rex daemon`

**File:** Create `internal/cli/daemon.go`. Delete stub.

- [ ] **Step 1: Delete `RunDaemon` stub.**

- [ ] **Step 2: `internal/cli/daemon.go`:**

```go
package cli

import (
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// RunDaemon dispatches: rex daemon start | stop | status | restart | logs
func RunDaemon(args []string) error {
	if len(args) == 0 {
		return NewExitError(ExitInvalidArgs, "daemon: subcommand required (start|stop|status|restart|logs)")
	}
	switch args[0] {
	case "start":
		return daemonStart(args[1:])
	case "stop":
		return daemonStop(args[1:])
	case "status":
		return daemonStatus(args[1:])
	case "restart":
		return daemonRestart(args[1:])
	case "logs":
		return daemonLogs(args[1:])
	default:
		return NewExitError(ExitInvalidArgs, fmt.Sprintf("daemon: unknown subcommand %q", args[0]))
	}
}

func daemonStart(args []string) error {
	socket := DefaultSocket()
	// If already up, no-op.
	if conn, err := net.DialTimeout("unix", socket, 100*time.Millisecond); err == nil {
		_ = conn.Close()
		fmt.Println("rex-daemon already running")
		return nil
	}
	// Start rex-daemon in background.
	cmd := exec.Command("rex-daemon")
	cmd.Stdout = nil
	cmd.Stderr, _ = os.OpenFile(daemonLogPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err := cmd.Start(); err != nil {
		return NewExitError(ExitGeneric, fmt.Sprintf("start: %v", err))
	}
	// Wait up to 2s for socket.
	for i := 0; i < 100; i++ {
		if conn, err := net.Dial("unix", socket); err == nil {
			_ = conn.Close()
			fmt.Printf("rex-daemon started (pid %d)\n", cmd.Process.Pid)
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return NewExitError(ExitDaemonUnreachable, "daemon started but socket didn't appear")
}

func daemonStop(args []string) error {
	out, err := exec.Command("pgrep", "rex-daemon").Output()
	if err != nil {
		return NewExitError(ExitDaemonUnreachable, "no rex-daemon process found")
	}
	pid := strings.TrimSpace(strings.Split(string(out), "\n")[0])
	if err := exec.Command("kill", "-TERM", pid).Run(); err != nil {
		return NewExitError(ExitGeneric, err.Error())
	}
	fmt.Printf("sent SIGTERM to pid %s\n", pid)
	return nil
}

func daemonStatus(args []string) error {
	socket := DefaultSocket()
	if conn, err := net.DialTimeout("unix", socket, 200*time.Millisecond); err == nil {
		_ = conn.Close()
		out, _ := exec.Command("pgrep", "rex-daemon").Output()
		pid := strings.TrimSpace(string(out))
		fmt.Printf("running · socket=%s · pid=%s\n", socket, pid)
		return nil
	}
	fmt.Printf("not running · socket=%s\n", socket)
	return NewExitError(ExitDaemonUnreachable, "")
}

func daemonRestart(args []string) error {
	if err := daemonStop(args); err != nil {
		// Continue — maybe already stopped.
		fmt.Fprintln(os.Stderr, "stop:", err)
	}
	time.Sleep(300 * time.Millisecond)
	return daemonStart(args)
}

func daemonLogs(args []string) error {
	fs := flag.NewFlagSet("daemon logs", flag.ContinueOnError)
	follow := fs.Bool("f", false, "follow")
	if err := fs.Parse(args); err != nil {
		return NewExitError(ExitInvalidArgs, err.Error())
	}
	path := daemonLogPath()
	cmd := exec.Command("tail")
	if *follow {
		cmd = exec.Command("tail", "-f", path)
	} else {
		cmd = exec.Command("tail", "-n", "200", path)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func daemonLogPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "rex", "daemon.log")
}
```

- [ ] **Step 3: Build + commit**

```bash
go build ./...
git add internal/cli/
git commit -m "cli: rex daemon start|stop|restart|status|logs"
```

---

### Task B17: `rex completion`

**File:** Create `internal/cli/completion.go`. Delete stub.

- [ ] **Step 1: Delete `RunCompletion` stub.**

- [ ] **Step 2: `internal/cli/completion.go`:**

```go
package cli

import (
	"flag"
	"fmt"
)

// RunCompletion prints a shell completion script.
func RunCompletion(args []string) error {
	fs := flag.NewFlagSet("completion", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return NewExitError(ExitInvalidArgs, err.Error())
	}
	if fs.NArg() != 1 {
		return NewExitError(ExitInvalidArgs, "completion: shell required (bash|zsh|fish)")
	}
	switch fs.Arg(0) {
	case "bash":
		fmt.Print(bashCompletion)
	case "zsh":
		fmt.Print(zshCompletion)
	case "fish":
		fmt.Print(fishCompletion)
	default:
		return NewExitError(ExitInvalidArgs, "supported shells: bash, zsh, fish")
	}
	return nil
}

const bashCompletion = `# rex bash completion. Source this from your ~/.bashrc:
#   source <(rex completion bash)
_rex_complete() {
    local cur="${COMP_WORDS[COMP_CWORD]}"
    local verbs="status ls new attach reply send log wait rm rename archive reload daemon completion --version"
    COMPREPLY=( $(compgen -W "$verbs" -- "$cur") )
}
complete -F _rex_complete rex
`

const zshCompletion = `# rex zsh completion. Source this from your ~/.zshrc:
#   source <(rex completion zsh)
_rex() {
    local -a verbs
    verbs=(status ls new attach reply send log wait rm rename archive reload daemon completion --version)
    _describe 'verb' verbs
}
compdef _rex rex
`

const fishCompletion = `# rex fish completion. Source this:
#   rex completion fish | source
complete -c rex -f
complete -c rex -n "__fish_use_subcommand" -a "status ls new attach reply send log wait rm rename archive reload daemon completion --version"
`
```

- [ ] **Step 3: Build + commit**

```bash
go build ./...
./rex completion bash | head -3
git add internal/cli/
git commit -m "cli: rex completion bash|zsh|fish — emit shell completion scripts"
```

---

## Phase 7 — ClaudeStructured adapter + real tools (2 tasks)

### Task B18: `ClaudeStructured` adapter

**Files:** Create `internal/adapter/claude.go` + `internal/adapter/claude_test.go`.

- [ ] **Step 1: Create `internal/adapter/claude.go`:**

```go
package adapter

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/tristanbietsch/rex/internal/protocol"
)

// ClaudeStructured parses claude-code's stream-json output.
//
// Claude emits one JSON object per line. We watch for specific message types
// and translate them to session state transitions:
//   - "system":      ignored (init messages)
//   - "assistant":   state = working (regardless of content)
//   - "user":        state = working (tool result)
//   - "result":      state = done (final summary)
//
// Anything left buffering after an idle period without a recent "result" is
// classified as needs_input (Claude is waiting on a permission or human reply).
type ClaudeStructured struct {
	mu       sync.Mutex
	last     protocol.State
	buffer   []byte
	lastSeen time.Time
}

// NewClaudeStructured returns a fresh adapter.
func NewClaudeStructured() *ClaudeStructured {
	return &ClaudeStructured{last: protocol.StateWorking, lastSeen: time.Now()}
}

// Detect implements Adapter.
func (a *ClaudeStructured) Detect(window []byte, idle time.Duration) protocol.State {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Append window to buffer (window represents the most recent PTY output).
	a.buffer = append(a.buffer, window...)
	if len(a.buffer) > 64*1024 {
		// Trim the oldest half to bound memory.
		a.buffer = a.buffer[32*1024:]
	}

	// Parse complete lines from the buffer.
	for {
		nl := indexNewline(a.buffer)
		if nl < 0 {
			break
		}
		line := a.buffer[:nl]
		a.buffer = a.buffer[nl+1:]
		if len(line) == 0 {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal(line, &obj); err != nil {
			continue
		}
		a.lastSeen = time.Now()
		switch obj["type"] {
		case "assistant", "user":
			a.last = protocol.StateWorking
		case "result":
			a.last = protocol.StateDone
		}
	}

	// If we haven't seen a structured event for the idle duration and the
	// last state is `working`, assume Claude is waiting on input/permission.
	if a.last == protocol.StateWorking && idle > 0 && time.Since(a.lastSeen) > 5*time.Second {
		return protocol.StateNeedsInput
	}
	return a.last
}

func indexNewline(b []byte) int {
	for i, c := range b {
		if c == '\n' {
			return i
		}
	}
	return -1
}

// Ensure unused import warnings are silenced if strings ever needed.
var _ = strings.TrimSpace
```

- [ ] **Step 2: Test** at `internal/adapter/claude_test.go`:

```go
package adapter

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/protocol"
)

func TestClaudeStructured_AssistantMessage(t *testing.T) {
	a := NewClaudeStructured()
	line := []byte(`{"type":"assistant","message":{"content":"hello"}}` + "\n")
	got := a.Detect(line, 100*time.Millisecond)
	require.Equal(t, protocol.StateWorking, got)
}

func TestClaudeStructured_ResultMessage(t *testing.T) {
	a := NewClaudeStructured()
	line := []byte(`{"type":"result","subtype":"success"}` + "\n")
	got := a.Detect(line, 100*time.Millisecond)
	require.Equal(t, protocol.StateDone, got)
}

func TestClaudeStructured_IdleFallback(t *testing.T) {
	a := NewClaudeStructured()
	// Seed with an assistant message.
	a.Detect([]byte(`{"type":"assistant"}`+"\n"), 100*time.Millisecond)
	// Force lastSeen back so we're "idle".
	a.lastSeen = time.Now().Add(-6 * time.Second)
	got := a.Detect(nil, time.Second)
	require.Equal(t, protocol.StateNeedsInput, got)
}
```

- [ ] **Step 3: Build + test + commit**

```bash
go build ./...
go test ./... -count=1
git add internal/adapter/
git commit -m "adapter: ClaudeStructured — parses claude stream-json, classifies via message types"
```

---

### Task B19: Real tool registry

**File:** Modify `internal/registry/builtin.yaml`. Modify `internal/registry/types.go` to add `EnabledByDefault`.

- [ ] **Step 1: Add `EnabledByDefault` to types.go**

In `internal/registry/types.go`, update the `Tool` struct:

```go
type Tool struct {
    ID               string   `yaml:"id"`
    Name             string   `yaml:"name"`
    Category         string   `yaml:"category"`
    Command          []string `yaml:"command"`
    CWDStrategy      string   `yaml:"cwd_strategy,omitempty"`
    Detect           Detect   `yaml:"detect"`
    Icon             string   `yaml:"icon"`
    Color            string   `yaml:"color"`
    EnabledByDefault *bool    `yaml:"enabled_by_default,omitempty"`
    Models           []Model  `yaml:"models"`
}
```

(Pointer to distinguish "absent" from "explicitly false". Absent = enabled by default.)

- [ ] **Step 2: Replace `internal/registry/builtin.yaml`** with the production registry (keeping `echo` as a test tool):

```yaml
# Built-in tools shipped with rex-daemon.
#
# Tools with `enabled_by_default: false` are opt-in: they're available but the
# wizard hides them until the user enables them in Settings → Onboarding.
tools:
  # Test tool — used by integration tests and the smoke recipe.
  - id: echo
    name: "Echo (test)"
    category: self_hosted
    command: ["bash", "-c"]
    detect:
      kind: heuristic
      prompt_regex: "(?m)^awaiting input:"
      idle_ms: 800
    icon: "◉"
    color: "#94A3B8"
    models:
      - id: short
        name: "Short script"
        args: ["echo 'hello from rex'; sleep 1; echo 'work in progress'; sleep 1; echo 'done'"]
      - id: prompt
        name: "Waits for input"
        args: ["echo 'awaiting input:'; read line; echo \"got: $line\"; echo 'done'"]
      - id: long
        name: "Longer script"
        args: ["for i in $(seq 1 5); do echo \"step $i\"; sleep 1; done; echo 'done'"]

  - id: claude
    name: "Claude Code"
    category: paid
    command: ["claude", "--output-format=stream-json", "--verbose"]
    detect:
      kind: structured
      format: claude_jsonl
    icon: "◆"
    color: "#D97757"
    models:
      - id: opus
        name: "Opus 4.7"
        args: ["--model", "opus"]
        effort:
          options: [minimal, default, high, max]
          default: default
          arg_template: "--effort={value}"
      - id: sonnet
        name: "Sonnet 4.6"
        args: ["--model", "sonnet"]
        effort:
          options: [minimal, default, high, max]
          default: default
          arg_template: "--effort={value}"
      - id: haiku
        name: "Haiku 4.5"
        args: ["--model", "haiku"]
        effort:
          options: [minimal, default]
          default: default
          arg_template: "--effort={value}"

  - id: codex
    name: "OpenAI Codex"
    category: paid
    command: ["codex"]
    detect:
      kind: heuristic
      prompt_regex: "(?m)^❯ "
      idle_ms: 1200
    icon: "◇"
    color: "#10A37F"
    models:
      - id: gpt-5
        name: "GPT-5"
        args: ["--model", "gpt-5"]
        effort:
          options: [low, medium, high]
          default: medium
          arg_template: "--reasoning-effort={value}"
      - id: gpt-5-codex
        name: "GPT-5 Codex"
        args: ["--model", "gpt-5-codex"]
        effort:
          options: [low, medium, high]
          default: medium
          arg_template: "--reasoning-effort={value}"

  - id: gemini
    name: "Gemini CLI"
    category: paid
    command: ["gemini"]
    detect:
      kind: heuristic
      prompt_regex: "(?m)^> "
      idle_ms: 1200
    icon: "◈"
    color: "#4285F4"
    models:
      - id: 2.5-pro
        name: "Gemini 2.5 Pro"
        args: ["--model", "gemini-2.5-pro"]
      - id: 2.5-flash
        name: "Gemini 2.5 Flash"
        args: ["--model", "gemini-2.5-flash"]

  - id: ollama
    name: "Ollama (local)"
    category: self_hosted
    command: ["ollama", "run"]
    detect:
      kind: heuristic
      prompt_regex: "(?m)>>> $"
      idle_ms: 1500
    icon: "◉"
    color: "#B8A382"
    models:
      - id: llama3.1
        name: "llama3.1"
        args: ["llama3.1"]
      - id: mistral
        name: "mistral"
        args: ["mistral"]

  - id: grok
    name: "Grok (xAI)"
    category: paid
    command: ["grok"]
    detect:
      kind: heuristic
      prompt_regex: "(?m)^> $"
      idle_ms: 1200
    icon: "⬢"
    color: "#E5544D"
    enabled_by_default: false
    models:
      - id: grok-4
        name: "Grok 4"
        args: ["--model", "grok-4"]
        effort:
          options: [low, medium, high]
          default: medium
          arg_template: "--reasoning-effort={value}"
      - id: grok-4-mini
        name: "Grok 4 Mini"
        args: ["--model", "grok-4-mini"]

  - id: deepseek
    name: "DeepSeek"
    category: paid
    command: ["deepseek"]
    detect:
      kind: heuristic
      prompt_regex: "(?m)^> $"
      idle_ms: 1200
    icon: "⊙"
    color: "#5E72E4"
    enabled_by_default: false
    models:
      - id: deepseek-chat
        name: "DeepSeek Chat"
        args: ["--model", "deepseek-chat"]
      - id: deepseek-coder
        name: "DeepSeek Coder"
        args: ["--model", "deepseek-coder"]
      - id: deepseek-reasoner
        name: "DeepSeek Reasoner"
        args: ["--model", "deepseek-reasoner"]
        effort:
          options: [low, medium, high]
          default: medium
          arg_template: "--effort={value}"

  - id: kimi
    name: "Kimi (Moonshot)"
    category: paid
    command: ["kimi"]
    detect:
      kind: heuristic
      prompt_regex: "(?m)^> $"
      idle_ms: 1200
    icon: "◗"
    color: "#7C3AED"
    enabled_by_default: false
    models:
      - id: kimi-k2
        name: "Kimi K2"
        args: ["--model", "kimi-k2"]
      - id: kimi-k2-mini
        name: "Kimi K2 Mini"
        args: ["--model", "kimi-k2-mini"]
```

- [ ] **Step 3: Update the loader test in `internal/registry/loader_test.go`**

Find `TestLoad_BuiltinOnly` and update the assertion for echo:

```go
echo, ok := reg.Find("echo")
require.True(t, ok)
require.Equal(t, "Echo (test)", echo.Name)
require.Len(t, echo.Models, 3)
```

Add a new test:

```go
func TestLoad_RealToolsPresent(t *testing.T) {
	reg, err := Load("")
	require.NoError(t, err)
	for _, id := range []string{"claude", "codex", "gemini", "ollama", "grok", "deepseek", "kimi"} {
		_, ok := reg.Find(id)
		require.True(t, ok, "tool %s missing", id)
	}
}

func TestLoad_OptInDefaults(t *testing.T) {
	reg, err := Load("")
	require.NoError(t, err)
	for _, id := range []string{"grok", "deepseek", "kimi"} {
		t, ok := reg.Find(id)
		require.True(t, ok)
		require.NotNil(t, t.EnabledByDefault, "tool %s should have explicit enabled_by_default", id)
		require.False(t, *t.EnabledByDefault, "tool %s should be opt-in", id)
	}
}
```

(The `t` shadowing in the loop is intentional but renames `t` (the test) — fix by renaming the loop variable to `tool`.)

Correction:

```go
func TestLoad_OptInDefaults(t *testing.T) {
	reg, err := Load("")
	require.NoError(t, err)
	for _, id := range []string{"grok", "deepseek", "kimi"} {
		tool, ok := reg.Find(id)
		require.True(t, ok)
		require.NotNil(t, tool.EnabledByDefault, "tool %s should have explicit enabled_by_default", id)
		require.False(t, *tool.EnabledByDefault, "tool %s should be opt-in", id)
	}
}
```

- [ ] **Step 4: Build + test + commit**

```bash
go build ./...
go test ./... -count=1
git add internal/registry/
git commit -m "registry: real tool set (claude/codex/gemini/ollama + opt-in grok/deepseek/kimi)"
```

---

## Phase 8 — Acceptance (1 task)

### Task B20: Plan B smoke test recipe + acceptance doc

**File:** Create `docs/superpowers/plans/2026-05-14-rex-cli-smoke.md`.

- [ ] **Step 1: Create the doc with this content:**

````markdown
# Plan B smoke test

Ten-minute manual check that the CLI is alive and behaves.

## Setup

```sh
make build-all
rm -f /tmp/rex.sock; rm -rf /tmp/rex-state
./rex-daemon --socket /tmp/rex.sock --state-dir /tmp/rex-state &
DAEMON=$!
sleep 0.5
export REX_SOCKET=/tmp/rex.sock
```

## 1. Status (no sessions)

```sh
./rex status --socket $REX_SOCKET
```

Expect `0 awaiting input · 0 working · 0 completed`. Exit code 0.

## 2. Spawn + watch + log

```sh
./rex new --socket $REX_SOCKET --tool echo --model short --slug demo --cwd /tmp
./rex ls --socket $REX_SOCKET
sleep 4
./rex ls --socket $REX_SOCKET
./rex log --socket $REX_SOCKET --state-dir /tmp/rex-state demo
```

Expect `rex new` to print `<short-id>\tdemo`. After 4s, `rex ls` shows the demo session in `done` state. `rex log` prints the transcript.

## 3. Attach to a long session

```sh
./rex new --socket $REX_SOCKET --tool echo --model long --slug attach-me --cwd /tmp
sleep 1
timeout 10 ./rex attach --socket $REX_SOCKET attach-me || true
```

You should see `step 1` through `step 5` stream by. Press ctrl+a d to detach early.

## 4. Reply to a waiting session

```sh
./rex new --socket $REX_SOCKET --tool echo --model prompt --slug ask --cwd /tmp
sleep 2
./rex ls --socket $REX_SOCKET --state needs_input
./rex reply --socket $REX_SOCKET ask "hello back"
sleep 1
./rex log --socket $REX_SOCKET --state-dir /tmp/rex-state ask | tail -5
```

Expect to see "got: hello back" in the transcript.

## 5. Wait

```sh
./rex new --socket $REX_SOCKET --tool echo --model long --slug waiter --cwd /tmp
./rex wait --socket $REX_SOCKET waiter --until done --timeout 30s
echo "exit code: $?"
```

Exit code 0 after ~5 seconds.

## 6. Concurrency cap

```sh
# Restart with cap of 1
kill $DAEMON; wait $DAEMON 2>/dev/null
./rex-daemon --socket /tmp/rex.sock --state-dir /tmp/rex-state --max-concurrent-sessions 1 &
DAEMON=$!
sleep 0.5

./rex new --socket $REX_SOCKET --tool echo --model long --slug cap-1 --cwd /tmp
./rex new --socket $REX_SOCKET --tool echo --model long --slug cap-2 --cwd /tmp
# Second one should print a failure (the session won't appear in ls).
./rex ls --socket $REX_SOCKET
```

## 7. Cleanup

```sh
./rex daemon stop
```

## Acceptance

Plan B is **done** when steps 1–7 succeed end-to-end and `make test` is green with the race detector clean.
````

- [ ] **Step 2: Commit**

```bash
git add docs/superpowers/plans/
git commit -m "docs: Plan B smoke test recipe"
```

---

## Acceptance criteria

Plan B is complete when:

1. `make test` is green (all packages including new client and cli).
2. `make lint` is green.
3. `make build` produces both `rex` and `rex-daemon` binaries.
4. The smoke test recipe at `docs/superpowers/plans/2026-05-14-rex-cli-smoke.md` succeeds end-to-end.
5. `rex attach <slug>` connects to a running session and detaches cleanly on ctrl+a d.
6. The opt-in providers (grok, deepseek, kimi) are present in the registry but flagged `enabled_by_default: false`.
7. `ClaudeStructured` adapter passes its unit tests.

## Self-review notes

- **Spec coverage**: Every Plan A follow-up is closed in Phase 1. Every CLI verb documented in `docs/cli.md` has an implementation. The real tool registry is present. `ClaudeStructured` adapter ships.
- **Deferred to Plan C**: TUI itself; Lua / audio / animations / settings page; full `rex slash` palette (we just have completion); `rex config` is dropped from Plan B (settings registry lands in Plan C).
- **Known limitation**: `rex reload` sends SIGHUP but the daemon doesn't yet handle it (Plan C). Documented in the commit message and in `cli.md` if needed.
- **Type consistency**: `EnabledByDefault *bool` (pointer) chosen so YAML absence reads as nil (enabled) vs explicit `false` (opt-in).
- **Commit hygiene**: Per repo convention, **never** add `Co-Authored-By: Claude ...` trailers to commit messages.
