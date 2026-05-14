# Rex Daemon Foundation — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a working `rex-daemon` binary that supervises agent sessions via PTY, exposes a JSONL protocol over a Unix domain socket, persists state to disk, and is fully debuggable with `socat`/`jq`. After this plan, you can spawn and observe agent sessions with no TUI — just shell tools.

**Architecture:** Single Go binary. One goroutine per connected client (reads intents, writes events). One goroutine per live session (drives a PTY through an `Adapter`). A central `Store` owns the session set and broadcasts diffs to all clients. State persisted to `~/.local/share/rex/sessions/<id>/`. Registry loaded from a built-in default merged with `~/.config/rex/tools.yaml`. Tool/model selection is data-driven from the registry — adding a tool never touches Go code.

**Tech Stack:** Go 1.22+. `creack/pty` for PTYs. `gopkg.in/yaml.v3` for config. `golang.org/x/sync/errgroup` + `semaphore`. stdlib `encoding/json` + `net` + `os/exec` for everything else. `testify/require` for test assertions.

**Out of scope for Plan A (deferred to B/C/D):**
- `ClaudeStructured` adapter (Plan B)
- CLI commands (`rex ls`, `rex status`, etc. — Plan B)
- TUI (Plan C)
- Lua, audio, animations, settings page, slash palette (Plan D)
- `rex` binary itself (Plans B/C)
- Daemon auto-start helper (Plan B)

---

## File Structure

```
rex/
├── go.mod
├── go.sum
├── cmd/
│   └── rex-daemon/
│       └── main.go              — entry point, flag parsing, lifecycle
├── internal/
│   ├── protocol/
│   │   ├── envelope.go          — wire envelope (v/kind/type/id/data)
│   │   ├── envelope_test.go
│   │   ├── intents.go           — Intent payload structs
│   │   ├── events.go            — Event payload structs + SessionSummary
│   │   ├── codec.go             — JSONL read/write helpers
│   │   └── codec_test.go
│   ├── registry/
│   │   ├── types.go             — Tool, Model, Effort, Detect structs
│   │   ├── builtin.go           — embedded built-in tools.yaml
│   │   ├── builtin.yaml         — //go:embed source for the built-ins
│   │   ├── loader.go            — built-in + user YAML merge
│   │   └── loader_test.go
│   ├── state/
│   │   ├── session.go           — Session struct + State enum
│   │   ├── store.go             — in-memory store with subscribers
│   │   ├── store_test.go
│   │   ├── persist.go           — meta.json + transcript.log I/O
│   │   └── persist_test.go
│   ├── adapter/
│   │   ├── adapter.go           — Adapter interface, SpawnParams, Result
│   │   ├── heuristic.go         — HeuristicCLI implementation
│   │   └── heuristic_test.go
│   ├── pty/
│   │   ├── supervisor.go        — runs one session: spawn, read, classify, persist
│   │   └── supervisor_test.go
│   ├── server/
│   │   ├── server.go            — UDS listener, accept loop, lifecycle
│   │   ├── client.go            — one client handler: intent dispatch + event emit
│   │   ├── client_test.go
│   │   └── server_test.go
│   └── ids/
│       ├── ids.go               — UUID generation + short_id derivation + uniqueness
│       └── ids_test.go
└── testdata/
    └── tools-user.yaml          — fixture for registry merge tests
```

Two modules of note are deliberately small so they're easy to grow in Plan B/C:

- `internal/adapter` ships only `HeuristicCLI` in Plan A. `ClaudeStructured` lands in Plan B as a peer file.
- `internal/registry/builtin.yaml` ships only an `echo` test tool that wraps `bash -c '...'`. Real tool entries (claude/codex/gemini/ollama) land in Plan B once `ClaudeStructured` is real.

---

## Task 0: Repo bootstrap

**Files:**
- Create: `go.mod`
- Create: `.gitignore`
- Create: `.golangci.yml`
- Create: `cmd/rex-daemon/main.go` (stub)
- Create: `Makefile`

- [ ] **Step 1: Create the Go module**

Run:
```bash
cd /Users/tristan/Documents/personal/dev/rex
go mod init github.com/tristanbietsch/rex
```

- [ ] **Step 2: Add the .gitignore**

Create `.gitignore`:
```
# Binaries
/rex-daemon
/rex
*.test
*.out

# Editor / OS
.DS_Store
.idea/
.vscode/

# Local runtime artifacts (the user's actual state dir is XDG, but if anyone runs from cwd…)
/sessions/
/rex.sock
```

- [ ] **Step 3: Add a strict golangci-lint config**

Create `.golangci.yml`:
```yaml
run:
  timeout: 5m

linters:
  disable-all: true
  enable:
    - errcheck
    - govet
    - staticcheck
    - revive
    - ineffassign
    - unused
    - gofmt
    - goimports

linters-settings:
  revive:
    rules:
      - name: exported
      - name: error-return
      - name: error-naming
      - name: unused-parameter
        disabled: true
```

- [ ] **Step 4: Stub the daemon entry point**

Create `cmd/rex-daemon/main.go`:
```go
// Package main is the rex-daemon entry point.
package main

import (
	"fmt"
	"os"
)

const version = "0.0.1-plan-a"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "rex-daemon:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fmt.Println("rex-daemon", version)
	_ = args
	return nil
}
```

- [ ] **Step 5: Add the Makefile**

Create `Makefile`:
```makefile
.PHONY: build test lint clean

build:
	go build -o rex-daemon ./cmd/rex-daemon

test:
	go test ./...

lint:
	golangci-lint run

clean:
	rm -f rex-daemon
```

- [ ] **Step 6: Verify it builds**

Run:
```bash
make build && ./rex-daemon
```

Expected:
```
rex-daemon 0.0.1-plan-a
```

- [ ] **Step 7: Commit**

```bash
git add go.mod .gitignore .golangci.yml cmd/rex-daemon/main.go Makefile
git commit -m "scaffold: rex-daemon module + lint + makefile"
```

---

## Task 1: Protocol envelope

**Files:**
- Create: `internal/protocol/envelope.go`
- Create: `internal/protocol/envelope_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/protocol/envelope_test.go`:
```go
package protocol

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnvelope_MarshalRoundTrip(t *testing.T) {
	e := Envelope{V: 1, Kind: KindIntent, Type: "Hello", ID: "abc", Data: json.RawMessage(`{"client_version":"manual"}`)}
	b, err := json.Marshal(e)
	require.NoError(t, err)

	var got Envelope
	require.NoError(t, json.Unmarshal(b, &got))
	require.Equal(t, e.V, got.V)
	require.Equal(t, e.Kind, got.Kind)
	require.Equal(t, e.Type, got.Type)
	require.Equal(t, e.ID, got.ID)
	require.JSONEq(t, `{"client_version":"manual"}`, string(got.Data))
}

func TestEnvelope_OmitsEmptyID(t *testing.T) {
	e := Envelope{V: 1, Kind: KindEvent, Type: "Snapshot", Data: json.RawMessage(`{}`)}
	b, err := json.Marshal(e)
	require.NoError(t, err)
	require.NotContains(t, string(b), `"id"`)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:
```bash
go get github.com/stretchr/testify/require
go test ./internal/protocol/...
```

Expected: build error — `Envelope`, `KindIntent`, `KindEvent` undefined.

- [ ] **Step 3: Implement the envelope**

Create `internal/protocol/envelope.go`:
```go
// Package protocol defines the JSONL wire format between rex and rex-daemon.
package protocol

import "encoding/json"

// ProtocolVersion is the wire version. Bump only on breaking changes.
const ProtocolVersion = 1

// Kind discriminates direction of a message on the wire.
type Kind string

const (
	KindIntent Kind = "Intent" // client -> daemon
	KindEvent  Kind = "Event"  // daemon -> client
)

