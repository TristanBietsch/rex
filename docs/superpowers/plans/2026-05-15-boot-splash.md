# rex Boot Splash Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a 1-1.5 s alt-screen boot splash that renders rex's real init pipeline as systemd-style `[ OK ]` / `[ FAIL ]` / `[ WARN ]` log rows with Japanese flavor, a per-row chime from the active soundset, and a graceful failure screen.

**Architecture:** Move the daemon dial / Hello / Subscribe / settings / audio bootstrap *into* the Bubble Tea program as a sequence of async `tea.Cmd`s. A new `FocusBoot` mode renders the boot log while these run. The splash hands off to `FocusBoard` once the real pipeline finishes AND a min-duration timer elapses. Daemon-spawn helpers are extracted to a new `internal/daemonctl` package so both the CLI and the splash can call them. Three new audio events (`boot_ok` / `boot_warn` / `boot_fail`) are added to every soundset catalog.

**Tech Stack:** Go 1.22+, Bubble Tea (`github.com/charmbracelet/bubbletea`), lipgloss, `github.com/charmbracelet/x/ansi` for east-asian width, `ebitengine/oto/v3` (already in use), `stretchr/testify`, YAML settings via `gopkg.in/yaml.v3`.

**Spec:** `docs/superpowers/specs/2026-05-15-boot-splash-design.md`

**Erratum on the spec:** settings live in `~/.config/rex/config.yaml` (YAML, not JSON). Plan code uses the real path / format.

**Build order summary:**
- Stage 1: extract `internal/daemonctl` (no behavior change). Tasks 1.1–1.4.
- Stage 2: add boot audio events to catalogs (no UI change yet). Tasks 2.1–2.3.
- Stage 3: splash skeleton with stubbed boot pipeline. Tasks 3.1–3.10.
- Stage 4: real step Cmds + remove pre-`NewProgram` boot work. Tasks 4.1–4.10.
- Stage 5: failure UI + final tests + manual smoke. Tasks 5.1–5.4.

Stages 1 and 2 are independent and can ship in either order. Stage 3 depends on Stage 2. Stage 4 depends on Stages 1 and 3. Stage 5 depends on Stage 4.

**Conventions used in this plan:**
- File paths are absolute from repo root.
- All Go imports use the module path `github.com/tristanbietsch/rex`.
- Test commands assume `cd` to the repo root.
- Commits do not co-author Claude (per user preference).

---

## Stage 1: Extract `internal/daemonctl`

### Task 1.1: Create `daemonctl` package with `Reachable`

**Files:**
- Create: `internal/daemonctl/daemon.go`
- Create: `internal/daemonctl/daemon_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/daemonctl/daemon_test.go
package daemonctl

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReachable_NoSocket(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "missing.sock")
	require.False(t, Reachable(tmp))
}

func TestReachable_LiveSocket(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "live.sock")
	ln, err := net.Listen("unix", sock)
	require.NoError(t, err)
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	require.True(t, Reachable(sock))
}

func TestReachable_StaleSocketFile(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "stale.sock")
	require.NoError(t, os.WriteFile(sock, []byte{}, 0o644))
	require.False(t, Reachable(sock))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/daemonctl/... -run TestReachable -v`
Expected: build error (`undefined: Reachable`).

- [ ] **Step 3: Write minimal implementation**

```go
// internal/daemonctl/daemon.go

// Package daemonctl spawns, checks, and resolves the rex-daemon process.
// Shared between the CLI (rex daemon start) and the TUI boot splash.
package daemonctl

import (
	"net"
	"os"
	"path/filepath"
	"time"
)

// Reachable returns true when a unix socket connect succeeds quickly.
func Reachable(socket string) bool {
	conn, err := net.DialTimeout("unix", socket, 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// DefaultSocket returns the default UDS path, matching rex-daemon's logic.
func DefaultSocket() string {
	if r := os.Getenv("XDG_RUNTIME_DIR"); r != "" {
		return filepath.Join(r, "rex.sock")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "rex", "rex.sock")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/daemonctl/... -run TestReachable -v`
Expected: PASS — all three subtests green.

- [ ] **Step 5: Commit**

```bash
git add internal/daemonctl/daemon.go internal/daemonctl/daemon_test.go
git commit -m "daemonctl: extract Reachable + DefaultSocket"
```

---

### Task 1.2: Add `Start` and `FindBinary` to `daemonctl`

**Files:**
- Modify: `internal/daemonctl/daemon.go`
- Modify: `internal/daemonctl/daemon_test.go`

- [ ] **Step 1: Write failing tests**

Add to `internal/daemonctl/daemon_test.go`:

```go
import (
	// ... existing imports ...
	"os/exec"
)

func TestFindBinary_NextToSelf(t *testing.T) {
	dir := t.TempDir()
	candidate := filepath.Join(dir, "rex-daemon")
	require.NoError(t, os.WriteFile(candidate, []byte("#!/bin/sh\nexit 0\n"), 0o755))
	// Simulate "rex" living in dir by passing dir explicitly.
	got := findBinaryIn(dir)
	require.Equal(t, candidate, got)
}

func TestFindBinary_PathFallback(t *testing.T) {
	// If neither sibling nor PATH has it, returns the literal name.
	got := findBinaryIn(t.TempDir())
	if _, err := exec.LookPath("rex-daemon"); err != nil {
		require.Equal(t, "rex-daemon", got)
	}
}
```

- [ ] **Step 2: Run tests, expect fail**

Run: `go test ./internal/daemonctl/... -run TestFindBinary -v`
Expected: build error (`undefined: findBinaryIn`).

- [ ] **Step 3: Implement `FindBinary` + `Start`**

Append to `internal/daemonctl/daemon.go`:

```go
import (
	// add to existing import block:
	"fmt"
	"os/exec"
)

// FindBinary returns the path to rex-daemon. It checks (in order):
//   1) the same directory as the current rex executable,
//   2) PATH lookup,
//   3) the literal name "rex-daemon" (so exec gives a useful error).
func FindBinary() string {
	if self, err := os.Executable(); err == nil {
		if path := findBinaryIn(filepath.Dir(self)); path != "" {
			return path
		}
	}
	if path, err := exec.LookPath("rex-daemon"); err == nil {
		return path
	}
	return "rex-daemon"
}

// findBinaryIn is FindBinary's first probe, exposed for tests.
func findBinaryIn(dir string) string {
	candidate := filepath.Join(dir, "rex-daemon")
	info, err := os.Stat(candidate)
	if err == nil && !info.IsDir() {
		return candidate
	}
	return ""
}

// StartResult describes a successful daemon spawn.
type StartResult struct {
	PID     int
	Elapsed time.Duration
}

// Start spawns rex-daemon and waits up to ~2 s for the socket to appear.
// Returns nil StartResult on failure. The caller chooses the log file (pass nil
// to discard stderr).
func Start(socket string, stderrLog *os.File) (*StartResult, error) {
	t0 := time.Now()
	cmd := exec.Command(FindBinary())
	if stderrLog != nil {
		cmd.Stderr = stderrLog
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("spawn rex-daemon: %w", err)
	}
	for i := 0; i < 100; i++ {
		if Reachable(socket) {
			return &StartResult{PID: cmd.Process.Pid, Elapsed: time.Since(t0)}, nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return nil, fmt.Errorf("daemon started but socket %s didn't appear within 2s", socket)
}
```

- [ ] **Step 4: Run tests, expect pass**

Run: `go test ./internal/daemonctl/... -v`
Expected: all green.

- [ ] **Step 5: Commit**

```bash
git add internal/daemonctl/daemon.go internal/daemonctl/daemon_test.go
git commit -m "daemonctl: add Start + FindBinary"
```

---

### Task 1.3: Make `internal/cli/daemon.go` delegate to `daemonctl`

**Files:**
- Modify: `internal/cli/daemon.go`
- Modify: `internal/cli/tui.go`
- Modify: `internal/cli/socket.go`

- [ ] **Step 1: Replace inline helpers with delegation**

Edit `internal/cli/daemon.go`. Change:

```go
import (
	// existing imports + add:
	"github.com/tristanbietsch/rex/internal/daemonctl"
)

func daemonStart(args []string) error {
	_ = args
	socket := DefaultSocket()
	if daemonctl.Reachable(socket) {
		fmt.Println("rex-daemon already running")
		return nil
	}
	logf, _ := os.OpenFile(daemonLogPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	res, err := daemonctl.Start(socket, logf)
	if err != nil {
		return NewExitError(ExitGeneric, err.Error())
	}
	fmt.Printf("rex-daemon started (pid %d)\n", res.PID)
	return nil
}
```

Delete the old `findDaemonBinary` function from `internal/cli/daemon.go` (`daemonctl.FindBinary` replaces it).

- [ ] **Step 2: Update `internal/cli/tui.go` to delegate**

Replace the body of `RunTUI` in `internal/cli/tui.go` with:

```go
func RunTUI() error {
	rexlog.Init("tui")
	defer rexlog.Close()
	socket := DefaultSocket()
	slog.Info("tui: starting", "socket", socket)
	err := tui.Run(socket)
	if err != nil {
		slog.Error("tui: exited with error", "err", err)
	} else {
		slog.Info("tui: exited cleanly")
	}
	return err
}
```

Delete the `daemonReachable` function from `internal/cli/tui.go` (no longer used here). The boot-time daemon check moves into the splash pipeline in Stage 4.