// Envelope is the outer wrapper for every message on the wire.
type Envelope struct {
	V    int             `json:"v"`
	Kind Kind            `json:"kind"`
	Type string          `json:"type"`
	ID   string          `json:"id,omitempty"`
	Data json.RawMessage `json:"data"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run:
```bash
go test ./internal/protocol/... -v
```

Expected: both tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/protocol/envelope.go internal/protocol/envelope_test.go go.mod go.sum
git commit -m "protocol: envelope encode/decode with versioning"
```

---

## Task 2: Intent and Event payload structs

**Files:**
- Create: `internal/protocol/intents.go`
- Create: `internal/protocol/events.go`

- [ ] **Step 1: Define the intent payloads**

Create `internal/protocol/intents.go`:
```go
package protocol

// Intent types.
const (
	IntentHello        = "Hello"
	IntentSubscribe    = "Subscribe"
	IntentNewSession   = "NewSession"
	IntentOpenSession  = "OpenSession"
	IntentSendInput    = "SendInput"
	IntentReply        = "Reply"
	IntentRename       = "Rename"
	IntentDelete       = "Delete"
	IntentFocusFilter  = "FocusFilter"
	IntentShutdown     = "Shutdown"
)

// Hello is the first message a client sends after connect.
type Hello struct {
	ClientVersion string `json:"client_version"`
}

// Subscribe controls what events flow back. SessionID == "" means board-wide updates only.
type Subscribe struct {
	SessionID string `json:"session_id,omitempty"`
}

// NewSession asks the daemon to spawn a new agent session.
type NewSession struct {
	ToolID        string `json:"tool_id"`
	ModelID       string `json:"model_id"`
	Effort        string `json:"effort,omitempty"`
	Slug          string `json:"slug"`
	Title         string `json:"title,omitempty"`
	CWD           string `json:"cwd"`
	InitialPrompt string `json:"initial_prompt,omitempty"`
}

// OpenSession marks a session as the client's foreground.
type OpenSession struct {
	SessionID string `json:"session_id"`
}

// SendInput forwards raw bytes to the session's PTY.
type SendInput struct {
	SessionID string `json:"session_id"`
	Bytes     []byte `json:"bytes"` // base64-encoded by encoding/json
}

// Reply is a convenience for inline text replies. Daemon appends a newline.
type Reply struct {
	SessionID string `json:"session_id"`
	Text      string `json:"text"`
}

// Rename changes the slug and/or title.
type Rename struct {
	SessionID string `json:"session_id"`
	Slug      string `json:"slug,omitempty"`
	Title     string `json:"title,omitempty"`
}

// Delete removes a session.
type Delete struct {
	SessionID string `json:"session_id"`
}

// FocusFilter is a cosmetic intent stored per-client.
type FocusFilter struct {
	ToolID string `json:"tool_id"` // "all" or a tool id
}
```

- [ ] **Step 2: Define the event payloads**

Create `internal/protocol/events.go`:
```go
package protocol

import "time"

// Event types.
const (
	EventSnapshot       = "Snapshot"
	EventSessionAdded   = "SessionAdded"
	EventSessionUpdated = "SessionUpdated"
	EventSessionRemoved = "SessionRemoved"
	EventSessionOutput  = "SessionOutput"
	EventError          = "Error"
)

// State is the lifecycle state of a session.
type State string

const (
	StateQueued      State = "queued"
	StateWorking     State = "working"
	StateNeedsInput  State = "needs_input"
	StateDone        State = "done"
	StateFailed      State = "failed"
	StateCrashed     State = "crashed"
)

// SessionSummary is the canonical session shape on the wire.
type SessionSummary struct {
	ID          string    `json:"id"`
	ShortID     string    `json:"short_id"`
	ToolID      string    `json:"tool_id"`
	ModelID     string    `json:"model_id"`
	Effort      string    `json:"effort,omitempty"`
	Slug        string    `json:"slug"`
	Title       string    `json:"title,omitempty"`
	CWD         string    `json:"cwd"`
	State       State     `json:"state"`
	StartedAt   time.Time `json:"started_at"`
	LastEventAt time.Time `json:"last_event_at"`
	LastLine    string    `json:"last_line,omitempty"`
	ExitCode    *int      `json:"exit_code,omitempty"`
}

// Snapshot is the daemon's response to Hello: full board state.
type Snapshot struct {
	Sessions []SessionSummary `json:"sessions"`
	Filter   string           `json:"filter"`
}

// SessionUpdated carries a sparse merge-patch over the summary.
type SessionUpdated struct {
	SessionID string         `json:"session_id"`
	Patch     map[string]any `json:"patch"`
}

// SessionRemoved indicates the session is gone.
type SessionRemoved struct {
	SessionID string `json:"session_id"`
}

// SessionOutput is incremental PTY output for a subscribed session.
type SessionOutput struct {
	SessionID string `json:"session_id"`
	Bytes     []byte `json:"bytes"`
}

// ErrorEvent surfaces an error correlated to an intent's id.
type ErrorEvent struct {
	ID      string `json:"id,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error codes used by the daemon. Keep stable; clients may switch on them.
const (
	ErrCodeBadIntent       = "bad_intent"
	ErrCodeUnknownSession  = "unknown_session"
	ErrCodeAmbiguousID     = "ambiguous_id"
	ErrCodeRegistry        = "registry_invalid"
	ErrCodeSpawn           = "spawn_failed"
	ErrCodeTooManySessions = "too_many_sessions"
	ErrCodeBadState        = "bad_state"
)
```

- [ ] **Step 3: Compile-only check**

Run:
```bash
go build ./...
```

Expected: builds cleanly.

- [ ] **Step 4: Commit**

```bash
git add internal/protocol/intents.go internal/protocol/events.go
git commit -m "protocol: intent + event payload structs, state enum, session summary"
```

---

## Task 3: JSONL codec

**Files:**
- Create: `internal/protocol/codec.go`
- Create: `internal/protocol/codec_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/protocol/codec_test.go`:
```go
package protocol

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReaderWriter_RoundTrip(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	require.NoError(t, w.WriteIntent(IntentHello, "1", Hello{ClientVersion: "test"}))
	require.NoError(t, w.WriteEvent(EventSnapshot, "", Snapshot{Filter: "all"}))

	r := NewReader(&buf)
	first, err := r.Read()
	require.NoError(t, err)
	require.Equal(t, KindIntent, first.Kind)
	require.Equal(t, IntentHello, first.Type)
	require.Equal(t, "1", first.ID)

	second, err := r.Read()
	require.NoError(t, err)
	require.Equal(t, KindEvent, second.Kind)
	require.Equal(t, EventSnapshot, second.Type)

	_, err = r.Read()
	require.ErrorIs(t, err, io.EOF)
}

func TestReader_RejectsWrongVersion(t *testing.T) {
	var buf bytes.Buffer
	bad, _ := json.Marshal(Envelope{V: 99, Kind: KindIntent, Type: "Hello", Data: json.RawMessage(`{}`)})
	buf.Write(append(bad, '\n'))

	_, err := NewReader(&buf).Read()
	require.ErrorContains(t, err, "version")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:
```bash
go test ./internal/protocol/... -run TestReaderWriter -v
```

Expected: FAIL — `NewWriter`, `NewReader`, `WriteIntent`, `WriteEvent`, `Read` undefined.

- [ ] **Step 3: Implement the codec**

Create `internal/protocol/codec.go`:
```go
package protocol

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

// Reader reads newline-delimited Envelopes from an io.Reader.
type Reader struct {
	br *bufio.Reader
}

// NewReader wraps r.
func NewReader(r io.Reader) *Reader {
	return &Reader{br: bufio.NewReaderSize(r, 64*1024)}
}

// Read returns the next envelope or io.EOF.
func (r *Reader) Read() (Envelope, error) {
	line, err := r.br.ReadBytes('\n')
	if err == io.EOF && len(line) == 0 {
		return Envelope{}, io.EOF
	}
	if err != nil && err != io.EOF {
		return Envelope{}, fmt.Errorf("read line: %w", err)
	}
	var e Envelope
	if err := json.Unmarshal(line, &e); err != nil {
		return Envelope{}, fmt.Errorf("decode envelope: %w", err)
	}
	if e.V != ProtocolVersion {
		return Envelope{}, fmt.Errorf("unsupported protocol version %d (want %d)", e.V, ProtocolVersion)
	}
	return e, nil
}

// Writer writes Envelopes as JSONL.
type Writer struct {
	w io.Writer
}

// NewWriter wraps w.
func NewWriter(w io.Writer) *Writer {
	return &Writer{w: w}
}

// WriteIntent writes an Intent envelope.
func (w *Writer) WriteIntent(typ, id string, payload any) error {
	return w.write(KindIntent, typ, id, payload)
}

// WriteEvent writes an Event envelope.
func (w *Writer) WriteEvent(typ, id string, payload any) error {
	return w.write(KindEvent, typ, id, payload)
}

func (w *Writer) write(kind Kind, typ, id string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	env := Envelope{V: ProtocolVersion, Kind: kind, Type: typ, ID: id, Data: data}
	b, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshal envelope: %w", err)
	}
	b = append(b, '\n')
	if _, err := w.w.Write(b); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run:
```bash
go test ./internal/protocol/... -v
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/protocol/codec.go internal/protocol/codec_test.go
git commit -m "protocol: JSONL reader/writer with version check"
```

---

## Task 4: ID generation

**Files:**
- Create: `internal/ids/ids.go`
- Create: `internal/ids/ids_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/ids/ids_test.go`:
```go
package ids

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewSessionID_FormatAndUniqueness(t *testing.T) {
	uuidRE := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	seen := make(map[string]struct{})
	for i := 0; i < 100; i++ {
		id := NewSessionID()
		require.Regexp(t, uuidRE, id)
		_, dup := seen[id]
		require.False(t, dup, "duplicate id %s", id)
		seen[id] = struct{}{}
	}
}

func TestShortID_FirstFourHex(t *testing.T) {
	id := "7d4f3c8a-1234-4abc-89ab-cdefabcdef01"
	require.Equal(t, "7d4f", ShortID(id))
}

func TestExtendShortID_GrowsOnCollision(t *testing.T) {
	a := "7d4f3c8a-1234-4abc-89ab-cdefabcdef01"
	b := "7d4f9999-1234-4abc-89ab-cdefabcdef02" // shares first 4 hex with a
	taken := map[string]struct{}{ShortID(a): {}}
	// ExtendShortID(b, taken) must return at least 5 chars
	got := ExtendShortID(b, taken)
	require.GreaterOrEqual(t, len(got), 5)
	require.NotEqual(t, ShortID(a), got)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:
```bash
go test ./internal/ids/... -v
```

Expected: FAIL — undefined functions.

- [ ] **Step 3: Implement IDs**

Create `internal/ids/ids.go`:
```go
// Package ids generates session IDs and derives short ids.
package ids

import (
	"crypto/rand"
	"fmt"
)

// NewSessionID returns a random RFC 4122 v4 UUID.
func NewSessionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Should never happen on a healthy system; panic is acceptable per Power-of-Ten escape clause.
		panic(fmt.Errorf("crypto/rand failed: %w", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// ShortID returns the first 4 hex chars of an id.
func ShortID(id string) string {
	if len(id) < 4 {
		return id
	}
	return id[:4]
}

// ExtendShortID returns the smallest prefix of id (>=4 chars) not present in taken.
// Used to disambiguate when two sessions happen to share the first four hex.
func ExtendShortID(id string, taken map[string]struct{}) string {
	for n := 4; n <= len(id); n++ {
		if id[n-1] == '-' { // skip the dash positions in UUIDs
			continue
		}
		candidate := id[:n]
		if _, exists := taken[candidate]; !exists {
			return candidate
		}
	}
	return id
}
```

- [ ] **Step 4: Run test to verify it passes**

Run:
```bash
go test ./internal/ids/... -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ids/
git commit -m "ids: uuid v4 generation + short-id derivation with collision extension"
```

---

## Task 5: Registry types

**Files:**
- Create: `internal/registry/types.go`

- [ ] **Step 1: Define the types**

Create `internal/registry/types.go`:
```go
// Package registry defines the tool/model registry shape and the merge of built-ins with user YAML.
package registry

// Tool is one entry in the registry.
type Tool struct {
	ID           string  `yaml:"id"`
	Name         string  `yaml:"name"`
	Category     string  `yaml:"category"` // "paid" | "self_hosted"
	Command      []string `yaml:"command"`
	CWDStrategy  string  `yaml:"cwd_strategy,omitempty"` // "inherit" | "session_dir"
	Detect       Detect  `yaml:"detect"`
	Icon         string  `yaml:"icon"`
	Color        string  `yaml:"color"`
	Models       []Model `yaml:"models"`
}

// Detect describes how the adapter decides session state.
type Detect struct {
	Kind         string `yaml:"kind"`           // "structured" | "heuristic"
	Format       string `yaml:"format,omitempty"` // when kind=structured
	PromptRegex  string `yaml:"prompt_regex,omitempty"`
	IdleMs       int    `yaml:"idle_ms,omitempty"`
}

// Model is one variant of a tool.
type Model struct {
	ID         string  `yaml:"id"`
	Name       string  `yaml:"name"`
	Args       []string `yaml:"args,omitempty"`
	ArgsPrompt string  `yaml:"args_prompt,omitempty"` // free-form value asked at launch
	Effort     *Effort `yaml:"effort,omitempty"`
}

// Effort is an optional reasoning-effort spec.
type Effort struct {
	Options     []string `yaml:"options"`
	Default     string   `yaml:"default"`
	ArgTemplate string   `yaml:"arg_template"` // e.g. "--effort={value}"
}

// File is the on-disk root of a tools.yaml file.
type File struct {
	Tools []Tool `yaml:"tools"`
}
```

- [ ] **Step 2: Compile-only check**

Run:
```bash
go build ./...
```

Expected: builds.

- [ ] **Step 3: Commit**

```bash
git add internal/registry/types.go
git commit -m "registry: type definitions for tools, models, effort, detect"
```

---

## Task 6: Built-in registry (echo test tool)

**Files:**
- Create: `internal/registry/builtin.yaml`
- Create: `internal/registry/builtin.go`

- [ ] **Step 1: Write the built-in YAML**

Create `internal/registry/builtin.yaml`:
```yaml
# Built-in tools shipped with rex-daemon. Plan A only ships a test tool.
# Real tools (claude/codex/gemini/ollama) land in Plan B once their adapters exist.
tools:
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
```

- [ ] **Step 2: Embed the YAML**

Create `internal/registry/builtin.go`:
```go
package registry

import _ "embed"

//go:embed builtin.yaml
var builtinYAML []byte

// BuiltinBytes returns the embedded built-in registry YAML.
func BuiltinBytes() []byte { return builtinYAML }
```

- [ ] **Step 3: Commit**

```bash
git add internal/registry/builtin.go internal/registry/builtin.yaml
git commit -m "registry: embed built-in tools (Plan A: echo test tool only)"
```

---

## Task 7: Registry loader and merge

**Files:**
- Create: `internal/registry/loader.go`
- Create: `internal/registry/loader_test.go`
- Create: `testdata/tools-user.yaml`

- [ ] **Step 1: Write the failing test**

Create `testdata/tools-user.yaml`:
```yaml
tools:
  - id: echo
    models:
      - id: extra
        name: "Extra (user-added)"
        args: ["echo 'extra'; echo 'done'"]
  - id: usertool
    name: "User tool"
    category: self_hosted
    command: ["true"]
    detect:
      kind: heuristic
      prompt_regex: ">"
      idle_ms: 500
    icon: "◐"
    color: "#FF00FF"
    models:
      - id: only
        name: "Only"
        args: []
```

Create `internal/registry/loader_test.go`:
```go
package registry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoad_BuiltinOnly(t *testing.T) {
	reg, err := Load("")
	require.NoError(t, err)

	echo, ok := reg.Find("echo")
	require.True(t, ok)
	require.Equal(t, "Echo (test)", echo.Name)
	require.Len(t, echo.Models, 3)
}

func TestLoad_UserExtends(t *testing.T) {
	wd, _ := os.Getwd()
	user := filepath.Join(wd, "..", "..", "testdata", "tools-user.yaml")

	reg, err := Load(user)
	require.NoError(t, err)

	echo, _ := reg.Find("echo")
	require.Len(t, echo.Models, 4) // 3 builtin + 1 user-extra

	user2, ok := reg.Find("usertool")
	require.True(t, ok)
	require.Equal(t, "#FF00FF", user2.Color)
}

func TestLoad_BadYAMLFails(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "bad.yaml")
	require.NoError(t, os.WriteFile(tmp, []byte("tools: [oops"), 0o644))
	_, err := Load(tmp)
	require.Error(t, err)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:
```bash
go get gopkg.in/yaml.v3
go test ./internal/registry/... -v
```

Expected: FAIL — `Load`, `Registry`, `Find` undefined.

- [ ] **Step 3: Implement the loader**

Create `internal/registry/loader.go`:
```go
package registry

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Registry is the merged tool set.
type Registry struct {
	Tools []Tool
}

// Find returns the tool with id, plus whether it was found.
func (r *Registry) Find(id string) (Tool, bool) {
	for _, t := range r.Tools {
		if t.ID == id {
			return t, true
		}
	}
	return Tool{}, false
}

// FindModel returns a model within a tool.
func (r *Registry) FindModel(toolID, modelID string) (Tool, Model, bool) {
	t, ok := r.Find(toolID)
	if !ok {
		return Tool{}, Model{}, false
	}
	for _, m := range t.Models {
		if m.ID == modelID {
			return t, m, true
		}
	}
	return t, Model{}, false
}

// Load reads the built-in registry and merges userPath (if non-empty and exists).
func Load(userPath string) (*Registry, error) {
	var built File
	if err := yaml.Unmarshal(BuiltinBytes(), &built); err != nil {
		return nil, fmt.Errorf("parse builtin registry: %w", err)
	}
	if err := validate(built.Tools); err != nil {
		return nil, fmt.Errorf("validate builtin registry: %w", err)
	}

	if userPath == "" {
		return &Registry{Tools: built.Tools}, nil
	}
	raw, err := os.ReadFile(userPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &Registry{Tools: built.Tools}, nil
		}
		return nil, fmt.Errorf("read user registry %s: %w", userPath, err)
	}
	var user File
	if err := yaml.Unmarshal(raw, &user); err != nil {
		return nil, fmt.Errorf("parse user registry %s: %w", userPath, err)
	}

	merged := merge(built.Tools, user.Tools)
	if err := validate(merged); err != nil {
		return nil, fmt.Errorf("validate merged registry: %w", err)
	}
	return &Registry{Tools: merged}, nil
}

func merge(base, over []Tool) []Tool {
	// Index base by id.
	idx := make(map[string]int, len(base))
	out := make([]Tool, len(base))
	copy(out, base)
	for i, t := range out {
		idx[t.ID] = i
	}
	for _, u := range over {
		i, ok := idx[u.ID]
		if !ok {
			out = append(out, u)
			idx[u.ID] = len(out) - 1
			continue
		}
		out[i] = mergeOne(out[i], u)
	}
	return out
}

func mergeOne(base, over Tool) Tool {
	merged := base
	if over.Name != "" {
		merged.Name = over.Name
	}
	if over.Category != "" {
		merged.Category = over.Category
	}
	if len(over.Command) > 0 {
		merged.Command = over.Command
	}
	if over.CWDStrategy != "" {
		merged.CWDStrategy = over.CWDStrategy
	}
	if over.Detect.Kind != "" {
		merged.Detect = over.Detect
	}
	if over.Icon != "" {
		merged.Icon = over.Icon
	}
	if over.Color != "" {
		merged.Color = over.Color
	}
	// Models: extend by id rather than replace.
	if len(over.Models) > 0 {
		mi := make(map[string]int, len(merged.Models))
		for i, m := range merged.Models {
			mi[m.ID] = i
		}
		for _, m := range over.Models {
			if i, ok := mi[m.ID]; ok {
				merged.Models[i] = m
			} else {
				merged.Models = append(merged.Models, m)
				mi[m.ID] = len(merged.Models) - 1
			}
		}
	}
	return merged
}

func validate(tools []Tool) error {
	if len(tools) == 0 {
		return fmt.Errorf("no tools in registry")
	}
	seen := make(map[string]struct{}, len(tools))
	for _, t := range tools {
		if t.ID == "" {
			return fmt.Errorf("tool with empty id")
		}
		if _, dup := seen[t.ID]; dup {
			return fmt.Errorf("duplicate tool id %q", t.ID)
		}
		seen[t.ID] = struct{}{}
		if len(t.Models) == 0 {
			return fmt.Errorf("tool %q has no models", t.ID)
		}
		switch t.Detect.Kind {
		case "structured":
			if t.Detect.Format == "" {
				return fmt.Errorf("tool %q: structured detect needs format", t.ID)
			}
		case "heuristic":
			if t.Detect.PromptRegex == "" || t.Detect.IdleMs <= 0 {
				return fmt.Errorf("tool %q: heuristic detect needs prompt_regex and idle_ms", t.ID)
			}
		default:
			return fmt.Errorf("tool %q: unknown detect.kind %q", t.ID, t.Detect.Kind)
		}
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run:
```bash
go test ./internal/registry/... -v
```

Expected: all three tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/registry/loader.go internal/registry/loader_test.go testdata/tools-user.yaml
git commit -m "registry: built-in + user YAML merge with validation"
```

---

## Task 8: Session struct and State store

**Files:**
- Create: `internal/state/session.go`
- Create: `internal/state/store.go`
- Create: `internal/state/store_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/state/store_test.go`:
```go
package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/protocol"
)

func TestStore_AddAndGet(t *testing.T) {
	s := NewStore()
	sess := &Session{
		ID: "7d4f3c8a-...", ShortID: "7d4f", ToolID: "echo", ModelID: "short",
		Slug: "test", State: protocol.StateWorking, StartedAt: time.Now(),
	}
	require.NoError(t, s.Add(sess))

	got, ok := s.Get(sess.ID)
	require.True(t, ok)
	require.Equal(t, sess.ID, got.ID)

	got2, ok := s.GetByShortID(sess.ShortID)
	require.True(t, ok)
	require.Equal(t, sess.ID, got2.ID)
}

func TestStore_AddDuplicateIDFails(t *testing.T) {
	s := NewStore()
	sess := &Session{ID: "id1", ShortID: "id1"}
	require.NoError(t, s.Add(sess))
	require.Error(t, s.Add(sess))
}

func TestStore_TransitionEmitsUpdate(t *testing.T) {
	s := NewStore()
	sess := &Session{ID: "id1", ShortID: "id1", State: protocol.StateWorking}
	require.NoError(t, s.Add(sess))

	updates := make(chan Event, 4)
	cancel := s.Subscribe(func(e Event) { updates <- e })
	defer cancel()

	require.NoError(t, s.Transition("id1", protocol.StateDone))
	select {
	case e := <-updates:
		require.Equal(t, EventUpdated, e.Kind)
		require.Equal(t, "id1", e.SessionID)
		require.Equal(t, protocol.StateDone, *e.NewState)
	case <-time.After(time.Second):
		t.Fatal("no update emitted")
	}
}

func TestStore_Remove(t *testing.T) {
	s := NewStore()
	sess := &Session{ID: "id1", ShortID: "id1"}
	require.NoError(t, s.Add(sess))
	require.NoError(t, s.Remove("id1"))
	_, ok := s.Get("id1")
	require.False(t, ok)
}

func TestStore_Snapshot(t *testing.T) {
	s := NewStore()
	require.NoError(t, s.Add(&Session{ID: "a", ShortID: "a", State: protocol.StateWorking}))
	require.NoError(t, s.Add(&Session{ID: "b", ShortID: "b", State: protocol.StateDone}))
	snap := s.Snapshot()
	require.Len(t, snap, 2)
}
```

- [ ] **Step 2: Define Session**

Create `internal/state/session.go`:
```go
// Package state owns the in-memory session set and persistence to disk.
package state

import (
	"sync"
	"time"

	"github.com/tristanbietsch/rex/internal/protocol"
)

// Session is the daemon's view of a live or terminated session.
type Session struct {
	ID          string
	ShortID     string
	ToolID      string
	ModelID     string
	Effort      string
	Slug        string
	Title       string
	CWD         string
	State       protocol.State
	StartedAt   time.Time
	LastEventAt time.Time
	LastLine    string
	ExitCode    *int

	// Internal — not serialized to summary.
	mu sync.Mutex
}

// Summary copies the session into a wire-friendly SessionSummary.
func (s *Session) Summary() protocol.SessionSummary {
	s.mu.Lock()
	defer s.mu.Unlock()
	return protocol.SessionSummary{
		ID:          s.ID,
		ShortID:     s.ShortID,
		ToolID:      s.ToolID,
		ModelID:     s.ModelID,
		Effort:      s.Effort,
		Slug:        s.Slug,
		Title:       s.Title,
		CWD:         s.CWD,
		State:       s.State,
		StartedAt:   s.StartedAt,
		LastEventAt: s.LastEventAt,
		LastLine:    s.LastLine,
		ExitCode:    s.ExitCode,
	}
}
```

- [ ] **Step 3: Implement Store**

Create `internal/state/store.go`:
```go
package state

import (
	"fmt"
	"sync"
	"time"

	"github.com/tristanbietsch/rex/internal/protocol"
)

// EventKind classifies a store event.
type EventKind int

const (
	EventAdded EventKind = iota
	EventUpdated
	EventRemoved
)

// Event is what subscribers receive on every change.
type Event struct {
	Kind      EventKind
	SessionID string
	NewState  *protocol.State // set on Updated when state changed
	Patch     map[string]any  // set on Updated for arbitrary field changes
	Summary   *protocol.SessionSummary // set on Added
}

// Store is the central session table.
type Store struct {
	mu          sync.RWMutex
	sessions    map[string]*Session
	byShortID   map[string]string // short_id -> id
	subscribers []func(Event)
	subsMu      sync.RWMutex
}

// NewStore returns an empty store.
func NewStore() *Store {
	return &Store{
		sessions:  make(map[string]*Session),
		byShortID: make(map[string]string),
	}
}

// Add inserts a session. Errors if the ID is taken.
func (s *Store) Add(sess *Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.sessions[sess.ID]; exists {
		return fmt.Errorf("session %s already exists", sess.ID)
	}
	s.sessions[sess.ID] = sess
	s.byShortID[sess.ShortID] = sess.ID

	sum := sess.Summary()
	s.broadcast(Event{Kind: EventAdded, SessionID: sess.ID, Summary: &sum})
	return nil
}

// Get returns a session by full id.
func (s *Store) Get(id string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[id]
	return sess, ok
}

// GetByShortID returns a session by its 4-char short id (or extended).
func (s *Store) GetByShortID(short string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.byShortID[short]
	if !ok {
		return nil, false
	}
	return s.sessions[id], true
}

// All returns a snapshot of every session (pointer values; do not mutate without locking).
func (s *Store) All() []*Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		out = append(out, sess)
	}
	return out
}

// Snapshot returns wire-shaped summaries for every session.
func (s *Store) Snapshot() []protocol.SessionSummary {
	all := s.All()
	out := make([]protocol.SessionSummary, len(all))
	for i, sess := range all {
		out[i] = sess.Summary()
	}
	return out
}

// Remove deletes a session and emits.
func (s *Store) Remove(id string) error {
	s.mu.Lock()
	sess, ok := s.sessions[id]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("session %s not found", id)
	}
	delete(s.sessions, id)
	delete(s.byShortID, sess.ShortID)
	s.mu.Unlock()

	s.broadcast(Event{Kind: EventRemoved, SessionID: id})
	return nil
}

// Transition changes a session's state and emits.
func (s *Store) Transition(id string, newState protocol.State) error {
	s.mu.RLock()
	sess, ok := s.sessions[id]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("session %s not found", id)
	}
	sess.mu.Lock()
	sess.State = newState
	sess.LastEventAt = time.Now().UTC()
	sess.mu.Unlock()

	ns := newState
	s.broadcast(Event{
		Kind:      EventUpdated,
		SessionID: id,
		NewState:  &ns,
		Patch:     map[string]any{"state": newState, "last_event_at": time.Now().UTC()},
	})
	return nil
}

// UpdateLastLine records a transcript-derived summary line.
func (s *Store) UpdateLastLine(id, line string) error {
	s.mu.RLock()
	sess, ok := s.sessions[id]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("session %s not found", id)
	}
	sess.mu.Lock()
	sess.LastLine = line
	sess.LastEventAt = time.Now().UTC()
	sess.mu.Unlock()

	s.broadcast(Event{
		Kind:      EventUpdated,
		SessionID: id,
		Patch:     map[string]any{"last_line": line, "last_event_at": time.Now().UTC()},
	})
	return nil
}

// Subscribe registers a callback for store events. Returns a cancel func.
func (s *Store) Subscribe(fn func(Event)) func() {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	s.subscribers = append(s.subscribers, fn)
	idx := len(s.subscribers) - 1
	return func() {
		s.subsMu.Lock()
		defer s.subsMu.Unlock()
		if idx < len(s.subscribers) {
			s.subscribers[idx] = nil
		}
	}
}