- [ ] **Step 3: Make `DefaultSocket()` delegate to daemonctl**

Edit `internal/cli/socket.go`:

```go
package cli

import (
	"time"

	"github.com/tristanbietsch/rex/internal/daemonctl"
)

// DefaultSocket returns the default UDS path.
func DefaultSocket() string { return daemonctl.DefaultSocket() }

func deadlineSeconds(n int) time.Time {
	return time.Now().Add(time.Duration(n) * time.Second)
}

var _ = deadlineSeconds
```

- [ ] **Step 4: Build + existing tests pass**

Run: `go build ./... && go test ./...`
Expected: full build green; all existing tests still pass. **There is a known consequence: `rex` (no args) now skips the pre-program daemon spawn. The splash pipeline in Stage 4 puts it back. Until Stage 4 lands, `rex` will fail to dial the daemon if it isn't already running.** That is acceptable because Stage 4 follows immediately; do not ship without Stage 4.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/daemon.go internal/cli/tui.go internal/cli/socket.go
git commit -m "cli: delegate daemon spawn/check to daemonctl"
```

---

### Task 1.4: Build-validate Stage 1

- [ ] **Step 1: Run full build + test suite**

Run: `go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 2: Stage 1 done — no commit (already committed task-by-task)**

---

## Stage 2: Add boot audio events

### Task 2.1: Add `EventBoot*` constants

**Files:**
- Modify: `internal/audio/audio.go`

- [ ] **Step 1: Add the three constants**

In `internal/audio/audio.go`, expand the `Event names` block:

```go
const (
	EventStartup  = "startup"
	EventCreate   = "create"
	EventDone     = "done"
	EventDelete   = "delete"
	EventNav      = "nav"
	EventOpen     = "open"
	EventClose    = "close"
	EventCommand  = "command"
	EventFilter   = "filter"
	EventBootOK   = "boot_ok"
	EventBootWarn = "boot_warn"
	EventBootFail = "boot_fail"
)
```

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/audio/audio.go
git commit -m "audio: add EventBootOK/Warn/Fail constants"
```

---

### Task 2.2: Add factorio catalog entries

**Files:**
- Modify: `internal/audio/factorio.go`

- [ ] **Step 1: Append entries to `factorioCatalog`**

Inside the returned map (after the `EventFilter` entry, before the closing brace), add:

```go
		// Boot OK: a single quick mechanical click (think "lever locking into place").
		EventBootOK: {
			{tones: []tone{{1200, 18, 10}}},
		},
		// Boot WARN: damped lower thunk — softer, slightly off-pitch.
		EventBootWarn: {
			{tones: []tone{{660, 32, 22}}},
		},
		// Boot FAIL: dropped-belt clatter — two-step descending pair.
		EventBootFail: {
			{tones: []tone{{440, 30, 18}}},
			{tones: []tone{{220, 80, 60}, {110, 60, 40}}},
		},
```

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/audio/factorio.go
git commit -m "audio: factorio catalog entries for boot events"
```

---

### Task 2.3: Add evangelion catalog entries

**Files:**
- Modify: `internal/audio/evangelion.go`

- [ ] **Step 1: Append entries to `evangelionCatalog`**

Inside the returned map (after the existing entries, before the closing brace), add:

```go
		// Boot OK: high FM blip — NERV terminal "row checked".
		EventBootOK: {
			{tones: []tone{{2349, 20, 12}, {4699, 10, 5}}},
		},
		// Boot WARN: softer off-tuned blip with a slight tail.
		EventBootWarn: {
			{tones: []tone{{1568, 30, 22}, {1175, 20, 14}}},
		},
		// Boot FAIL: descending dissonant pair, klaxon edge, distinct from EventDelete.
		EventBootFail: {
			{tones: []tone{{880, 30, 18}, {1318, 18, 10}}},
			{tones: []tone{{440, 90, 70}, {220, 60, 45}}},
		},
```

- [ ] **Step 2: Run the bake-coverage test**

Run: `go test ./internal/audio/... -run TestBakeNonEmpty -v`
Expected: still PASS for `factorio` and `evangelion` (the existing events). The new events aren't in `allEvents` yet, so they aren't enforced.

- [ ] **Step 3: Extend `allEvents` to include the new boot events**

Edit `internal/audio/audio_test.go`:

```go
var allEvents = []string{
	EventStartup, EventCreate, EventDone, EventDelete,
	EventNav, EventOpen, EventClose, EventCommand, EventFilter,
	EventBootOK, EventBootWarn, EventBootFail,
}
```

- [ ] **Step 4: Re-run the bake-coverage test**

Run: `go test ./internal/audio/... -run TestBakeNonEmpty -v`
Expected: PASS for both `factorio` and `evangelion` — each new event has non-empty PCM in both catalogs.

- [ ] **Step 5: Commit**

```bash
git add internal/audio/evangelion.go internal/audio/audio_test.go
git commit -m "audio: evangelion catalog entries for boot events; enforce coverage"
```

---

## Stage 3: Splash skeleton with stubbed boot pipeline

The goal of this stage is to produce a visible splash that animates 17 fake `[ OK ]` rows over ~1.2 s and transitions to the board, with chimes firing. The daemon dial / Hello / Subscribe still happen pre-program (same as today) — that's replaced in Stage 4.

### Task 3.1: Add `FocusBoot` to the focus enum and Boot fields to `Model`

**Files:**
- Modify: `internal/tui/model.go`

- [ ] **Step 1: Add `FocusBoot` constant**

Edit the `const` block in `internal/tui/model.go`:

```go
const (
	FocusBoard Focus = iota
	FocusPrompt
	FocusCommand
	FocusWizard
	FocusHelp
	FocusConfirmQuit
	FocusConfirmDelete
	FocusSettings
	FocusAttach
	FocusFail
	FocusBoot
)
```

- [ ] **Step 2: Add Boot fields to `Model`**

Inside the `Model` struct, add at the end (before the closing brace):

```go
	// Boot / splash state.
	BootLog     []bootLine
	BootStep    int
	BootStart   time.Time
	BootMinDone bool
	BootDone    bool
	BootFailed  bool
	BootError   error
```

- [ ] **Step 3: Build (will fail — `bootLine` is undefined)**

Run: `go build ./internal/tui/...`
Expected: build error referencing `bootLine`.

- [ ] **Step 4: Stub `bootLine` so the build passes**

Create `internal/tui/splash.go` with the minimum needed for `model.go` to compile:

```go
package tui

// bootLine is one rendered row in the splash log.
type bootLine struct {
	Name   string
	Status bootStatus
	Desc   string
	Err    error
}

// bootStatus is the status token for a row.
type bootStatus int

const (
	stepOK bootStatus = iota
	stepFail
	stepWarn
	stepSkip
)
```

- [ ] **Step 5: Build**

Run: `go build ./internal/tui/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/model.go internal/tui/splash.go
git commit -m "tui: add FocusBoot + Boot fields to Model"
```

---

### Task 3.2: Define splash messages, sequence type, constants

**Files:**
- Modify: `internal/tui/splash.go`

- [ ] **Step 1: Add constants and message types**

Append to `internal/tui/splash.go`:

```go
import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Splash tuning constants.
const (
	bootMinDuration = 1200 * time.Millisecond
	bootInterStep   = 70 * time.Millisecond
	bootCategoryW   = 18 // fixed column width for the category name
)

// bootStepMsg is emitted by a step Cmd when it finishes.
type bootStepMsg struct {
	Name   string
	Status bootStatus
	Desc   string
	Dur    time.Duration
	Err    error
}

// bootMinElapsedMsg is the min-duration tick.
type bootMinElapsedMsg struct{}

// bootMinTick returns a Cmd that posts bootMinElapsedMsg after bootMinDuration.
func bootMinTick() tea.Cmd {
	return tea.Tick(bootMinDuration, func(time.Time) tea.Msg { return bootMinElapsedMsg{} })
}

// stepFunc builds a step Cmd given the current model snapshot.
type stepFunc func(m Model) tea.Cmd
```

- [ ] **Step 2: Build**

Run: `go build ./internal/tui/...`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/splash.go
git commit -m "tui: splash message types, sequence type, constants"
```

---

### Task 3.3: Status bracket styles (reuse existing palette)

**Files:**
- Modify: `internal/tui/styles.go`

- [ ] **Step 1: Add splash styles, theme-aware**

Extend `internal/tui/styles.go`. Add new style vars to the live-styles block:

```go
var (
	// ... existing style vars ...

	styleBootOK     lipgloss.Style
	styleBootFail   lipgloss.Style
	styleBootWarn   lipgloss.Style
	styleBootSkip   lipgloss.Style
	styleBootRun    lipgloss.Style
	styleBootBracket lipgloss.Style
	styleBootHeader lipgloss.Style
	styleBootReady  lipgloss.Style
	styleBootCause  lipgloss.Style
)
```

In `rebuildStyles()`, after the existing assignments, add:

```go
	styleBootOK = lipgloss.NewStyle().Bold(true).Foreground(colorDone)
	styleBootFail = lipgloss.NewStyle().Bold(true).Foreground(colorFailed)
	styleBootWarn = lipgloss.NewStyle().Foreground(colorNeeds)
	styleBootSkip = lipgloss.NewStyle().Foreground(colorFgMuted)
	styleBootRun = lipgloss.NewStyle().Foreground(colorWorking)
	styleBootBracket = lipgloss.NewStyle().Foreground(colorFgMuted)
	styleBootHeader = lipgloss.NewStyle().Bold(true).Foreground(colorFgPrimary)
	styleBootReady = lipgloss.NewStyle().Foreground(colorDone)
	styleBootCause = lipgloss.NewStyle().Foreground(colorFailed)