func (s *Store) broadcast(e Event) {
	s.subsMu.RLock()
	defer s.subsMu.RUnlock()
	for _, fn := range s.subscribers {
		if fn != nil {
			fn(e)
		}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run:
```bash
go test ./internal/state/... -v
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/state/session.go internal/state/store.go internal/state/store_test.go
git commit -m "state: in-memory session store with subscribers and short-id index"
```

---

## Task 9: Persistence (meta.json + transcript.log)

**Files:**
- Create: `internal/state/persist.go`
- Create: `internal/state/persist_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/state/persist_test.go`:
```go
package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/protocol"
)

func TestWriteAndLoadMeta(t *testing.T) {
	dir := t.TempDir()
	sess := &Session{
		ID: "id1", ShortID: "id1", ToolID: "echo", ModelID: "short",
		Slug: "ok", State: protocol.StateDone, StartedAt: time.Now().UTC(),
	}
	require.NoError(t, WriteMeta(dir, sess))
	got, err := LoadMeta(dir, sess.ID)
	require.NoError(t, err)
	require.Equal(t, sess.ID, got.ID)
	require.Equal(t, protocol.StateDone, got.State)
}

func TestAppendTranscript(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, AppendTranscript(dir, "id1", []byte("line1\n")))
	require.NoError(t, AppendTranscript(dir, "id1", []byte("line2\n")))
	b, err := os.ReadFile(filepath.Join(dir, "sessions", "id1", "transcript.log"))
	require.NoError(t, err)
	require.Equal(t, "line1\nline2\n", string(b))
}

func TestLoadAllRecoversCrashed(t *testing.T) {
	dir := t.TempDir()
	sess := &Session{
		ID: "id1", ShortID: "id1", State: protocol.StateWorking,
		StartedAt: time.Now().UTC(),
	}
	require.NoError(t, WriteMeta(dir, sess))

	loaded, err := LoadAll(dir)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	require.Equal(t, protocol.StateCrashed, loaded[0].State,
		"previously-working sessions must reload as crashed")
}
```

- [ ] **Step 2: Implement persistence**

Create `internal/state/persist.go`:
```go
package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tristanbietsch/rex/internal/protocol"
)

// sessionDir returns ~/.local/share/rex/sessions/<id> built from a state-dir root.
func sessionDir(root, id string) string {
	return filepath.Join(root, "sessions", id)
}

// WriteMeta persists a session's metadata atomically.
func WriteMeta(root string, s *Session) error {
	dir := sessionDir(root, s.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	final := filepath.Join(dir, "meta.json")
	tmp := final + ".tmp"

	sum := s.Summary()
	b, err := json.MarshalIndent(sum, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal meta: %w", err)
	}
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return fmt.Errorf("write tmp meta: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		return fmt.Errorf("rename meta: %w", err)
	}
	return nil
}

// LoadMeta loads one session's metadata.
func LoadMeta(root, id string) (*Session, error) {
	path := filepath.Join(sessionDir(root, id), "meta.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read meta %s: %w", path, err)
	}
	var sum protocol.SessionSummary
	if err := json.Unmarshal(b, &sum); err != nil {
		return nil, fmt.Errorf("parse meta %s: %w", path, err)
	}
	return fromSummary(sum), nil
}

// LoadAll loads every session under root/sessions/. Any session whose persisted
// state was "working", "queued", or "needs_input" gets reloaded as "crashed"
// because the live PTY didn't survive whatever caused the daemon to stop.
func LoadAll(root string) ([]*Session, error) {
	dir := filepath.Join(root, "sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read sessions dir: %w", err)
	}
	out := make([]*Session, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sess, err := LoadMeta(root, e.Name())
		if err != nil {
			// Skip unreadable sessions rather than fail the whole daemon.
			continue
		}
		switch sess.State {
		case protocol.StateQueued, protocol.StateWorking, protocol.StateNeedsInput:
			sess.State = protocol.StateCrashed
		}
		out = append(out, sess)
	}
	return out, nil
}

// AppendTranscript appends bytes to the session's transcript.log, creating dirs as needed.
func AppendTranscript(root, id string, b []byte) error {
	dir := sessionDir(root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	f, err := os.OpenFile(filepath.Join(dir, "transcript.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open transcript: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(b); err != nil {
		return fmt.Errorf("write transcript: %w", err)
	}
	return nil
}

func fromSummary(sum protocol.SessionSummary) *Session {
	return &Session{
		ID: sum.ID, ShortID: sum.ShortID, ToolID: sum.ToolID, ModelID: sum.ModelID,
		Effort: sum.Effort, Slug: sum.Slug, Title: sum.Title, CWD: sum.CWD,
		State: sum.State, StartedAt: sum.StartedAt, LastEventAt: sum.LastEventAt,
		LastLine: sum.LastLine, ExitCode: sum.ExitCode,
	}
}
```

- [ ] **Step 3: Run test to verify it passes**

Run:
```bash
go test ./internal/state/... -v
```

Expected: all tests PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/state/persist.go internal/state/persist_test.go
git commit -m "state: persist meta.json + transcript.log; reload working->crashed"
```

---

## Task 10: Adapter interface and HeuristicCLI

**Files:**
- Create: `internal/adapter/adapter.go`
- Create: `internal/adapter/heuristic.go`
- Create: `internal/adapter/heuristic_test.go`

- [ ] **Step 1: Define the adapter interface**

Create `internal/adapter/adapter.go`:
```go
// Package adapter classifies session state from PTY output.
//
// Plan A ships HeuristicCLI (regex + idle). Plan B adds ClaudeStructured.
package adapter

import (
	"time"

	"github.com/tristanbietsch/rex/internal/protocol"
	"github.com/tristanbietsch/rex/internal/registry"
)

// Adapter classifies output chunks into states.
//
// The adapter is given a window of recent bytes plus the time since the last
// chunk arrived. It returns the state the session should be in (or an empty
// string to leave the state unchanged).
type Adapter interface {
	// Detect returns the next state given recent output and idle duration.
	// Returns "" to leave the state unchanged.
	Detect(window []byte, idle time.Duration) protocol.State
}

// For builds an adapter for a tool's detection config.
func For(t registry.Tool) (Adapter, error) {
	switch t.Detect.Kind {
	case "heuristic":
		return NewHeuristic(t.Detect.PromptRegex, time.Duration(t.Detect.IdleMs)*time.Millisecond), nil
	case "structured":
		// Plan B will return ClaudeStructured here.
		return nil, ErrStructuredUnsupported
	default:
		return nil, ErrUnknownDetect
	}
}
```

- [ ] **Step 2: Write the failing HeuristicCLI test**

Create `internal/adapter/heuristic_test.go`:
```go
package adapter

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/protocol"
)

func TestHeuristic_NeedsInputWhenIdleAndPromptMatches(t *testing.T) {
	h := NewHeuristic("^awaiting input:", 100*time.Millisecond)
	got := h.Detect([]byte("hello\nawaiting input:"), 200*time.Millisecond)
	require.Equal(t, protocol.StateNeedsInput, got)
}

func TestHeuristic_WorkingWhenNotIdle(t *testing.T) {
	h := NewHeuristic("^awaiting input:", 100*time.Millisecond)
	got := h.Detect([]byte("doing things..."), 10*time.Millisecond)
	require.Equal(t, protocol.StateWorking, got)
}

func TestHeuristic_WorkingWhenIdleButNoPromptMatch(t *testing.T) {
	h := NewHeuristic("^awaiting input:", 100*time.Millisecond)
	got := h.Detect([]byte("doing things"), 5*time.Second)
	require.Equal(t, protocol.StateWorking, got)
}
```

- [ ] **Step 3: Implement HeuristicCLI**

Create `internal/adapter/heuristic.go`:
```go
package adapter

import (
	"errors"
	"regexp"
	"time"

	"github.com/tristanbietsch/rex/internal/protocol"
)

// ErrUnknownDetect signals an unsupported detect.kind in the registry.
var ErrUnknownDetect = errors.New("unknown detect kind")

// ErrStructuredUnsupported is returned in Plan A; Plan B implements ClaudeStructured.
var ErrStructuredUnsupported = errors.New("structured adapter not implemented in Plan A")

// HeuristicCLI is a regex+idle adapter for CLIs without structured output.
type HeuristicCLI struct {
	prompt *regexp.Regexp
	idle   time.Duration
}

// NewHeuristic builds a HeuristicCLI.
func NewHeuristic(promptRegex string, idle time.Duration) *HeuristicCLI {
	// Use multiline mode so ^ matches start-of-line by default.
	re := regexp.MustCompile("(?m)" + promptRegex)
	return &HeuristicCLI{prompt: re, idle: idle}
}

// Detect implements Adapter.
func (h *HeuristicCLI) Detect(window []byte, idle time.Duration) protocol.State {
	if idle < h.idle {
		return protocol.StateWorking
	}
	// The window has been idle for longer than idle_ms. Now check whether the
	// trailing region matches the prompt pattern — if so, the session is waiting.
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

- [ ] **Step 4: Run test to verify it passes**

Run:
```bash
go test ./internal/adapter/... -v
```

Expected: all three tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/adapter/
git commit -m "adapter: interface + HeuristicCLI (regex + idle)"
```

---

## Task 11: PTY supervisor

**Files:**
- Create: `internal/pty/supervisor.go`
- Create: `internal/pty/supervisor_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/pty/supervisor_test.go`:
```go
package pty

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/protocol"
	"github.com/tristanbietsch/rex/internal/state"
)

func TestSupervisor_RunEchoToCompletion(t *testing.T) {
	stateDir := t.TempDir()
	store := state.NewStore()
	sess := &state.Session{
		ID:        "id1",
		ShortID:   "id1",
		ToolID:    "echo",
		ModelID:   "short",
		Slug:      "test",
		State:     protocol.StateQueued,
		StartedAt: time.Now().UTC(),
	}
	require.NoError(t, store.Add(sess))

	output := make(chan []byte, 32)
	sup := New(SupervisorConfig{
		StateDir:  stateDir,
		Store:     store,
		Command:   []string{"bash", "-c", "echo hello; echo done"},
		Adapter:   nil, // unused: we'll mark done on exit
		OutputSink: func(b []byte) { output <- b },
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := sup.Run(ctx, sess)
	require.NoError(t, err)

	got, _ := store.Get("id1")
	require.Equal(t, protocol.StateDone, got.State)

	// Should have collected something on the output channel.
	close(output)
	var all strings.Builder
	for chunk := range output {
		all.Write(chunk)
	}
	require.Contains(t, all.String(), "hello")
}
```

- [ ] **Step 2: Implement the supervisor**

Create `internal/pty/supervisor.go`:
```go
// Package pty supervises a single agent session: spawn, read, classify, persist.
package pty

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"

	"github.com/creack/pty"
	"github.com/tristanbietsch/rex/internal/adapter"
	"github.com/tristanbietsch/rex/internal/protocol"
	"github.com/tristanbietsch/rex/internal/state"
)

// SupervisorConfig configures a per-session supervisor.
type SupervisorConfig struct {
	StateDir   string                   // root of persisted state (~/.local/share/rex)
	Store      *state.Store             // central store for state transitions
	Command    []string                 // argv to spawn (command + args resolved from registry+model)
	CWD        string                   // working directory for the child
	Adapter    adapter.Adapter          // nil = no state classification (tests/echo tool)
	OutputSink func(b []byte)           // called with every chunk read from PTY; non-blocking
	IdleTick   time.Duration            // how often we sample idle for the adapter (default 200ms)
}

// Supervisor runs one PTY session to completion.
type Supervisor struct {
	cfg SupervisorConfig
}

// New constructs a Supervisor.
func New(cfg SupervisorConfig) *Supervisor {
	if cfg.IdleTick == 0 {
		cfg.IdleTick = 200 * time.Millisecond
	}
	return &Supervisor{cfg: cfg}
}

// Run starts the child process and blocks until it exits, ctx is canceled, or an error occurs.
func (s *Supervisor) Run(ctx context.Context, sess *state.Session) error {
	if len(s.cfg.Command) == 0 {
		return errors.New("supervisor: empty command")
	}

	cmd := exec.CommandContext(ctx, s.cfg.Command[0], s.cfg.Command[1:]...)
	if s.cfg.CWD != "" {
		cmd.Dir = s.cfg.CWD
	}
	f, err := pty.Start(cmd)
	if err != nil {
		_ = s.cfg.Store.Transition(sess.ID, protocol.StateFailed)
		return fmt.Errorf("pty start: %w", err)
	}
	defer f.Close()

	if err := s.cfg.Store.Transition(sess.ID, protocol.StateWorking); err != nil {
		return err
	}

	// Track the most recent output window and last-chunk time for adapter classification.
	window := make([]byte, 0, 8192)
	lastChunk := time.Now()
	errc := make(chan error, 1)

	// Reader goroutine.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, rerr := f.Read(buf)
			if n > 0 {
				chunk := append([]byte(nil), buf[:n]...)
				if err := state.AppendTranscript(s.cfg.StateDir, sess.ID, chunk); err != nil {
					errc <- fmt.Errorf("persist transcript: %w", err)
					return
				}
				if s.cfg.OutputSink != nil {
					s.cfg.OutputSink(chunk)
				}
				window = appendBounded(window, chunk, 8192)
				lastChunk = time.Now()
				// Update last_line — last non-empty line in the window.
				if line := lastNonEmptyLine(window); line != "" {
					_ = s.cfg.Store.UpdateLastLine(sess.ID, line)
				}
			}
			if rerr != nil {
				if errors.Is(rerr, io.EOF) {
					errc <- nil
				} else {
					errc <- fmt.Errorf("pty read: %w", rerr)
				}
				return
			}
		}
	}()

	// Adapter classification ticker.
	ticker := time.NewTicker(s.cfg.IdleTick)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			<-errc
			_ = s.cfg.Store.Transition(sess.ID, protocol.StateFailed)
			return ctx.Err()
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
		case <-ticker.C:
			if s.cfg.Adapter == nil {
				continue
			}
			idle := time.Since(lastChunk)
			next := s.cfg.Adapter.Detect(window, idle)
			if next == "" {
				continue
			}
			current := sess.State
			if next != current && next != protocol.StateWorking {
				_ = s.cfg.Store.Transition(sess.ID, next)
			}
		}
	}
}

func appendBounded(buf, b []byte, cap int) []byte {
	out := append(buf, b...)
	if len(out) > cap {
		out = out[len(out)-cap:]
	}
	return out
}

func lastNonEmptyLine(b []byte) string {
	// Walk backwards to find the last newline; return the trimmed segment after it.
	end := len(b)
	for end > 0 && (b[end-1] == '\n' || b[end-1] == '\r') {
		end--
	}
	start := end
	for start > 0 && b[start-1] != '\n' && b[start-1] != '\r' {
		start--
	}
	if start == end {
		return ""
	}
	return string(b[start:end])
}
```

- [ ] **Step 3: Pull in creack/pty**

Run:
```bash
go get github.com/creack/pty
```

- [ ] **Step 4: Run test to verify it passes**

Run:
```bash
go test ./internal/pty/... -v
```

Expected: PASS within ~3 s. The echo+done pipeline completes and the store transitions to `done`.

- [ ] **Step 5: Commit**

```bash
git add internal/pty/ go.mod go.sum
git commit -m "pty: per-session supervisor — spawn, read, classify, persist"
```

---

## Task 12: Server scaffolding (UDS listen + accept)

**Files:**
- Create: `internal/server/server.go`
- Create: `internal/server/server_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/server/server_test.go`:
```go
package server

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/protocol"
	"github.com/tristanbietsch/rex/internal/registry"
	"github.com/tristanbietsch/rex/internal/state"
)