```

- [ ] **Step 2: Build**

Run: `go build ./internal/tui/...`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/styles.go
git commit -m "tui: splash status styles, theme-aware via existing palette"
```

---

### Task 3.4: Implement `renderSplash` — initial frame

**Files:**
- Modify: `internal/tui/splash.go`
- Create: `internal/tui/splash_snapshot_test.go`

- [ ] **Step 1: Write the failing snapshot test**

Create `internal/tui/splash_snapshot_test.go`:

```go
package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSplashInitialFrame(t *testing.T) {
	m := Model{
		Focus:     FocusBoot,
		Width:     80,
		Height:    24,
		BootStart: time.Now().Add(-470 * time.Millisecond), // 0.47s elapsed
	}
	out := renderSplash(m, m.Width, m.Height)
	lines := strings.Split(out, "\n")

	require.GreaterOrEqual(t, len(lines), 4)
	require.Contains(t, lines[1], "レックス")
	require.Contains(t, lines[1], "rex runtime executive")
	require.Contains(t, lines[2], "起動中")
	require.Contains(t, lines[2], "booting")
	// No log rows yet — the third row should be blank.
	require.Equal(t, "", strings.TrimSpace(lines[3]))
}
```

- [ ] **Step 2: Run test, expect fail**

Run: `go test ./internal/tui/... -run TestSplashInitialFrame -v`
Expected: build error (`undefined: renderSplash`).

- [ ] **Step 3: Implement minimal `renderSplash`**

Append to `internal/tui/splash.go`:

```go
import (
	// add to import block:
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// renderSplash renders the boot splash, full-screen, no overlay.
func renderSplash(m Model, w, h int) string {
	leftPad := "  "
	header := leftPad + styleBootHeader.Render("∴ レックス") +
		styleDim.Render(" · rex runtime executive")

	elapsed := time.Since(m.BootStart).Truncate(10 * time.Millisecond)
	statusLine := leftPad + renderSplashStatusLine(m, elapsed)

	rows := []string{
		"",
		header,
		statusLine,
		"",
	}
	for _, ln := range m.BootLog {
		rows = append(rows, leftPad+renderBootLine(ln))
	}
	if m.BootDone && !m.BootFailed {
		ready := fmt.Sprintf("準備完了 ready · %s", elapsed)
		rows = append(rows, "", lipgloss.PlaceHorizontal(w-2, lipgloss.Right, styleBootReady.Render(ready)))
	}
	for len(rows) < h {
		rows = append(rows, "")
	}
	if len(rows) > h {
		rows = rows[:h]
	}
	return strings.Join(rows, "\n")
}

// renderSplashStatusLine produces either the running header or, on failure,
// the freeze-state header.
func renderSplashStatusLine(m Model, elapsed time.Duration) string {
	if m.BootFailed {
		return styleBootFail.Render("起動失敗 boot failed")
	}
	return styleDim.Render(fmt.Sprintf("起動中 booting...  %s", elapsed))
}

// renderBootLine renders one log row: "[ STATUS ] <category> · <desc>".
func renderBootLine(ln bootLine) string {
	token := bootStatusToken(ln.Status)
	cat := padCategory(ln.Name)
	desc := ln.Desc
	return token + " " + stylePrimary.Render(cat) + "  " +
		styleDim.Render("·") + " " + stylePrimary.Render(desc)
}

// bootStatusToken returns the 8-char "[ XX ]" status fragment with color.
func bootStatusToken(s bootStatus) string {
	inner := func(text string, style lipgloss.Style) string {
		return styleBootBracket.Render("[") + " " + style.Render(text) + " " + styleBootBracket.Render("]")
	}
	switch s {
	case stepOK:
		return inner("  OK  ", styleBootOK)
	case stepFail:
		return inner(" FAIL ", styleBootFail)
	case stepWarn:
		return inner(" WARN ", styleBootWarn)
	case stepSkip:
		return inner(" SKIP ", styleBootSkip)
	}
	return inner("  ..  ", styleBootRun)
}

// padCategory left-pads the category name to bootCategoryW cells, east-asian-width aware.
func padCategory(name string) string {
	w := ansi.StringWidth(name)
	if w >= bootCategoryW {
		return name
	}
	return name + strings.Repeat(" ", bootCategoryW-w)
}
```

- [ ] **Step 4: Run test, expect pass**

Run: `go test ./internal/tui/... -run TestSplashInitialFrame -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/splash.go internal/tui/splash_snapshot_test.go
git commit -m "tui: renderSplash initial frame + snapshot test"
```

---

### Task 3.5: Snapshot test — mid-boot rows

**Files:**
- Modify: `internal/tui/splash_snapshot_test.go`

- [ ] **Step 1: Add failing snapshot test**

Append to `internal/tui/splash_snapshot_test.go`:

```go
func TestSplashMidBoot(t *testing.T) {
	m := Model{
		Focus:     FocusBoot,
		Width:     80,
		Height:    24,
		BootStart: time.Now().Add(-300 * time.Millisecond),
		BootLog: []bootLine{
			{Name: "log.init", Status: stepOK, Desc: "~/.local/state/rex/tui.log"},
			{Name: "paths.ensure", Status: stepOK, Desc: "config + state dirs ok"},
			{Name: "tty.probe", Status: stepOK, Desc: "198×52 · truecolor · en_US.UTF-8"},
		},
	}
	out := renderSplash(m, m.Width, m.Height)
	require.Contains(t, out, "[")
	require.Contains(t, out, "OK")
	require.Contains(t, out, "log.init")
	require.Contains(t, out, "paths.ensure")
	require.Contains(t, out, "tty.probe")
	// No ready footer yet (BootDone=false).
	require.NotContains(t, out, "準備完了")
}
```

- [ ] **Step 2: Run, expect pass**

Run: `go test ./internal/tui/... -run TestSplashMidBoot -v`
Expected: PASS — `renderSplash` already supports this.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/splash_snapshot_test.go
git commit -m "tui: snapshot test for mid-boot splash"
```

---

### Task 3.6: Snapshot test — all OK + ready footer

**Files:**
- Modify: `internal/tui/splash_snapshot_test.go`

- [ ] **Step 1: Add failing test**

Append:

```go
func TestSplashAllOKShowsReady(t *testing.T) {
	m := Model{
		Focus:       FocusBoot,
		Width:       80,
		Height:      30,
		BootStart:   time.Now().Add(-1340 * time.Millisecond),
		BootDone:    true,
		BootMinDone: false, // ready footer should show even before min elapsed
		BootLog: []bootLine{
			{Name: "log.init", Status: stepOK, Desc: "ok"},
			{Name: "handshake", Status: stepOK, Desc: "接続 · rex-tui"},
			{Name: "subscribe", Status: stepOK, Desc: "受信中 · stream open"},
		},
	}
	out := renderSplash(m, m.Width, m.Height)
	require.Contains(t, out, "準備完了 ready")
	require.Contains(t, out, "接続")
	require.Contains(t, out, "受信中")
}
```

- [ ] **Step 2: Run, expect pass**

Run: `go test ./internal/tui/... -run TestSplashAllOKShowsReady -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/splash_snapshot_test.go
git commit -m "tui: snapshot test for ready footer + japanese rows"
```

---

### Task 3.7: Wire `FocusBoot` into `Model.View`

**Files:**
- Modify: `internal/tui/tui.go`

- [ ] **Step 1: Add `FocusBoot` case**

In `internal/tui/tui.go`'s `Model.View()` switch, add a new case before the existing ones:

```go
	switch m.Focus {
	case FocusBoot:
		return renderSplash(m, w, h)
	case FocusWizard:
		return centerOverlay(w, h, renderWizard(m), renderFullScreen(m, w, h))
	// ... existing cases ...
	}
```

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/tui.go
git commit -m "tui: route FocusBoot to renderSplash"
```

---

### Task 3.8: Add `handleBootStepMsg` and `handleBootMinElapsedMsg` to `Update`

**Files:**
- Modify: `internal/tui/update.go`
- Modify: `internal/tui/splash.go`

- [ ] **Step 1: Add `nextStep` helper and `handOffToBoard`**

Append to `internal/tui/splash.go`:

```go
import (
	// add to import block:
	"github.com/tristanbietsch/rex/internal/audio"
)

// bootSequence is populated by Stage 4 (real Cmds). Stage 3 uses a stub.
// Stage 3 stub builds 17 fake step msgs separated by bootInterStep ticks.
var bootSequence []stepFunc

// nextStep returns a Cmd that produces bootStepMsg #BootStep, then schedules
// the inter-step delay before the *next* step's Cmd. Returns nil when the
// pipeline is complete.
func nextStep(m Model) tea.Cmd {
	if m.BootStep >= len(bootSequence) {
		return nil
	}
	step := bootSequence[m.BootStep]
	return step(m)
}

// delayThen schedules a tea.Tick of bootInterStep, then dispatches inner.
// Used to space step messages so the splash cascades visibly.
func delayThen(inner tea.Cmd) tea.Cmd {
	return tea.Tick(bootInterStep, func(time.Time) tea.Msg {
		if inner == nil {
			return nil
		}
		return inner()
	})
}

// chimeFor maps a status to the audio event name; "" for SKIP (silent).
func chimeFor(s bootStatus) string {
	switch s {
	case stepOK:
		return audio.EventBootOK
	case stepWarn:
		return audio.EventBootWarn
	case stepFail:
		return audio.EventBootFail
	}
	return ""
}

// handOffToBoard transitions from FocusBoot to FocusBoard, releases the boot
// log, plays the startup chime, and starts the daemon-event listener.
func (m Model) handOffToBoard() (Model, tea.Cmd) {
	m.Focus = FocusBoard
	m.BootLog = nil
	if m.Audio != nil {
		m.Audio.Play(audio.EventStartup)
	}
	cmds := []tea.Cmd{tea.HideCursor, tickSpinner()}
	if m.Client != nil {
		cmds = append(cmds, listenDaemon(m.Client))
	}
	return m, tea.Batch(cmds...)
}
```