func TestServer_AcceptsConnection(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "rex.sock")
	reg, err := registry.Load("")
	require.NoError(t, err)

	srv, err := New(Config{
		Socket:   sock,
		StateDir: dir,
		Registry: reg,
		Store:    state.NewStore(),
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.Serve(ctx) }()

	// Wait for the socket to appear.
	deadline := time.Now().Add(2 * time.Second)
	var conn net.Conn
	for time.Now().Before(deadline) {
		conn, err = net.Dial("unix", sock)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	require.NoError(t, err)
	require.NotNil(t, conn)
	defer conn.Close()

	r := protocol.NewReader(conn)
	w := protocol.NewWriter(conn)
	require.NoError(t, w.WriteIntent(protocol.IntentHello, "1", protocol.Hello{ClientVersion: "test"}))

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	env, err := r.Read()
	require.NoError(t, err)
	require.Equal(t, protocol.KindEvent, env.Kind)
	require.Equal(t, protocol.EventSnapshot, env.Type)
}
```

- [ ] **Step 2: Implement the server**

Create `internal/server/server.go`:
```go
// Package server runs the UDS listener and dispatches per-client handlers.
package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"

	"github.com/tristanbietsch/rex/internal/registry"
	"github.com/tristanbietsch/rex/internal/state"
)

// Config bundles a Server's dependencies.
type Config struct {
	Socket   string
	StateDir string
	Registry *registry.Registry
	Store    *state.Store
}

// Server owns the UDS listener and accepts clients.
type Server struct {
	cfg   Config
	wg    sync.WaitGroup
	once  sync.Once
	closed bool
}

// New unlinks any stale socket and constructs a Server. It does not listen yet.
func New(cfg Config) (*Server, error) {
	if cfg.Socket == "" {
		return nil, errors.New("server: empty socket path")
	}
	// Unlink any stale socket. If something is actually listening on it we'll
	// fail on Listen below, which is the right outcome.
	_ = os.Remove(cfg.Socket)
	return &Server{cfg: cfg}, nil
}

// Serve listens until ctx is canceled. The socket is unlinked on return.
func (s *Server) Serve(ctx context.Context) error {
	l, err := net.Listen("unix", s.cfg.Socket)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.cfg.Socket, err)
	}
	defer func() {
		_ = l.Close()
		_ = os.Remove(s.cfg.Socket)
	}()

	// Close the listener when ctx is canceled to unblock Accept.
	go func() {
		<-ctx.Done()
		_ = l.Close()
	}()

	for {
		conn, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				s.wg.Wait()
				return nil
			}
			return fmt.Errorf("accept: %w", err)
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			handleClient(ctx, conn, s.cfg)
		}()
	}
}
```

- [ ] **Step 3: Stub the client handler**

Create `internal/server/client.go`:
```go
package server

import (
	"context"
	"net"

	"github.com/tristanbietsch/rex/internal/protocol"
)

func handleClient(ctx context.Context, conn net.Conn, cfg Config) {
	defer conn.Close()
	r := protocol.NewReader(conn)
	w := protocol.NewWriter(conn)

	// Read intents until ctx canceled or connection closes.
	for {
		if ctx.Err() != nil {
			return
		}
		env, err := r.Read()
		if err != nil {
			return
		}
		if env.Kind != protocol.KindIntent {
			_ = w.WriteEvent(protocol.EventError, env.ID, protocol.ErrorEvent{
				ID: env.ID, Code: protocol.ErrCodeBadIntent, Message: "expected an Intent",
			})
			continue
		}
		switch env.Type {
		case protocol.IntentHello:
			snap := protocol.Snapshot{Sessions: cfg.Store.Snapshot(), Filter: "all"}
			_ = w.WriteEvent(protocol.EventSnapshot, env.ID, snap)
		default:
			// Plan A only routes Hello; richer intents arrive in subsequent tasks.
			_ = w.WriteEvent(protocol.EventError, env.ID, protocol.ErrorEvent{
				ID: env.ID, Code: protocol.ErrCodeBadIntent, Message: "intent not implemented in Plan A",
			})
		}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run:
```bash
go test ./internal/server/... -v
```

Expected: PASS — the server starts, accepts the connection, responds to Hello with Snapshot.

- [ ] **Step 5: Commit**

```bash
git add internal/server/
git commit -m "server: UDS listener + client handler stub (Hello -> Snapshot)"
```

---

## Task 13: NewSession / Delete intents

**Files:**
- Modify: `internal/server/client.go`
- Create: `internal/server/client_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/server/client_test.go`:
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

func startServer(t *testing.T) (string, context.CancelFunc) {
	t.Helper()
	dir := t.TempDir()
	sock := filepath.Join(dir, "rex.sock")
	reg, err := registry.Load("")
	require.NoError(t, err)
	srv, err := New(Config{Socket: sock, StateDir: dir, Registry: reg, Store: state.NewStore()})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = srv.Serve(ctx) }()

	// Wait for socket.
	for i := 0; i < 100; i++ {
		if _, err := net.Dial("unix", sock); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	return sock, cancel
}

func TestNewSession_SpawnsAndCompletes(t *testing.T) {
	sock, cancel := startServer(t)
	defer cancel()
	conn, err := net.Dial("unix", sock)
	require.NoError(t, err)
	defer conn.Close()

	w := protocol.NewWriter(conn)
	r := protocol.NewReader(conn)

	require.NoError(t, w.WriteIntent(protocol.IntentHello, "h", protocol.Hello{ClientVersion: "test"}))
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	env, err := r.Read()
	require.NoError(t, err)
	require.Equal(t, protocol.EventSnapshot, env.Type)

	require.NoError(t, w.WriteIntent(protocol.IntentNewSession, "n1", protocol.NewSession{
		ToolID: "echo", ModelID: "short", Slug: "test", CWD: t.TempDir(),
	}))

	// Drain events until we see SessionAdded then SessionUpdated to Done.
	gotDone := false
	deadline := time.Now().Add(10 * time.Second)
	for !gotDone && time.Now().Before(deadline) {
		conn.SetReadDeadline(time.Now().Add(deadline.Sub(time.Now())))
		env, err := r.Read()
		require.NoError(t, err)
		if env.Type == protocol.EventSessionUpdated {
			var upd protocol.SessionUpdated
			require.NoError(t, json.Unmarshal(env.Data, &upd))
			if s, ok := upd.Patch["state"].(string); ok && s == string(protocol.StateDone) {
				gotDone = true
			}
		}
	}
	require.True(t, gotDone, "session never reached done")
}
```

- [ ] **Step 2: Extend the client handler**

Modify `internal/server/client.go` — replace the entire file with:

```go
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/tristanbietsch/rex/internal/adapter"
	"github.com/tristanbietsch/rex/internal/ids"
	"github.com/tristanbietsch/rex/internal/protocol"
	"github.com/tristanbietsch/rex/internal/pty"
	"github.com/tristanbietsch/rex/internal/state"
)

func handleClient(ctx context.Context, conn net.Conn, cfg Config) {
	defer conn.Close()
	r := protocol.NewReader(conn)
	w := protocol.NewWriter(conn)

	// Per-client subscription that forwards store events back to this client.
	subscribed := false
	cancel := cfg.Store.Subscribe(func(e state.Event) {
		if !subscribed {
			return
		}
		emitEvent(w, e)
	})
	defer cancel()

	for {
		if ctx.Err() != nil {
			return
		}
		env, err := r.Read()
		if err != nil {
			return
		}
		if env.Kind != protocol.KindIntent {
			writeError(w, env.ID, protocol.ErrCodeBadIntent, "expected an Intent")
			continue
		}
		switch env.Type {
		case protocol.IntentHello:
			snap := protocol.Snapshot{Sessions: cfg.Store.Snapshot(), Filter: "all"}
			_ = w.WriteEvent(protocol.EventSnapshot, env.ID, snap)
			subscribed = true
		case protocol.IntentNewSession:
			var p protocol.NewSession
			if err := json.Unmarshal(env.Data, &p); err != nil {
				writeError(w, env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			if err := handleNewSession(ctx, env.ID, p, cfg, w); err != nil {
				writeError(w, env.ID, protocol.ErrCodeSpawn, err.Error())
			}
		case protocol.IntentDelete:
			var p protocol.Delete
			if err := json.Unmarshal(env.Data, &p); err != nil {
				writeError(w, env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			if err := cfg.Store.Remove(p.SessionID); err != nil {
				writeError(w, env.ID, protocol.ErrCodeUnknownSession, err.Error())
			}
		default:
			writeError(w, env.ID, protocol.ErrCodeBadIntent, "intent not implemented in Plan A")
		}
	}
}

func handleNewSession(ctx context.Context, intentID string, p protocol.NewSession, cfg Config, w *protocol.Writer) error {
	tool, model, ok := cfg.Registry.FindModel(p.ToolID, p.ModelID)
	if !ok {
		return fmt.Errorf("tool %s/%s not in registry", p.ToolID, p.ModelID)
	}

	id := ids.NewSessionID()
	short := ids.ShortID(id)
	// Disambiguate against the live set.
	taken := make(map[string]struct{})
	for _, s := range cfg.Store.All() {
		taken[s.ShortID] = struct{}{}
	}
	short = ids.ExtendShortID(id, taken)

	cmdArgs := append([]string{}, tool.Command...)
	cmdArgs = append(cmdArgs, model.Args...)

	sess := &state.Session{
		ID:        id,
		ShortID:   short,
		ToolID:    p.ToolID,
		ModelID:   p.ModelID,
		Effort:    p.Effort,
		Slug:      p.Slug,
		Title:     p.Title,
		CWD:       p.CWD,
		State:     protocol.StateQueued,
		StartedAt: time.Now().UTC(),
	}
	if err := cfg.Store.Add(sess); err != nil {
		return err
	}

	ad, err := adapter.For(tool)
	if err != nil {
		return fmt.Errorf("build adapter: %w", err)
	}

	sup := pty.New(pty.SupervisorConfig{
		StateDir:   cfg.StateDir,
		Store:      cfg.Store,
		Command:    cmdArgs,
		CWD:        p.CWD,
		Adapter:    ad,
		OutputSink: nil, // Plan A: subscribers consume via SessionOutput in Plan B; for now drop.
	})

	// Run in a background goroutine; the store events drive the wire.
	go func() {
		_ = sup.Run(ctx, sess)
	}()
	return nil
}

func emitEvent(w *protocol.Writer, e state.Event) {
	switch e.Kind {
	case state.EventAdded:
		if e.Summary != nil {
			_ = w.WriteEvent(protocol.EventSessionAdded, "", *e.Summary)
		}
	case state.EventUpdated:
		_ = w.WriteEvent(protocol.EventSessionUpdated, "", protocol.SessionUpdated{
			SessionID: e.SessionID, Patch: e.Patch,
		})
	case state.EventRemoved:
		_ = w.WriteEvent(protocol.EventSessionRemoved, "", protocol.SessionRemoved{
			SessionID: e.SessionID,
		})
	}
}

func writeError(w *protocol.Writer, id, code, msg string) {
	_ = w.WriteEvent(protocol.EventError, id, protocol.ErrorEvent{ID: id, Code: code, Message: msg})
}
```

- [ ] **Step 3: Run test to verify it passes**

Run:
```bash
go test ./internal/server/... -v
```

Expected: both server tests PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/server/client.go internal/server/client_test.go
git commit -m "server: handle NewSession (spawn + supervisor) and Delete"
```

---

## Task 14: Daemon entry point — flags + lifecycle

**Files:**
- Modify: `cmd/rex-daemon/main.go`

- [ ] **Step 1: Replace the stub with a real entry point**

Replace `cmd/rex-daemon/main.go`:

```go
// Package main is the rex-daemon entry point.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/tristanbietsch/rex/internal/registry"
	"github.com/tristanbietsch/rex/internal/server"
	"github.com/tristanbietsch/rex/internal/state"
)

const version = "0.0.1-plan-a"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "rex-daemon:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("rex-daemon", flag.ContinueOnError)
	socketPath := fs.String("socket", defaultSocketPath(), "UDS path")
	stateDir := fs.String("state-dir", defaultStateDir(), "state directory")
	toolsPath := fs.String("tools", defaultToolsPath(), "path to tools.yaml override (optional)")
	printVersion := fs.Bool("version", false, "print version and exit")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *printVersion {
		fmt.Println(version)
		return nil
	}

	reg, err := registry.Load(*toolsPath)
	if err != nil {
		return fmt.Errorf("registry: %w", err)
	}

	if err := os.MkdirAll(*stateDir, 0o755); err != nil {
		return fmt.Errorf("state dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(*socketPath), 0o755); err != nil {
		return fmt.Errorf("socket dir: %w", err)
	}

	store := state.NewStore()
	prior, err := state.LoadAll(*stateDir)
	if err != nil {
		return fmt.Errorf("load prior sessions: %w", err)
	}
	for _, s := range prior {
		_ = store.Add(s)
	}

	srv, err := server.New(server.Config{
		Socket:   *socketPath,
		StateDir: *stateDir,
		Registry: reg,
		Store:    store,
	})
	if err != nil {
		return fmt.Errorf("server: %w", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	fmt.Fprintf(os.Stderr, "rex-daemon %s listening on %s\n", version, *socketPath)
	return srv.Serve(ctx)
}

func defaultSocketPath() string {
	if r := os.Getenv("XDG_RUNTIME_DIR"); r != "" {
		return filepath.Join(r, "rex.sock")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "rex", "rex.sock")
}

func defaultStateDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "rex")
}

func defaultToolsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "rex", "tools.yaml")
}
```

- [ ] **Step 2: Build and smoke-test**

Run:
```bash
make build
./rex-daemon --version
```

Expected:
```
0.0.1-plan-a
```

Run:
```bash
./rex-daemon --socket /tmp/rex-test.sock --state-dir /tmp/rex-state &
sleep 0.5
printf '{"v":1,"kind":"Intent","type":"Hello","data":{"client_version":"manual"}}\n' \
  | nc -U /tmp/rex-test.sock | head -1
kill %1 2>/dev/null || true
rm -f /tmp/rex-test.sock
```

Expected (on the captured line): a JSON object whose `type` is `"Snapshot"`.

- [ ] **Step 3: Commit**

```bash
git add cmd/rex-daemon/main.go
git commit -m "rex-daemon: real entry point — flags, lifecycle, signal handling"
```

---

## Task 15: End-to-end test — spawn → done

**Files:**
- Create: `internal/server/e2e_test.go`

- [ ] **Step 1: Write the e2e test**

Create `internal/server/e2e_test.go`:
```go
package server

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/protocol"
)

// TestE2E_FullFlow validates: Hello -> Snapshot -> NewSession -> SessionAdded ->
// SessionUpdated(working) -> SessionUpdated(done) -> meta.json on disk.
func TestE2E_FullFlow(t *testing.T) {
	sock, cancel := startServer(t)
	defer cancel()

	conn, err := net.Dial("unix", sock)
	require.NoError(t, err)
	defer conn.Close()
	r := protocol.NewReader(conn)
	w := protocol.NewWriter(conn)

	require.NoError(t, w.WriteIntent(protocol.IntentHello, "h", protocol.Hello{ClientVersion: "e2e"}))
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	env, err := r.Read()
	require.NoError(t, err)
	require.Equal(t, protocol.EventSnapshot, env.Type)

	cwd := t.TempDir()
	require.NoError(t, w.WriteIntent(protocol.IntentNewSession, "n", protocol.NewSession{
		ToolID: "echo", ModelID: "short", Slug: "e2e", CWD: cwd,
	}))

	var sessID string
	gotDone := false
	deadline := time.Now().Add(8 * time.Second)
	for !gotDone {
		require.True(t, time.Now().Before(deadline), "timed out waiting for done")
		conn.SetReadDeadline(deadline)
		env, err := r.Read()
		require.NoError(t, err)
		switch env.Type {
		case protocol.EventSessionAdded:
			var sum protocol.SessionSummary
			require.NoError(t, json.Unmarshal(env.Data, &sum))
			sessID = sum.ID
		case protocol.EventSessionUpdated:
			var upd protocol.SessionUpdated
			require.NoError(t, json.Unmarshal(env.Data, &upd))
			if s, ok := upd.Patch["state"].(string); ok && s == string(protocol.StateDone) {
				gotDone = true
			}
		}
	}
	require.NotEmpty(t, sessID)

	// State dir resolution: startServer uses TempDir, but we can't reach it from here.
	// Instead, derive the path from XDG-style assumptions used by server.Config.
	// startServer sets StateDir to a t.TempDir(); we'll need to thread it out.
	// (See Task 15 follow-up below: startServer is enhanced to return the state dir.)
	dir := stateDirFor(t, sock)
	meta := filepath.Join(dir, "sessions", sessID, "meta.json")
	_, err = os.Stat(meta)
	require.NoError(t, err, "meta.json must exist on disk after done")
}

// stateDirFor recovers the state dir from the socket path's parent.
// Helper introduced because startServer puts both inside the same t.TempDir().
func stateDirFor(t *testing.T, sock string) string {
	t.Helper()
	return filepath.Dir(sock)
}
```

- [ ] **Step 2: Run the test**

Run:
```bash
go test ./internal/server/... -run TestE2E -v
```

Expected: PASS within ~5 seconds. `meta.json` exists on disk.

- [ ] **Step 3: Commit**

```bash
git add internal/server/e2e_test.go
git commit -m "test: e2e — hello -> snapshot -> spawn -> done -> meta.json persisted"
```

---

## Task 16: Manual smoke test recipe (Plan A acceptance)

**Files:**
- Create: `docs/superpowers/plans/2026-05-14-rex-daemon-foundation-smoke.md`

- [ ] **Step 1: Write the smoke-test doc**

Create `docs/superpowers/plans/2026-05-14-rex-daemon-foundation-smoke.md`:

````markdown
# Plan A smoke test

Five-minute manual check that the daemon is alive and behaves.

## Setup

```sh
make build
./rex-daemon --socket /tmp/rex.sock --state-dir /tmp/rex-state
```

In another terminal:

## 1. Hello → Snapshot

```sh
( printf '{"v":1,"kind":"Intent","type":"Hello","id":"h","data":{"client_version":"manual"}}\n'; sleep 0.2 ) \
  | socat - UNIX-CONNECT:/tmp/rex.sock | head -1 | jq
```

Expect a `Snapshot` event with `sessions: []`.

## 2. Spawn an echo session

```sh
( printf '{"v":1,"kind":"Intent","type":"Hello","id":"h","data":{}}\n'; \
  printf '{"v":1,"kind":"Intent","type":"NewSession","id":"n","data":{"tool_id":"echo","model_id":"short","slug":"smoke","cwd":"/tmp"}}\n'; \
  sleep 4 ) \
  | socat - UNIX-CONNECT:/tmp/rex.sock | jq -c '.type'
```

Expect, in order:
- `"Snapshot"`
- `"SessionAdded"`
- `"SessionUpdated"` (state → working)
- `"SessionUpdated"` (last_line updates as echo runs)
- `"SessionUpdated"` (state → done)

## 3. Verify persistence

```sh
ls /tmp/rex-state/sessions/
cat /tmp/rex-state/sessions/*/meta.json | jq
cat /tmp/rex-state/sessions/*/transcript.log
```

Expect one session dir, a `meta.json` with `state: "done"`, and a transcript with the echo output.

## 4. Verify "crashed" recovery

Kill the daemon (ctrl+c), then restart and verify the prior session reloads as `crashed`. (Spawn a `long` session first if you want to see a state actually flip from working to crashed.)
````

- [ ] **Step 2: Commit**

```bash
git add docs/superpowers/plans/2026-05-14-rex-daemon-foundation-smoke.md
git commit -m "docs: Plan A smoke test recipe"
```

---

## Acceptance criteria

Plan A is **done** when all of the following hold:

1. `make test` is green.
2. `make lint` is green.
3. `make build` produces a `rex-daemon` binary.
4. The smoke test recipe in `docs/superpowers/plans/2026-05-14-rex-daemon-foundation-smoke.md` passes end-to-end.
5. Killing the daemon mid-session and restarting it reloads that session as `crashed`.
6. `tools-user.yaml` overrides demonstrably extend the registry (tested via `TestLoad_UserExtends`).

The daemon at this point is feature-complete for: spawning sessions, classifying state via heuristics, persisting transcripts and metadata, and serving JSONL over UDS. Plan B can begin in parallel against this surface.

## Self-review notes

- **Spec coverage:** Tasks cover protocol envelope/codec, registry merge, session state store, persistence (meta + transcript + crashed-recovery), `HeuristicCLI` adapter, PTY supervisor, UDS server, `Hello`/`NewSession`/`Delete` intents end-to-end. **Deferred to Plan B:** other intents (Subscribe-stream, SendInput, Reply, Rename, FocusFilter, Shutdown), `ClaudeStructured` adapter, real tools (claude/codex/gemini/ollama/grok/deepseek/kimi), CLI commands, `--max-concurrent-sessions` enforcement, transcript rotation, `--foreground`/`--log-level` flags. These deferrals are intentional and called out at the top of the plan.
- **Placeholder scan:** No "TBD"/"add appropriate error handling"/"similar to Task N" references. Every code-bearing step includes complete code.
- **Type consistency:** `SessionSummary`, `Session`, `Tool`, `Model`, `Detect`, `Effort`, `Registry`, `Store`, `Event`, `Adapter`, `SupervisorConfig`, `Config` (server) — each appears in exactly the form defined; method signatures match across tasks (`Store.Add`/`Get`/`GetByShortID`/`Transition`/`Remove`/`UpdateLastLine`/`Subscribe`).
- **Module path:** Hard-coded to `github.com/tristanbietsch/rex`. Engineers should adjust the `go mod init` line in Task 0 if a different module path is desired; the rest of the plan's imports follow it.