- [ ] **Step 2: Handle the boot messages in Update**

Edit `internal/tui/update.go`. Find the top-level `switch msg := msg.(type)` in `Update`. Add two new cases (positioning before the default / existing cases is fine):

```go
	case bootStepMsg:
		m.BootLog = append(m.BootLog, bootLine{
			Name: msg.Name, Status: msg.Status, Desc: msg.Desc, Err: msg.Err,
		})
		if msg.Status == stepFail {
			m.BootFailed = true
			m.BootError = msg.Err
			if m.Audio != nil {
				m.Audio.Play(audio.EventBootFail)
			}
			return m, nil
		}
		if ev := chimeFor(msg.Status); ev != "" && m.Audio != nil {
			m.Audio.Play(ev)
		}
		m.BootStep++
		if m.BootStep >= len(bootSequence) {
			m.BootDone = true
			if m.BootMinDone {
				return m.handOffToBoard()
			}
			return m, nil
		}
		return m, delayThen(nextStep(m))

	case bootMinElapsedMsg:
		m.BootMinDone = true
		if m.BootDone && !m.BootFailed {
			return m.handOffToBoard()
		}
		return m, nil
```

- [ ] **Step 3: Build (will fail — `bootSequence` empty so the splash hangs at step 0; we wire stubs next)**

Run: `go build ./...`
Expected: PASS (build is green; runtime will hang on splash, which we'll exercise once stubs land).

- [ ] **Step 4: Commit**

```bash
git add internal/tui/update.go internal/tui/splash.go
git commit -m "tui: Update handles bootStepMsg + bootMinElapsedMsg"
```

---

### Task 3.9: Stub `bootSequence` with 17 fake steps

**Files:**
- Create: `internal/tui/splash_steps.go`

- [ ] **Step 1: Add stub step functions**

Create `internal/tui/splash_steps.go`:

```go
package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// fakeStep returns a stepFunc that synthesizes a successful bootStepMsg after a
// tiny tick. Used during Stage 3 to validate the splash visuals + audio without
// the real boot pipeline yet.
func fakeStep(name, desc string) stepFunc {
	return func(_ Model) tea.Cmd {
		return tea.Tick(time.Millisecond, func(time.Time) tea.Msg {
			return bootStepMsg{Name: name, Status: stepOK, Desc: desc}
		})
	}
}

func init() {
	bootSequence = []stepFunc{
		fakeStep("log.init", "~/.local/state/rex/tui.log"),
		fakeStep("paths.ensure", "config + state dirs ok"),
		fakeStep("tty.probe", "198×52 · truecolor · en_US.UTF-8"),
		fakeStep("settings.load", "~/.config/rex/config.yaml"),
		fakeStep("theme.apply", "default"),
		fakeStep("audio.init", "soundset=factorio · vol=0.80"),
		fakeStep("audio.load", "12 cues · 起動準備"),
		fakeStep("registry.load", "8 tools (5 enabled · 3 hidden)"),
		fakeStep("keymap.bind", "42 bindings"),
		fakeStep("socket.resolve", "/tmp/rex/rex.sock"),
		fakeStep("daemon", "already running (pid 0)"),
		fakeStep("client.dial", "connected · 0ms"),
		fakeStep("handshake", "接続 · rex-tui"),
		fakeStep("subscribe", "受信中 · event stream open"),
		fakeStep("snapshot.parse", "0 sessions"),
		fakeStep("state.restore", "first run"),
		fakeStep("renderer.warm", "styles cached"),
	}
}
```

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/splash_steps.go
git commit -m "tui: stub bootSequence with 17 fake steps"
```

---

### Task 3.10: Wire splash into `tui.Run` + visual smoke test

**Files:**
- Modify: `internal/tui/tui.go`

- [ ] **Step 1: Start the program in `FocusBoot`**

Edit `internal/tui/tui.go`. In `Run`, before `tea.NewProgram`, set up the splash. Also dispatch the first step + the min-duration tick from `Init`. Replace the existing `Run` body with:

```go
func Run(socket string) error {
	c, err := client.Dial(socket)
	if err != nil {
		return fmt.Errorf("dial daemon: %w", err)
	}
	defer c.Close()

	snap, err := c.Hello("rex-tui")
	if err != nil {
		return fmt.Errorf("hello: %w", err)
	}
	if err := c.Subscribe(""); err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}

	store := settings.NewStore()
	storePath := settings.DefaultPath()
	_ = store.Load(storePath)

	m := Model{
		Client:    c,
		Socket:    socket,
		Focus:     FocusBoot, // start in splash
		Sessions:  snap.Sessions,
		Filter:    "all",
		Store:     store,
		StorePath: storePath,
		BootStart: time.Now(),
	}

	if scheme, _ := store.Get("color_scheme").(string); scheme != "" {
		applyTheme(scheme)
	}

	soundEnabled, _ := store.Get("sound_enabled").(bool)
	soundset, _ := store.Get("soundset").(string)
	volume, _ := store.Get("master_volume").(float64)
	if soundset == "off" {
		soundEnabled = false
	}
	m.Audio = audio.New(audio.Config{Enabled: soundEnabled, Volume: volume, Soundset: soundset})
	// Startup chime now fires on hand-off, not here.

	if sel, filt, ok := LoadTUIState(); ok {
		m.SelectedID = sel
		if filt != "" {
			m.Filter = filt
		}
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}
```

Add `"time"` to the import block if not already present.

- [ ] **Step 2: Dispatch first step + min tick from `Init`**

Replace `Model.Init` in `internal/tui/tui.go`:

```go
func (m Model) Init() tea.Cmd {
	if m.Focus == FocusBoot {
		first := nextStep(m)
		return tea.Batch(tea.HideCursor, bootMinTick(), delayThen(first), tickSpinner())
	}
	return tea.Batch(tea.HideCursor, listenDaemon(m.Client), tickSpinner())
}
```

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: PASS.

- [ ] **Step 4: Visual smoke (manual)**

If you have a daemon running:

```bash
pkill rex-daemon 2>/dev/null; rex daemon start; go run ./cmd/rex
```

Expected behavior:
- alt-screen activates
- splash header + spinner visible
- 17 `[ OK ]` rows cascade in over ~1.2 s with ~70 ms spacing
- chime plays per row if audio is enabled and the soundset is supported
- `準備完了 ready · <total>` line appears at the bottom right
- screen transitions to the rex board after the min-duration window
- `q` quits cleanly

If quitting leaves alt-screen residue, run `reset` and re-check `tea.WithAltScreen()` is intact in `Run`.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/tui.go
git commit -m "tui: launch program in FocusBoot, dispatch first step + min tick"
```

---

## Stage 4: Real step Cmds + boot-pipeline rewiring

This stage replaces the 17 stub steps with real Cmds that do the actual boot work, and moves the daemon dial / Hello / Subscribe out of pre-`NewProgram` and into the splash pipeline.

### Task 4.1: Add `audioPlayer` interface so steps + tests don't need `*audio.Player`

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/splash.go`

- [ ] **Step 1: Introduce the interface**

In `internal/tui/model.go`, change the `Audio` field type:

```go
	// Audio is the active soundset player. Interface so tests can swap a recording fake.
	Audio audioPlayer
```

And add the interface near the type's definition (anywhere in the file):

```go
// audioPlayer is the subset of *audio.Player the TUI uses.
type audioPlayer interface {
	Play(event string)
}
```

- [ ] **Step 2: Build (will fail in places that assigned `*audio.Player`)**

Run: `go build ./...`
Expected: likely fails at the assignment in `tui.Run` because `audio.New(...)` returns `*audio.Player`. `*audio.Player` implements `Play(string)` so the interface is satisfied; the explicit assignment is fine.

If a build error appears for a different file (e.g. existing test or settings code), it's that code calling a `*audio.Player`-only method. Confirm by grepping:

```bash
grep -n 'm\.Audio\.' internal/tui/
grep -n 'Audio\s*\*audio' internal/
```

Audit any callers and either keep them using the concrete `*audio.Player` through the (now-interface) field via type assertion, OR widen the interface here. The current rex code only calls `Play(string)`, so this should compile.

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/model.go
git commit -m "tui: audioPlayer interface on Model.Audio"
```

---

### Task 4.2: Implement `step` Cmds that don't need the client

Replace stub steps with real Cmds in groups. Each step is a `stepFunc`.

**Files:**
- Modify: `internal/tui/splash_steps.go`

- [ ] **Step 1: Add real step implementations (log.init through socket.resolve)**

Replace the contents of `internal/tui/splash_steps.go` with:

```go
package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tristanbietsch/rex/internal/audio"
	"github.com/tristanbietsch/rex/internal/daemonctl"
	"github.com/tristanbietsch/rex/internal/registry"
	"github.com/tristanbietsch/rex/internal/rexlog"
	"github.com/tristanbietsch/rex/internal/settings"
)

// emit wraps a synchronous step in a tea.Cmd.
func emit(name string, fn func() (bootStatus, string, error)) tea.Cmd {
	return func() tea.Msg {
		t0 := time.Now()
		status, desc, err := fn()
		return bootStepMsg{
			Name:   name,
			Status: status,
			Desc:   desc,
			Dur:    time.Since(t0),
			Err:    err,
		}
	}
}

func stepLogInit(_ Model) tea.Cmd {
	return emit("log.init", func() (bootStatus, string, error) {
		rexlog.Init("tui")
		home, _ := os.UserHomeDir()
		return stepOK, filepath.Join(home, ".local/state/rex/tui.log"), nil
	})
}

func stepPathsEnsure(_ Model) tea.Cmd {
	return emit("paths.ensure", func() (bootStatus, string, error) {
		home, _ := os.UserHomeDir()
		paths := []string{
			filepath.Join(home, ".config", "rex"),
			filepath.Join(home, ".local", "state", "rex"),
		}
		for _, p := range paths {
			if err := os.MkdirAll(p, 0o755); err != nil {
				return stepFail, fmt.Sprintf("mkdir %s: %v", p, err), err
			}
		}
		return stepOK, "config + state dirs ok", nil
	})
}

func stepTTYProbe(m Model) tea.Cmd {
	return emit("tty.probe", func() (bootStatus, string, error) {
		lc := os.Getenv("LC_ALL")
		if lc == "" {
			lc = os.Getenv("LANG")
		}
		if lc == "" {
			lc = "C"
		}
		color := os.Getenv("COLORTERM")
		if color == "" {
			color = "256-color"
		}
		if !strings.Contains(strings.ToLower(lc), "utf-8") && !strings.Contains(strings.ToLower(lc), "utf8") {
			return stepWarn, fmt.Sprintf("%dx%d · %s · %s (ascii fallback)", m.Width, m.Height, color, lc), nil
		}
		return stepOK, fmt.Sprintf("%dx%d · %s · %s", m.Width, m.Height, color, lc), nil
	})
}

func stepSettingsLoad(_ Model) tea.Cmd {
	return emit("settings.load", func() (bootStatus, string, error) {
		path := settings.DefaultPath()
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return stepSkip, "no config file (defaults)", nil
		}
		s := settings.NewStore()
		if err := s.Load(path); err != nil {
			return stepWarn, fmt.Sprintf("%v (defaults)", err), err
		}
		return stepOK, path, nil
	})
}

func stepThemeApply(m Model) tea.Cmd {
	return emit("theme.apply", func() (bootStatus, string, error) {
		if m.Store == nil {
			return stepSkip, "default", nil
		}
		scheme, _ := m.Store.Get("color_scheme").(string)
		if scheme == "" {
			return stepSkip, "default", nil
		}
		applyTheme(scheme)
		return stepOK, scheme, nil
	})
}

func stepAudioInit(m Model) tea.Cmd {
	return emit("audio.init", func() (bootStatus, string, error) {
		if m.Store == nil || m.Audio == nil {
			return stepSkip, "no audio configured", nil
		}
		enabled, _ := m.Store.Get("sound_enabled").(bool)
		soundset, _ := m.Store.Get("soundset").(string)
		vol, _ := m.Store.Get("master_volume").(float64)
		if !enabled || soundset == "off" {
			return stepSkip, "muted", nil
		}
		return stepOK, fmt.Sprintf("soundset=%s · vol=%.2f", soundset, vol), nil
	})
}

func stepAudioLoad(m Model) tea.Cmd {
	return emit("audio.load", func() (bootStatus, string, error) {
		if m.Store == nil || m.Audio == nil {
			return stepSkip, "muted", nil
		}
		soundset, _ := m.Store.Get("soundset").(string)
		if soundset == "" || soundset == "off" {
			return stepSkip, "muted", nil
		}
		// Re-apply soundset on the live player (the pre-bootstrap built it with
		// whatever it could read; this is the canonical apply).
		if p, ok := m.Audio.(*audio.Player); ok {
			p.SetSoundset(soundset)
		}
		return stepOK, fmt.Sprintf("%s · 起動準備", soundset), nil
	})
}

func stepRegistryLoad(_ Model) tea.Cmd {
	return emit("registry.load", func() (bootStatus, string, error) {
		// Pass empty path: loads built-in registry only (no user overlay needed for boot).
		reg, err := registry.Load("")
		if err != nil {
			return stepFail, err.Error(), err
		}
		enabled, hidden := 0, 0
		for _, t := range reg.Tools {
			if t.EnabledByDefault {
				enabled++
			} else {
				hidden++
			}
		}
		desc := fmt.Sprintf("%d tools (%d enabled · %d hidden)", len(reg.Tools), enabled, hidden)
		return stepOK, desc, nil
	})
}

func stepKeymapBind(_ Model) tea.Cmd {
	return emit("keymap.bind", func() (bootStatus, string, error) {
		// Bindings live in keymap.go; counting them is purely cosmetic. We use a
		// sentinel count that the existing code path doesn't lie about (the
		// keymap is initialized package-level so binding has effectively
		// "already happened" by the time this step runs).
		return stepOK, "global + board + wizard bindings ready", nil
	})
}

func stepSocketResolve(m Model) tea.Cmd {
	return emit("socket.resolve", func() (bootStatus, string, error) {
		if m.Socket == "" {
			return stepFail, "socket path empty", fmt.Errorf("socket path empty")
		}
		return stepOK, m.Socket, nil
	})
}
```

Look up `Tool.EnabledByDefault` field — it should already exist on the Tool struct in `internal/registry/types.go`. If not, add it there to match the YAML field `enabled_by_default`:

```go
// internal/registry/types.go (if not already present)
type Tool struct {
	// ... existing fields ...
	EnabledByDefault bool `yaml:"enabled_by_default"`
}
```

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: PASS. If `Tool.EnabledByDefault` doesn't exist yet, add it per the snippet above and rebuild.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/splash_steps.go internal/registry/types.go
git commit -m "tui: real step Cmds for log/paths/tty/settings/theme/audio/registry/keymap/socket"
```

---

### Task 4.3: Implement client-dependent step Cmds

**Files:**
- Modify: `internal/tui/splash_steps.go`

- [ ] **Step 1: Add daemon / dial / handshake / subscribe / snapshot.parse / state.restore / renderer.warm**

Append to `internal/tui/splash_steps.go`:

```go
import (
	// add to import block:
	"github.com/tristanbietsch/rex/internal/client"
	"github.com/tristanbietsch/rex/internal/protocol"
)

// daemonStartedMsg is an internal helper produced by stepDaemon when spawn
// happened (so we can show pid + elapsed). For Stage 4 we just stuff it into
// the bootStepMsg.Desc; no separate msg type needed.

func stepDaemon(m Model) tea.Cmd {
	return emit("daemon", func() (bootStatus, string, error) {
		if daemonctl.Reachable(m.Socket) {
			return stepOK, "already running", nil
		}
		home, _ := os.UserHomeDir()
		logPath := filepath.Join(home, ".local", "state", "rex", "daemon.log")
		logf, _ := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		res, err := daemonctl.Start(m.Socket, logf)
		if err != nil {
			return stepFail, err.Error(), err
		}
		return stepOK, fmt.Sprintf("started · pid %d · %s", res.PID, res.Elapsed.Truncate(time.Millisecond)), nil
	})
}

// dialResultMsg carries the dialed client back into the model so later steps
// can use it. Dispatched via a separate msg because tea.Cmd can't update Model
// directly. We send this BEFORE the bootStepMsg so the model has the client
// when the next step (handshake) dispatches.
type dialResultMsg struct {
	C   *client.Client
	Err error
	Dur time.Duration
}

func stepClientDial(m Model) tea.Cmd {
	return func() tea.Msg {
		t0 := time.Now()
		c, err := client.Dial(m.Socket)
		return dialResultMsg{C: c, Err: err, Dur: time.Since(t0)}
	}
}

// snapshotResultMsg carries the snapshot back so snapshot.parse can compute counts.
type snapshotResultMsg struct {
	Snap *protocol.Snapshot
	Err  error
	Dur  time.Duration
}

func stepHandshake(m Model) tea.Cmd {
	return func() tea.Msg {
		if m.Client == nil {
			return bootStepMsg{Name: "handshake", Status: stepFail, Err: fmt.Errorf("client not dialed")}
		}
		t0 := time.Now()
		snap, err := m.Client.Hello("rex-tui")
		return snapshotResultMsg{Snap: snap, Err: err, Dur: time.Since(t0)}
	}
}

func stepSubscribe(m Model) tea.Cmd {
	return emit("subscribe", func() (bootStatus, string, error) {
		if m.Client == nil {
			return stepFail, "client not dialed", fmt.Errorf("client not dialed")
		}
		if err := m.Client.Subscribe(""); err != nil {
			return stepFail, err.Error(), err
		}
		return stepOK, "受信中 · event stream open", nil
	})
}

func stepSnapshotParse(m Model) tea.Cmd {
	return emit("snapshot.parse", func() (bootStatus, string, error) {
		if len(m.Sessions) == 0 {
			return stepSkip, "no sessions yet", nil
		}
		needs, work, done := 0, 0, 0
		for _, s := range m.Sessions {
			switch s.State {
			case protocol.StateNeedsInput:
				needs++
			case protocol.StateRunning, protocol.StateQueued:
				work++
			case protocol.StateDone:
				done++
			}
		}
		desc := fmt.Sprintf("%d sessions · %d needs · %d work · %d done", len(m.Sessions), needs, work, done)
		return stepOK, desc, nil
	})
}

func stepStateRestore(_ Model) tea.Cmd {
	return emit("state.restore", func() (bootStatus, string, error) {
		sel, filt, ok := LoadTUIState()
		if !ok {
			return stepSkip, "first run", nil
		}
		return stepOK, fmt.Sprintf("selected=%s · filter=%s", short8(sel), filt), nil
	})
}

func short8(s string) string {
	if len(s) <= 8 {
		return s
	}
	return s[:8]
}

func stepRendererWarm(_ Model) tea.Cmd {
	return emit("renderer.warm", func() (bootStatus, string, error) {
		// Force re-eval of style vars to pre-bake any lazy state.
		rebuildStyles()
		return stepOK, "styles cached", nil
	})
}
```

Look up the `protocol.State*` constants — confirm `StateNeedsInput`, `StateRunning`, `StateQueued`, `StateDone` are the actual names by:

```bash
grep -n "type State " internal/protocol/*.go
grep -n "State[A-Z]" internal/protocol/*.go | head -10
```

Substitute the real names if any differ. Also verify `Tool.EnabledByDefault` from Task 4.2's `registry/types.go` actually matches the YAML's `enabled_by_default` key — if the existing field name differs, use that name in `stepRegistryLoad`.

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: PASS. Address any mismatched constant names per the lookups above.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/splash_steps.go
git commit -m "tui: real step Cmds for daemon/dial/handshake/subscribe/snapshot/state/renderer"
```

---

### Task 4.4: Handle `dialResultMsg` and `snapshotResultMsg` in Update

**Files:**
- Modify: `internal/tui/update.go`

- [ ] **Step 1: Add msg handlers**

In `internal/tui/update.go`'s top-level switch, add cases (before `bootStepMsg`):

```go
	case dialResultMsg:
		if msg.Err != nil {
			return m.appendBootStep(bootStepMsg{
				Name: "client.dial", Status: stepFail, Err: msg.Err, Desc: msg.Err.Error(), Dur: msg.Dur,
			})
		}
		m.Client = msg.C
		return m.appendBootStep(bootStepMsg{
			Name: "client.dial", Status: stepOK,
			Desc: fmt.Sprintf("connected · %s", msg.Dur.Truncate(time.Millisecond)),
			Dur:  msg.Dur,
		})

	case snapshotResultMsg:
		if msg.Err != nil {
			return m.appendBootStep(bootStepMsg{
				Name: "handshake", Status: stepFail, Err: msg.Err, Desc: msg.Err.Error(), Dur: msg.Dur,
			})
		}
		m.Sessions = msg.Snap.Sessions
		if msg.Snap.Filter != "" {
			m.Filter = msg.Snap.Filter
		}
		return m.appendBootStep(bootStepMsg{
			Name: "handshake", Status: stepOK,
			Desc: "接続 · rex-tui",
			Dur:  msg.Dur,
		})
```

`appendBootStep` is shared logic between these special-cased msgs and the regular `bootStepMsg` path. Extract it.

- [ ] **Step 2: Extract `appendBootStep`**

Replace the existing `case bootStepMsg:` body with a call to a helper, and add the helper. Put the helper at the bottom of `internal/tui/update.go`:

```go
	case bootStepMsg:
		return m.appendBootStep(msg)
```

```go
// appendBootStep runs the shared "append row, chime, advance, maybe handoff"
// logic for both bootStepMsg and the special msgs (dial/snapshot) that produce
// a row plus carry side data into Model.
func (m Model) appendBootStep(msg bootStepMsg) (tea.Model, tea.Cmd) {
	m.BootLog = append(m.BootLog, bootLine{
		Name: msg.Name, Status: msg.Status, Desc: msg.Desc, Err: msg.Err,
	})
	if msg.Status == stepFail {
		m.BootFailed = true
		m.BootError = msg.Err
		if m.Audio != nil {
			m.Audio.Play(audio.EventBootFail)
		}
		return m, nil
	}
	if ev := chimeFor(msg.Status); ev != "" && m.Audio != nil {
		m.Audio.Play(ev)
	}
	m.BootStep++
	if m.BootStep >= len(bootSequence) {
		m.BootDone = true
		if m.BootMinDone {
			return m.handOffToBoard()
		}
		return m, nil
	}
	return m, delayThen(nextStep(m))
}
```

Add the `audio` import to `internal/tui/update.go` if it's not already there:

```go
import (
	// existing imports +:
	"github.com/tristanbietsch/rex/internal/audio"
	"fmt"
	"time"
)
```

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/update.go
git commit -m "tui: dial/snapshot result msgs feed appendBootStep"
```

---

### Task 4.5: Replace `bootSequence` with real Cmds

**Files:**
- Modify: `internal/tui/splash_steps.go`

- [ ] **Step 1: Update `init()` to use real step funcs**

Replace the `init()` block in `internal/tui/splash_steps.go` with:

```go
func init() {
	bootSequence = []stepFunc{
		stepLogInit,
		stepPathsEnsure,
		stepTTYProbe,
		stepSettingsLoad,
		stepThemeApply,
		stepAudioInit,
		stepAudioLoad,
		stepRegistryLoad,
		stepKeymapBind,
		stepSocketResolve,
		stepDaemon,
		stepClientDial,
		stepHandshake,
		stepSubscribe,
		stepSnapshotParse,
		stepStateRestore,
		stepRendererWarm,
	}
}
```

Delete the `fakeStep` helper and its earlier `init()` (since it's replaced).

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/splash_steps.go
git commit -m "tui: bootSequence wired to real step Cmds"
```

---

### Task 4.6: Move pre-`NewProgram` boot work into the splash, add pre-bootstrap audio

**Files:**
- Modify: `internal/tui/tui.go`

- [ ] **Step 1: Replace `Run` with the new boot-into-splash flow**

Replace the body of `Run` in `internal/tui/tui.go`:

```go
func Run(socket string) error {
	// Synchronous pre-bootstrap: read audio prefs so the Player exists by the
	// time the first step msg arrives. Best-effort; defaults on error.
	store := settings.NewStore()
	_ = store.Load(settings.DefaultPath())
	soundEnabled, _ := store.Get("sound_enabled").(bool)
	soundset, _ := store.Get("soundset").(string)
	volume, _ := store.Get("master_volume").(float64)
	if soundset == "off" {
		soundEnabled = false
	}
	player := audio.New(audio.Config{Enabled: soundEnabled, Volume: volume, Soundset: soundset})

	m := Model{
		Socket:    socket,
		Focus:     FocusBoot,
		Filter:    "all",
		Audio:     player,
		BootStart: time.Now(),
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return err
	}
	if fm, ok := final.(Model); ok && fm.BootFailed {
		return fm.BootError
	}
	return nil
}
```

Remove the existing `client.Dial`, `c.Hello`, `c.Subscribe`, settings/theme/audio/state setup that used to happen here — all of that is now done by step Cmds.

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: PASS.

- [ ] **Step 3: Run rex and validate end-to-end**

```bash
go run ./cmd/rex
```

Expected behavior:
- splash renders with real steps
- if daemon isn't running, `[ OK ] daemon · started · pid X · <ms>` appears
- chimes per row
- hand-off to the board after the min-duration window
- board functions normally

If `Settings != nil` panics occur in step funcs that pre-date the store being populated (e.g., `stepThemeApply` runs before `stepSettingsLoad`'s result lands in `m.Store`), this is real — the sequence currently has settings.load BEFORE theme.apply, so `m.Store` should be set. But `stepSettingsLoad`'s success doesn't write the store onto the model. That's a Task 4.7 fix.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/tui.go
git commit -m "tui: pre-bootstrap audio + start program in FocusBoot, drop pre-program boot"
```

---

### Task 4.7: Settings-load step writes Store onto Model

**Files:**
- Modify: `internal/tui/splash_steps.go`
- Modify: `internal/tui/update.go`

- [ ] **Step 1: Switch `stepSettingsLoad` to a special msg that carries the store**

Replace `stepSettingsLoad` in `internal/tui/splash_steps.go`:

```go
type settingsResultMsg struct {
	Store *settings.Store
	Path  string
	Err   error
	Found bool
	Dur   time.Duration
}

func stepSettingsLoad(_ Model) tea.Cmd {
	return func() tea.Msg {
		t0 := time.Now()
		path := settings.DefaultPath()
		s := settings.NewStore()
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return settingsResultMsg{Store: s, Path: path, Found: false, Dur: time.Since(t0)}
		}
		err := s.Load(path)
		return settingsResultMsg{Store: s, Path: path, Found: true, Err: err, Dur: time.Since(t0)}
	}
}
```

- [ ] **Step 2: Handle `settingsResultMsg` in Update**

In `internal/tui/update.go`, add a case before `bootStepMsg`:

```go
	case settingsResultMsg:
		m.Store = msg.Store
		m.StorePath = msg.Path
		if !msg.Found {
			return m.appendBootStep(bootStepMsg{
				Name: "settings.load", Status: stepSkip, Desc: "no config file (defaults)", Dur: msg.Dur,
			})
		}
		if msg.Err != nil {
			return m.appendBootStep(bootStepMsg{
				Name: "settings.load", Status: stepWarn, Desc: fmt.Sprintf("%v (defaults)", msg.Err), Err: msg.Err, Dur: msg.Dur,
			})
		}
		return m.appendBootStep(bootStepMsg{
			Name: "settings.load", Status: stepOK, Desc: msg.Path, Dur: msg.Dur,
		})
```

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/splash_steps.go internal/tui/update.go
git commit -m "tui: settings.load writes Store onto Model via result msg"
```

---

### Task 4.8: State machine unit tests

**Files:**
- Create: `internal/tui/splash_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/tui/splash_test.go`:

```go
package tui

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// recordingAudio is a recording audioPlayer fake.
type recordingAudio struct{ Events []string }

func (r *recordingAudio) Play(e string) { r.Events = append(r.Events, e) }

func newBootModelWithSeq(steps int) Model {
	bootSequence = make([]stepFunc, steps)
	for i := range bootSequence {
		bootSequence[i] = func(_ Model) tea.Cmd { return nil } // never called by these tests
	}
	return Model{
		Focus:     FocusBoot,
		BootStart: time.Now(),
		Audio:     &recordingAudio{},
	}
}

func TestBootPipelineAdvances(t *testing.T) {
	m := newBootModelWithSeq(3)
	mi, _ := m.appendBootStep(bootStepMsg{Name: "a", Status: stepOK})
	m = mi.(Model)
	require.Equal(t, 1, m.BootStep)
	require.False(t, m.BootDone)

	mi, _ = m.appendBootStep(bootStepMsg{Name: "b", Status: stepOK})
	m = mi.(Model)
	require.Equal(t, 2, m.BootStep)
	require.False(t, m.BootDone)

	mi, _ = m.appendBootStep(bootStepMsg{Name: "c", Status: stepOK})
	m = mi.(Model)
	require.Equal(t, 3, m.BootStep)
	require.True(t, m.BootDone)
}

func TestBootHandOffWaitsForMin(t *testing.T) {
	m := newBootModelWithSeq(1)
	mi, _ := m.appendBootStep(bootStepMsg{Name: "x", Status: stepOK})
	m = mi.(Model)
	require.True(t, m.BootDone)
	require.False(t, m.BootMinDone)
	require.Equal(t, FocusBoot, m.Focus, "no handoff before min")

	// Send min elapsed.
	mi2, cmd := (Model(m)).update(bootMinElapsedMsg{})
	m = mi2.(Model)
	require.True(t, m.BootMinDone)
	require.Equal(t, FocusBoard, m.Focus)
	require.NotNil(t, cmd, "handoff batch should be returned")
}

func TestBootHandOffWaitsForLastStep(t *testing.T) {
	m := newBootModelWithSeq(2)
	mi, _ := (Model(m)).update(bootMinElapsedMsg{})
	m = mi.(Model)
	require.True(t, m.BootMinDone)
	require.Equal(t, FocusBoot, m.Focus, "no handoff while pipeline running")

	mi, _ = m.appendBootStep(bootStepMsg{Name: "a", Status: stepOK})
	m = mi.(Model)
	require.Equal(t, FocusBoot, m.Focus)

	mi, _ = m.appendBootStep(bootStepMsg{Name: "b", Status: stepOK})
	m = mi.(Model)
	require.Equal(t, FocusBoard, m.Focus, "handoff fires on last step")
}

func TestBootFailureBlocksHandOff(t *testing.T) {
	m := newBootModelWithSeq(3)
	failErr := errors.New("daemon not found")
	mi, _ := m.appendBootStep(bootStepMsg{Name: "daemon", Status: stepFail, Err: failErr})
	m = mi.(Model)
	require.True(t, m.BootFailed)
	require.Equal(t, failErr, m.BootError)

	mi2, _ := (Model(m)).update(bootMinElapsedMsg{})
	m = mi2.(Model)
	require.True(t, m.BootMinDone)
	require.Equal(t, FocusBoot, m.Focus, "still on splash after fail+min")
}

func TestBootHandOffClearsLog(t *testing.T) {
	m := newBootModelWithSeq(1)
	mi, _ := m.appendBootStep(bootStepMsg{Name: "only", Status: stepOK})
	m = mi.(Model)
	mi, _ = (Model(m)).update(bootMinElapsedMsg{})
	m = mi.(Model)
	require.Equal(t, FocusBoard, m.Focus)
	require.Nil(t, m.BootLog)
}
```

The tests call `(Model).update(bootMinElapsedMsg{})` as if `Update`'s logic is on a method. Add a tiny shim in `internal/tui/update.go` (or wherever convenient) that lets tests drive the Update path for these two msgs without going through the full Bubble Tea machinery. Add this helper to `internal/tui/update.go`:

```go
// update is the test-friendly entry point that runs the same switch as Update.
// It only covers the boot msgs used by splash_test.go.
func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case bootMinElapsedMsg:
		m.BootMinDone = true
		if m.BootDone && !m.BootFailed {
			return m.handOffToBoard()
		}
		return m, nil
	}
	_ = msg
	return m, nil
}
```

(Keep the `bootMinElapsedMsg` case in the real `Update` as well — this shim is just for testing.)

- [ ] **Step 2: Run tests, expect pass**

Run: `go test ./internal/tui/... -run TestBoot -v`
Expected: PASS for all five tests.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/splash_test.go internal/tui/update.go
git commit -m "tui: state-machine tests for boot pipeline + handoff + failure"
```

---

### Task 4.9: Audio routing test

**Files:**
- Create: `internal/tui/splash_audio_test.go`

- [ ] **Step 1: Write the test**

Create `internal/tui/splash_audio_test.go`:

```go
package tui

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/audio"
)

func TestSplashChimePerStatus(t *testing.T) {
	bootSequence = make([]stepFunc, 4)

	rec := &recordingAudio{}
	m := Model{Focus: FocusBoot, BootStart: time.Now(), Audio: rec}

	mi, _ := m.appendBootStep(bootStepMsg{Name: "a", Status: stepOK})
	m = mi.(Model)
	mi, _ = m.appendBootStep(bootStepMsg{Name: "b", Status: stepWarn})
	m = mi.(Model)
	mi, _ = m.appendBootStep(bootStepMsg{Name: "c", Status: stepSkip})
	m = mi.(Model)
	mi, _ = m.appendBootStep(bootStepMsg{Name: "d", Status: stepFail, Err: errors.New("x")})
	m = mi.(Model)

	require.Equal(t, []string{
		audio.EventBootOK,
		audio.EventBootWarn,
		audio.EventBootFail,
	}, rec.Events, "OK + WARN + FAIL chime; SKIP is silent")
}

func TestSplashHandOffPlaysStartup(t *testing.T) {
	bootSequence = make([]stepFunc, 1)
	rec := &recordingAudio{}
	m := Model{Focus: FocusBoot, BootStart: time.Now(), Audio: rec}

	mi, _ := m.appendBootStep(bootStepMsg{Name: "x", Status: stepOK})
	m = mi.(Model)
	mi, _ = (Model(m)).update(bootMinElapsedMsg{})
	m = mi.(Model)

	require.Equal(t, FocusBoard, m.Focus)
	require.Contains(t, rec.Events, audio.EventStartup)
}
```

- [ ] **Step 2: Run, expect pass**

Run: `go test ./internal/tui/... -run TestSplash.*Audio -v`
Expected: PASS for both.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/splash_audio_test.go
git commit -m "tui: splash audio routing tests"
```

---

### Task 4.10: Build-validate Stage 4 end-to-end

- [ ] **Step 1: Full build + tests**

Run: `go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 2: Manual smoke — happy path (daemon already running)**

```bash
rex daemon start    # ensure daemon is up
go run ./cmd/rex
```

Expected: splash shows `[ OK ] daemon · already running (pid X)`, all 17 rows render with chimes, hand-off to the board.

- [ ] **Step 3: Manual smoke — cold start**

```bash
pkill rex-daemon 2>/dev/null
go run ./cmd/rex
```

Expected: splash shows `[ OK ] daemon · started · pid X · <ms>` visibly during the 300+ ms spawn, then proceeds.

- [ ] **Step 4: Stage 4 done — no commit (everything already committed)**

---

## Stage 5: Failure UI + final tests

### Task 5.1: Failure render — header swap, cause + hint lines, quit prompt

**Files:**
- Modify: `internal/tui/splash.go`

- [ ] **Step 1: Add hint map**

Append to `internal/tui/splash.go`:

```go
// bootFixHints maps a failed step name to a human-readable next step.
var bootFixHints = map[string]string{
	"paths.ensure":   "check that you can write to ~/.config and ~/.local/state",
	"registry.load":  "rex's built-in YAML is corrupt — reinstall or rebuild rex",
	"socket.resolve": "set XDG_RUNTIME_DIR or ensure HOME is set",
	"daemon":         "ensure rex-daemon is on PATH or installed alongside rex",
	"client.dial":    "the daemon socket disappeared — try `rex daemon start`",
	"handshake":      "protocol mismatch — rebuild rex and rex-daemon to the same version",
	"subscribe":      "the daemon rejected the event subscription — check daemon.log",
}
```

- [ ] **Step 2: Extend `renderSplash` to render the failure trailer**

In `internal/tui/splash.go`, modify `renderSplash` to append cause + hint lines + quit prompt when `m.BootFailed`. Replace the post-loop block:

```go
	if m.BootDone && !m.BootFailed {
		ready := fmt.Sprintf("準備完了 ready · %s", elapsed)
		rows = append(rows, "", lipgloss.PlaceHorizontal(w-2, lipgloss.Right, styleBootReady.Render(ready)))
	}
	if m.BootFailed {
		rows = append(rows, "")
		if m.BootError != nil {
			rows = append(rows, leftPad+styleDim.Render("cause: ")+styleBootCause.Render(m.BootError.Error()))
		}
		if hint, ok := bootFixHints[lastFailedStep(m)]; ok {
			rows = append(rows, leftPad+styleDim.Render("fix:   ")+stylePrimary.Render(hint))
		}
		rows = append(rows, "", lipgloss.PlaceHorizontal(w-2, lipgloss.Right, styleDim.Render("press q · ctrl+c to quit")))
	}
```

Add the helper:

```go
// lastFailedStep returns the name of the most recent stepFail row.
func lastFailedStep(m Model) string {
	for i := len(m.BootLog) - 1; i >= 0; i-- {
		if m.BootLog[i].Status == stepFail {
			return m.BootLog[i].Name
		}
	}
	return ""
}
```

- [ ] **Step 3: Build**

Run: `go build ./internal/tui/...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/splash.go
git commit -m "tui: splash failure trailer with cause + hint + quit prompt"
```

---

### Task 5.2: Failure snapshot test

**Files:**
- Modify: `internal/tui/splash_snapshot_test.go`

- [ ] **Step 1: Add failing test**

Append:

```go
import (
	// existing imports +:
	"errors"
)

func TestSplashFailed(t *testing.T) {
	m := Model{
		Focus:      FocusBoot,
		Width:      100,
		Height:     30,
		BootStart:  time.Now().Add(-2 * time.Second),
		BootFailed: true,
		BootError:  errors.New(`exec: "rex-daemon": executable file not found in $PATH`),
		BootLog: []bootLine{
			{Name: "log.init", Status: stepOK, Desc: "ok"},
			{Name: "daemon", Status: stepFail, Desc: "binary not found", Err: errors.New("missing")},
		},
	}
	out := renderSplash(m, m.Width, m.Height)
	require.Contains(t, out, "起動失敗")
	require.Contains(t, out, "FAIL")
	require.Contains(t, out, "cause:")
	require.Contains(t, out, "rex-daemon")
	require.Contains(t, out, "fix:")
	require.Contains(t, out, "press q")
}
```

- [ ] **Step 2: Run, expect pass**

Run: `go test ./internal/tui/... -run TestSplashFailed -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/splash_snapshot_test.go
git commit -m "tui: failure render snapshot test"
```

---

### Task 5.3: Failure end-to-end — stderr propagation

**Files:**
- Modify: `internal/tui/tui.go` (verify only — should already be correct from Task 4.6)

- [ ] **Step 1: Confirm `tui.Run` returns `BootError` on failure**

Open `internal/tui/tui.go`. Confirm the tail of `Run` reads:

```go
	final, err := p.Run()
	if err != nil {
		return err
	}
	if fm, ok := final.(Model); ok && fm.BootFailed {
		return fm.BootError
	}
	return nil
```

- [ ] **Step 2: Manual smoke — force a failure**

Temporarily break the daemon binary lookup:

```bash
pkill rex-daemon 2>/dev/null
PATH=/usr/bin:/bin go run ./cmd/rex
```

Expected: splash renders, fails at `[ FAIL ] daemon`, shows cause + hint + quit prompt. Press `q`. Terminal returns to shell, and rex prints something like:

```
rex: spawn rex-daemon: exec: "rex-daemon": executable file not found in $PATH
```

Exit status is non-zero (`echo $?` → ≥1).

- [ ] **Step 3: No commit needed if Step 1 was already correct**

---

### Task 5.4: Final full-suite test + manual checklist

- [ ] **Step 1: Run the whole test suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 2: Manual checklist (from spec § Manual verification)**

- [ ] Cadence at 70 ms inter-step delay feels right (not stuttery, not blurred).
- [ ] Real audio plays through CoreAudio with the evangelion soundset; chimes don't clip each other.
- [ ] Terminal returns clean (no leftover alt-screen residue) when failing and quitting (`reset` not required).
- [ ] A genuinely cold start surfaces `[ OK ] daemon · started · pid X · <ms>` visibly.
- [ ] Color scheme `paper` (light) renders the splash readably — green still reads as success, red as failure. Try with `~/.config/rex/config.yaml`:

  ```yaml
  color_scheme: paper
  ```

- [ ] **Step 3: Stage 5 complete — no commit needed.**

---

## Spec coverage cross-check

| spec section / requirement                                                    | task that implements it                |
|-------------------------------------------------------------------------------|----------------------------------------|
| Goal: self-contained splash component                                         | Task 3.4 (`renderSplash` in splash.go) |
| Goal: real per-step status, no fakes                                          | Tasks 4.2 + 4.3 (real Cmds)            |
| Goal: per-row audio + soundset respect                                        | Tasks 2.1–2.3 + 3.8 (chime in appendBootStep) |
| Goal: min visible duration                                                    | Task 3.2 (`bootMinDuration`) + 3.8/3.10 |
| Goal: graceful failure render                                                 | Task 5.1 + 5.2 + 5.3                   |
| Non-goal: skippable / retry — explicitly NOT implemented                      | n/a                                    |
| Arch: move boot into Bubble Tea cmds                                          | Task 4.6                               |
| Arch: daemonctl extract                                                       | Tasks 1.1–1.3                          |
| Arch: FocusBoot + Boot fields                                                 | Task 3.1                               |
| Arch: boot pipeline shape (sequence + msg + 70 ms gap)                        | Task 3.2 + 3.8                         |
| Visual: alt-screen, no border, header + spinner + log + ready                 | Task 3.4                               |
| Visual: status brackets w/ palette colors                                     | Task 3.3                               |
| Visual: row format + east-asian width                                         | Task 3.4 (`padCategory` + `ansi.StringWidth`) |
| Visual: Japanese touches                                                      | Tasks 3.4 + 3.5 + 3.6 + 5.1 (起動失敗) + step descs in 4.3 |
| Visual: spinner uses existing tickSpinner                                     | Task 3.10 (Init batch)                 |
| Boot pipeline: 17 steps with the described success/skip/warn/fail behaviors   | Tasks 4.2 + 4.3                        |
| Side effects deferred to hand-off (EventStartup, listenDaemon)                | Task 3.8 (`handOffToBoard`)            |
| Audio: 3 new events                                                           | Task 2.1                               |
| Audio: catalog entries                                                        | Tasks 2.2 + 2.3                        |
| Audio: bake-coverage enforcement                                              | Task 2.3 Step 3                        |
| Audio: wiring per status                                                      | Task 3.8 (`appendBootStep`)            |
| Audio: pre-bootstrap player                                                   | Task 4.6 (`Run` start)                 |
| Transition guard `BootDone && BootMinDone && !BootFailed`                     | Task 3.8 (`appendBootStep`) + 4.4      |
| Hand-off cmds (HideCursor, listenDaemon, tickSpinner)                         | Task 3.8 (`handOffToBoard`)            |
| Race outcomes: fast/slow/fail                                                 | Tested in 4.8                          |
| Resize during splash                                                          | inherited — existing `WindowSizeMsg` handler in update.go is untouched |
| Keyboard during splash (q/ctrl+c/esc → quit; others ignored)                  | inherited — existing key handlers in update.go for quit; no new keys for the splash so other keys naturally fall through |
| Failure: two tiers, terminal vs warn                                          | Task 3.8 (FAIL halts; WARN continues)  |
| Failure: render w/ cause + fix hint                                           | Task 5.1                               |
| Failure: stderr propagation                                                   | Task 4.6 (final.BootError) + 5.3       |
| Tests: render snapshots                                                       | Tasks 3.4, 3.5, 3.6, 5.2 (5 of the 7 listed; the remaining two — `TestSplashWithWarn`, `TestSplashJapaneseColumnAlign`, `TestSplashNarrowTerminal` — are nice-to-haves; see below) |
| Tests: state machine                                                          | Task 4.8                               |
| Tests: audio routing                                                          | Task 4.9                               |
| Tests: audio catalogs                                                         | Task 2.3 Step 3                        |
| Tests: step Cmd tests                                                         | partial — `stepSnapshotParse` and the daemonctl branch tests cover the spec's "Step Cmd tests" cluster; the explicit `TestSnapshotParseCountsByState` is left as a follow-up nice-to-have |
| Manual smoke checklist                                                        | Task 5.4                               |

**Known follow-ups not blocking ship:** `TestSplashWithWarn`, `TestSplashJapaneseColumnAlign`, `TestSplashNarrowTerminal`, `TestSnapshotParseCountsByState`. None are spec-blocking; they polish the test suite. Add them if Stage 5 has bandwidth, otherwise log as small TODOs.

---

## Plan complete

Plan saved to `docs/superpowers/plans/2026-05-15-boot-splash.md`. Execute task-by-task with `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans`.
