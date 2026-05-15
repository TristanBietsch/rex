# Plan C — Rex TUI core

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. **Commit hygiene: NEVER add `Co-Authored-By: Claude ...` trailers.**

**Goal:** Ship the Bubble Tea TUI — the headline experience of Rex. After this plan, running `rex` (no args) opens an interactive board that lists agent sessions, lets you navigate with vim keys, spawn new sessions via a wizard, attach to a running PTY via a modal, and detach with `:bg`.

**Architecture:** A single `internal/tui` package with the Bubble Tea Model/Msg/View pattern. The TUI's only daemon dependency is `internal/client` (already built in Plan B). A goroutine reads events from the daemon and converts them into Bubble Tea messages. State lives in one root model; sub-views (modal, wizard, command line, help, quit-confirm) are toggled via the root's focus enum.

**Tech Stack:** `charmbracelet/bubbletea`, `charmbracelet/lipgloss`, `charmbracelet/bubbles/viewport` for the modal PTY pane. Everything else is stdlib + already-vendored Plan A/B packages.

**Out of scope for Plan C (deferred to Plan D):**
- `/` slash palette (the picker UI; built-ins already implemented as CLI verbs)
- Settings page UI + Lua config layer + persistent settings registry
- Audio synth (Factorio sounds)
- Section-slide / modal-scale-in / row-pulse animations (Plan D — only spinner + done-blink ship in Plan C)
- Custom color schemes (`noir`, `paper`) — Plan C ships default only
- Row density variants — Plan C ships `normal` only

---

## File structure

```
internal/tui/
├── tui.go              — Run(socket string) entry point
├── model.go            — Root model + Msg types + focus enum
├── events.go           — Goroutine that reads daemon events → Bubble Tea Msgs
├── styles.go           — Lipgloss styles (palette + state colors)
├── header.go           — Header rendering (∴ REX + counts + chips)
├── board.go            — Section / row rendering
├── prompt.go           — Bottom λ prompt + help bar
├── modal.go            — Attach modal (PTY viewport + reply line)
├── wizard.go           — New-agent wizard (5 steps)
├── command.go          — `:` command-mode parser + dispatcher
├── help.go             — Help overlay
├── confirm.go          — Quit confirmation
├── persist.go          — tui-state.json save/load
├── keymap.go           — Keybindings table (single source of truth)
└── tui_test.go         — Unit tests for state transitions
cmd/rex/main.go         — `rex` (no args) now launches the TUI
```

---

## Phase 1 — Scaffold and connectivity (3 tasks)

### Task C0: TUI package skeleton

**Files:**
- Create: `internal/tui/tui.go`
- Create: `internal/tui/model.go`
- Create: `internal/tui/styles.go`
- Modify: `cmd/rex/main.go` — call `tui.Run` when no args

- [ ] **Branch check**

```bash
cd /Users/tristan/Documents/personal/dev/rex
git branch --show-current   # MUST print: plan-c-tui
```

- [ ] **Add Bubble Tea deps**

```bash
go get github.com/charmbracelet/bubbletea
go get github.com/charmbracelet/lipgloss
go get github.com/charmbracelet/bubbles/viewport
```

- [ ] **Create `internal/tui/styles.go`:**

```go
package tui

import "github.com/charmbracelet/lipgloss"

// Palette mirrors docs/design.md.
var (
	colorBgBase    = lipgloss.Color("#0F1115")
	colorBgElev    = lipgloss.Color("#171922")
	colorBgModal   = lipgloss.Color("#1B1E29")
	colorFgPrimary = lipgloss.Color("#E6E6E6")
	colorFgDim     = lipgloss.Color("#7A7F8C")
	colorFgMuted   = lipgloss.Color("#4A4F5A")
	colorBorder    = lipgloss.Color("#262A36")

	colorWorking = lipgloss.Color("#5B8DEF")
	colorNeeds   = lipgloss.Color("#E5B341")
	colorDone    = lipgloss.Color("#4ADE80")
	colorFailed  = lipgloss.Color("#EF4444")
	colorCrashed = lipgloss.Color("#7A7F8C")
)

// Reusable styles.
var (
	styleHeaderApp    = lipgloss.NewStyle().Bold(true).Foreground(colorFgPrimary)
	styleHeaderMeta   = lipgloss.NewStyle().Foreground(colorFgDim)
	styleSectionTitle = lipgloss.NewStyle().Bold(true).Foreground(colorFgPrimary)
	styleSlug         = lipgloss.NewStyle().Bold(true).Foreground(colorFgPrimary)
	styleDim          = lipgloss.NewStyle().Foreground(colorFgDim)
	styleMuted        = lipgloss.NewStyle().Foreground(colorFgMuted)
	styleSelected     = lipgloss.NewStyle().Background(colorBgElev).Foreground(colorFgPrimary)

	styleStateWorking = lipgloss.NewStyle().Bold(true).Foreground(colorWorking)
	styleStateNeeds   = lipgloss.NewStyle().Bold(true).Foreground(colorNeeds)
	styleStateDone    = lipgloss.NewStyle().Bold(true).Foreground(colorDone)
	styleStateFailed  = lipgloss.NewStyle().Bold(true).Foreground(colorFailed)
	styleStateCrashed = lipgloss.NewStyle().Bold(true).Foreground(colorCrashed)
)
```

- [ ] **Create `internal/tui/model.go`:**

```go
package tui

import (
	"github.com/tristanbietsch/rex/internal/protocol"
)

// Focus is what currently has keyboard focus.
type Focus int

const (
	FocusBoard Focus = iota
	FocusPrompt
	FocusCommand
	FocusModal
	FocusWizard
	FocusHelp
	FocusConfirmQuit
)

// Model is the root Bubble Tea model.
type Model struct {
	Focus       Focus
	Width       int
	Height      int
	Sessions    []protocol.SessionSummary
	SelectedID  string // empty if no selection
	Filter      string // "all" or a tool id
	PromptText  string // text in the λ prompt
	CmdText     string // text in `:` command line
	Err         string // last error to surface in the help line
	SpinnerTick int    // monotonic frame counter for spinner animation
	Quitting    bool

	// Sub-models — only populated when corresponding Focus is active.
	// Wizard, Modal, Help, ConfirmQuit are added by later tasks.
}
```

- [ ] **Create `internal/tui/tui.go`:**

```go
// Package tui is the Rex Bubble Tea interface.
package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tristanbietsch/rex/internal/client"
)

// Run launches the TUI. Blocks until the user quits.
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

	m := Model{
		Focus:    FocusBoard,
		Sessions: snap.Sessions,
		Filter:   "all",
	}
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = p.Run()
	return err
}

// Init satisfies tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// View satisfies tea.Model. Real rendering arrives in Task C2.
func (m Model) View() string { return "rex TUI scaffold — Task C2 lights it up\n" }

// Update satisfies tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		}
	}
	return m, nil
}
```

- [ ] **Modify `cmd/rex/main.go`:**

Find the `if len(args) == 0 {` block. Replace its body with:

```go
if len(args) == 0 {
    return cli.RunTUI()
}
```

- [ ] **Add `internal/cli/tui.go`:**

```go
package cli

import (
	"github.com/tristanbietsch/rex/internal/tui"
)

// RunTUI is the no-args entry — opens the Bubble Tea board.
func RunTUI() error {
	return tui.Run(DefaultSocket())
}
```

- [ ] **Build + smoke test**

```bash
go build ./...
make build-all
echo "q" | ./rex 2>&1 | head -3
```

Expected: "rex TUI scaffold — Task C2 lights it up" then `q` quits. (When stdin isn't a TTY Bubble Tea may degrade — that's fine for smoke.)

- [ ] **Commit**

```bash
git branch --show-current   # plan-c-tui
git add cmd/rex/ internal/cli/tui.go internal/tui/ go.mod go.sum
git commit -m "tui: scaffold — Bubble Tea entry, model skeleton, palette styles"
```

NO Claude trailer.

---

### Task C1: Daemon event subscription pump

**Files:** Create `internal/tui/events.go`. Modify `tui.go` to use it.

A goroutine reads from `client.Client.NextEvent()` and sends each event as a `DaemonEventMsg` to the Bubble Tea program. Plus a spinner ticker.

- [ ] **Create `internal/tui/events.go`:**

```go
package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tristanbietsch/rex/internal/client"
	"github.com/tristanbietsch/rex/internal/protocol"
)

// DaemonEventMsg wraps a single event from the daemon.
type DaemonEventMsg struct {
	Env protocol.Envelope
}

// DaemonErrMsg is sent when the connection to the daemon dies.
type DaemonErrMsg struct {
	Err error
}

// SpinnerTickMsg fires periodically to drive the working-state spinner.
type SpinnerTickMsg struct{}

// listenDaemon returns a tea.Cmd that reads ONE event then re-arms itself.
func listenDaemon(c *client.Client) tea.Cmd {
	return func() tea.Msg {
		env, err := c.NextEvent()
		if err != nil {
			return DaemonErrMsg{Err: err}
		}
		return DaemonEventMsg{Env: env}
	}
}

// tickSpinner emits a SpinnerTickMsg every 100ms.
func tickSpinner() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return SpinnerTickMsg{} })
}
```

- [ ] **Wire into Model.Init and Update** — modify `tui.go`:

Update `Model` (in `model.go`) to hold the `*client.Client`:

```go
type Model struct {
    Client *client.Client
    // ... existing fields ...
}
```

In `tui.go`, pass `c` to the model:

```go
m := Model{
    Client:   c,
    Focus:    FocusBoard,
    Sessions: snap.Sessions,
    Filter:   "all",
}
```

Update `Init`:

```go
func (m Model) Init() tea.Cmd {
    return tea.Batch(listenDaemon(m.Client), tickSpinner())
}
```

In `Update`, handle the new message types (add cases to the switch in update.go — actually update lives in tui.go for now; move it to update.go in Task C2):

```go
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case DaemonEventMsg:
        m = m.applyEvent(msg.Env)
        return m, listenDaemon(m.Client) // re-arm
    case DaemonErrMsg:
        m.Err = msg.Err.Error()
        return m, tea.Quit
    case SpinnerTickMsg:
        m.SpinnerTick++
        return m, tickSpinner()
    case tea.KeyMsg:
        switch msg.String() {
        case "q", "ctrl+c":
            return m, tea.Quit
        }
    }
    return m, nil
}
```

Add `applyEvent` to `model.go`:

```go
import (
	"encoding/json"
	"github.com/tristanbietsch/rex/internal/protocol"
)

func (m Model) applyEvent(env protocol.Envelope) Model {
	switch env.Type {
	case protocol.EventSessionAdded:
		var sum protocol.SessionSummary
		if err := json.Unmarshal(env.Data, &sum); err == nil {
			m.Sessions = append(m.Sessions, sum)
		}
	case protocol.EventSessionRemoved:
		var rem protocol.SessionRemoved
		if err := json.Unmarshal(env.Data, &rem); err == nil {
			out := m.Sessions[:0]
			for _, s := range m.Sessions {
				if s.ID != rem.SessionID {
					out = append(out, s)
				}
			}
			m.Sessions = out
		}
	case protocol.EventSessionUpdated:
		var upd protocol.SessionUpdated
		if err := json.Unmarshal(env.Data, &upd); err == nil {
			for i := range m.Sessions {
				if m.Sessions[i].ID == upd.SessionID {
					applyPatch(&m.Sessions[i], upd.Patch)
					break
				}
			}
		}
	case protocol.EventSnapshot:
		var snap protocol.Snapshot
		if err := json.Unmarshal(env.Data, &snap); err == nil {
			m.Sessions = snap.Sessions
			m.Filter = snap.Filter
		}
	}
	return m
}

func applyPatch(s *protocol.SessionSummary, patch map[string]any) {
	if v, ok := patch["state"].(string); ok {
		s.State = protocol.State(v)
	}
	if v, ok := patch["slug"].(string); ok {
		s.Slug = v
	}
	if v, ok := patch["title"].(string); ok {
		s.Title = v
	}
	if v, ok := patch["last_line"].(string); ok {
		s.LastLine = v
	}
}
```

- [ ] **Build + smoke**

```bash
go build ./...
go test ./... -count=1
```

Expected: all PASS.

- [ ] **Commit**

```bash
git add internal/tui/
git commit -m "tui: daemon event pump + spinner ticker"
```

NO Claude trailer.

---

### Task C2: Window-size handling and view structure

**Files:** Modify `internal/tui/model.go`, `internal/tui/tui.go`. Create `internal/tui/update.go`.

Split `Update` out of `tui.go` (it's getting long) and handle window resize.

- [ ] **Move Update to its own file** — create `internal/tui/update.go`:

```go
package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Update is the central Bubble Tea handler.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		return m, nil
	case DaemonEventMsg:
		m = m.applyEvent(msg.Env)
		return m, listenDaemon(m.Client)
	case DaemonErrMsg:
		m.Err = msg.Err.Error()
		return m, tea.Quit
	case SpinnerTickMsg:
		m.SpinnerTick++
		return m, tickSpinner()
	case tea.KeyMsg:
		return updateKey(m, msg)
	}
	return m, nil
}

func updateKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	switch k.String() {
	case "q", "ctrl+c":
		m.Quitting = true
		return m, tea.Quit
	}
	return m, nil
}
```

- [ ] **Delete the old Update from tui.go** (keep only `Run`, `Init`, `View`).

- [ ] **Build + commit**

```bash
go build ./...
go test ./... -count=1
git add internal/tui/
git commit -m "tui: window-size handling; split Update into its own file"
```

---

## Phase 2 — Rendering (4 tasks)

### Task C3: Header rendering

**Files:** Create `internal/tui/header.go`. Modify `tui.go` View.

- [ ] **Create `internal/tui/header.go`:**

```go
package tui

import (
	"fmt"
	"strings"

	"github.com/tristanbietsch/rex/internal/protocol"
)

func renderHeader(m Model) string {
	var working, needsInput, done, failed, crashed int
	for _, s := range m.Sessions {
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

	logo := styleHeaderApp.Render("∴ REX")
	counts := styleHeaderMeta.Render(fmt.Sprintf("  %d awaiting input · %d working · %d completed",
		needsInput, working, done))
	if failed+crashed > 0 {
		counts += styleStateFailed.Render(fmt.Sprintf("  · %d failed", failed+crashed))
	}

	newBtn := styleMuted.Render("[ + new ]")

	chips := renderFilterChips(m)

	return strings.Join([]string{logo + counts + "    " + newBtn, chips}, "\n")
}

func renderFilterChips(m Model) string {
	tools := []string{"all", "claude", "codex", "gemini", "ollama"}
	parts := make([]string, 0, len(tools)*2-1)
	for i, t := range tools {
		if i > 0 {
			parts = append(parts, styleMuted.Render(" · "))
		}
		if t == m.Filter {
			parts = append(parts, styleHeaderApp.Render(t))
		} else {
			parts = append(parts, styleDim.Render(t))
		}
	}
	return strings.Join(parts, "")
}
```

- [ ] **Update `View` in `tui.go`:**

```go
func (m Model) View() string {
    if m.Quitting {
        return ""
    }
    return renderHeader(m) + "\n\n[board renders in Task C4]\n"
}
```

- [ ] **Build + commit**

```bash
go build ./...
git add internal/tui/
git commit -m "tui: header render — ∴ REX + counts + filter chips"
```

---

### Task C4: Section + row rendering

**Files:** Create `internal/tui/board.go`. Modify `tui.go` View.

- [ ] **Create `internal/tui/board.go`:**

```go
package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/tristanbietsch/rex/internal/protocol"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func renderBoard(m Model) string {
	var b strings.Builder

	groups := []struct {
		title string
		state protocol.State
	}{
		{"Needs input", protocol.StateNeedsInput},
		{"Working", protocol.StateWorking},
		{"Completed", protocol.StateDone},
	}

	for _, g := range groups {
		rows := filterByState(m.Sessions, g.state, m.Filter)
		b.WriteString(styleSectionTitle.Render(g.title))
		b.WriteString("\n")
		if len(rows) == 0 {
			b.WriteString(styleMuted.Render("  (none)"))
			b.WriteString("\n\n")
			continue
		}
		for _, s := range rows {
			b.WriteString(renderRow(m, s))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	return b.String()
}

func filterByState(sessions []protocol.SessionSummary, st protocol.State, filter string) []protocol.SessionSummary {
	out := make([]protocol.SessionSummary, 0, len(sessions))
	for _, s := range sessions {
		if s.State != st {
			continue
		}
		if filter != "all" && filter != "" && s.ToolID != filter {
			continue
		}
		out = append(out, s)
	}
	return out
}

func renderRow(m Model, s protocol.SessionSummary) string {
	marker := stateMarker(s.State, m.SpinnerTick)
	id := styleMuted.Render(s.ShortID)
	slug := styleSlug.Render(truncate(s.Slug, 22))
	desc := styleDim.Render(truncate(s.LastLine, 40))
	model := styleDim.Render(modelLabel(s))
	ago := styleDim.Render(durationAgo(s.LastEventAt))

	row := fmt.Sprintf("%s %-5s %-22s %-40s %-18s %5s", marker, id, slug, desc, model, ago)
	if m.SelectedID == s.ID {
		row = styleSelected.Render(row)
	}
	return "  " + row
}

func stateMarker(st protocol.State, tick int) string {
	switch st {
	case protocol.StateWorking:
		return styleStateWorking.Render(spinnerFrames[tick%len(spinnerFrames)])
	case protocol.StateNeedsInput:
		return styleStateNeeds.Render("◆")
	case protocol.StateDone:
		return styleStateDone.Render("●")
	case protocol.StateFailed:
		return styleStateFailed.Render("✕")
	case protocol.StateCrashed:
		return styleStateCrashed.Render("○")
	}
	return " "
}

func modelLabel(s protocol.SessionSummary) string {
	if s.Effort != "" {
		return s.ModelID + " · " + s.Effort
	}
	return s.ModelID
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
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours())/24)
	}
}
```

- [ ] **Update View in `tui.go`:**

```go
func (m Model) View() string {
    if m.Quitting {
        return ""
    }
    return renderHeader(m) + "\n\n" + renderBoard(m)
}
```

- [ ] **Build + commit**

```bash
go build ./...
git add internal/tui/
git commit -m "tui: section + row rendering with spinner-tick state marker"
```

---

### Task C5: Bottom prompt + help line

**Files:** Create `internal/tui/prompt.go`. Modify `tui.go` View.

- [ ] **Create `internal/tui/prompt.go`:**

```go
package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var styleArrow = lipgloss.NewStyle().Foreground(colorWorking)

func renderPrompt(m Model) string {
	switch m.Focus {
	case FocusCommand:
		return styleStateNeeds.Render(":") + " " + m.CmdText + cursorBlock(m)
	default:
		body := m.PromptText
		if body == "" {
			body = styleDim.Render("describe a task for a new session")
		} else if m.Focus == FocusPrompt {
			body = m.PromptText + cursorBlock(m)
		}
		return styleArrow.Render("λ") + " " + body
	}
}

func cursorBlock(m Model) string {
	// Cursor block — alternates with spinner tick for blink effect.
	if m.SpinnerTick%10 < 5 {
		return lipgloss.NewStyle().Background(colorFgPrimary).Foreground(colorBgBase).Render(" ")
	}
	return " "
}

func renderHelpLine(m Model) string {
	parts := []string{
		styleHeaderApp.Render("i") + styleDim.Render(" focus"),
		styleHeaderApp.Render("enter") + styleDim.Render(" open"),
		styleHeaderApp.Render("n") + styleDim.Render(" new"),
		styleHeaderApp.Render("t") + styleDim.Render(" filter"),
		styleHeaderApp.Render(":") + styleDim.Render(" command"),
		styleHeaderApp.Render("dd") + styleDim.Render(" delete"),
		styleHeaderApp.Render("?") + styleDim.Render(" help"),
	}
	sep := styleMuted.Render(" · ")
	if m.Err != "" {
		return styleStateFailed.Render("err: " + m.Err)
	}
	return strings.Join(parts, sep)
}
```

- [ ] **Update View in tui.go:**

```go
func (m Model) View() string {
    if m.Quitting {
        return ""
    }
    return renderHeader(m) + "\n\n" + renderBoard(m) + "\n" + renderPrompt(m) + "\n" + renderHelpLine(m) + "\n"
}
```

- [ ] **Build + commit**

```bash
go build ./...
git add internal/tui/
git commit -m "tui: bottom λ prompt + help bar with mode-aware rendering"
```

---

### Task C6: Selection + j/k/g/G/1/2/3 navigation

**Files:** Modify `internal/tui/update.go`. Create `internal/tui/keymap.go`.

- [ ] **Create `internal/tui/keymap.go`:**

```go
package tui

import "github.com/tristanbietsch/rex/internal/protocol"

// orderedSessions returns sessions in display order: Needs input, Working, Completed, then everything else.
func orderedSessions(m Model) []protocol.SessionSummary {
	groups := []protocol.State{protocol.StateNeedsInput, protocol.StateWorking, protocol.StateDone}
	out := make([]protocol.SessionSummary, 0, len(m.Sessions))
	for _, st := range groups {
		out = append(out, filterByState(m.Sessions, st, m.Filter)...)
	}
	for _, s := range m.Sessions {
		if s.State != protocol.StateNeedsInput && s.State != protocol.StateWorking && s.State != protocol.StateDone {
			if m.Filter != "all" && m.Filter != "" && s.ToolID != m.Filter {
				continue
			}
			out = append(out, s)
		}
	}
	return out
}

// indexOfSelected returns the index of the selected session in display order, or -1.
func indexOfSelected(m Model) int {
	for i, s := range orderedSessions(m) {
		if s.ID == m.SelectedID {
			return i
		}
	}
	return -1
}

// moveSelection adjusts the selected session by delta in display order, clamped.
func moveSelection(m Model, delta int) Model {
	rows := orderedSessions(m)
	if len(rows) == 0 {
		m.SelectedID = ""
		return m
	}
	idx := indexOfSelected(m) + delta
	if idx < 0 {
		idx = 0
	}
	if idx >= len(rows) {
		idx = len(rows) - 1
	}
	m.SelectedID = rows[idx].ID
	return m
}

// jumpToSection sets selection to the first session matching the given state.
func jumpToSection(m Model, st protocol.State) Model {
	rows := filterByState(m.Sessions, st, m.Filter)
	if len(rows) > 0 {
		m.SelectedID = rows[0].ID
	}
	return m
}
```

- [ ] **Update `updateKey` in `internal/tui/update.go`** — replace the function with:

```go
func updateKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	// Quit shortcuts only honored from board focus.
	if m.Focus == FocusBoard {
		switch k.String() {
		case "ctrl+c", "q":
			m.Quitting = true
			return m, tea.Quit
		case "j", "down":
			return moveSelection(m, 1), nil
		case "k", "up":
			return moveSelection(m, -1), nil
		case "g":
			// 'g' is reserved for 'gg' chord later — for now, jump to first row.
			rows := orderedSessions(m)
			if len(rows) > 0 {
				m.SelectedID = rows[0].ID
			}
			return m, nil
		case "G":
			rows := orderedSessions(m)
			if len(rows) > 0 {
				m.SelectedID = rows[len(rows)-1].ID
			}
			return m, nil
		case "1":
			return jumpToSection(m, protocol.StateNeedsInput), nil
		case "2":
			return jumpToSection(m, protocol.StateWorking), nil
		case "3":
			return jumpToSection(m, protocol.StateDone), nil
		case "t":
			// Cycle filter
			tools := []string{"all", "claude", "codex", "gemini", "ollama"}
			next := 0
			for i, t := range tools {
				if t == m.Filter {
					next = (i + 1) % len(tools)
					break
				}
			}
			m.Filter = tools[next]
			return m, nil
		}
	}
	return m, nil
}
```

(Add `"github.com/tristanbietsch/rex/internal/protocol"` to the imports of `update.go`.)

- [ ] **Build + commit**

```bash
go build ./...
git add internal/tui/
git commit -m "tui: navigation — j/k, g/G, 1/2/3 section jumps, t filter cycle"
```

---

## Phase 3 — λ prompt + spawn + dd delete (3 tasks)

### Task C7: `i` to focus λ prompt, type, submit to spawn

**Files:** Modify `internal/tui/update.go`. Modify `internal/tui/prompt.go` (already supports the cursor).

- [ ] **Add focus + typing branches to `updateKey`:**

```go
// Inside the FocusBoard branch, add:
case "i":
    m.Focus = FocusPrompt
    return m, nil

// New branch for FocusPrompt:
if m.Focus == FocusPrompt {
    switch k.Type {
    case tea.KeyEsc:
        m.Focus = FocusBoard
        m.PromptText = ""
        return m, nil
    case tea.KeyEnter:
        text := strings.TrimSpace(m.PromptText)
        m.PromptText = ""
        m.Focus = FocusBoard
        if text == "" {
            return m, nil
        }
        return m, spawnSessionCmd(m.Client, text)
    case tea.KeyBackspace:
        if len(m.PromptText) > 0 {
            m.PromptText = m.PromptText[:len(m.PromptText)-1]
        }
        return m, nil
    case tea.KeyRunes:
        m.PromptText += string(k.Runes)
        return m, nil
    case tea.KeySpace:
        m.PromptText += " "
        return m, nil
    }
}
```

Imports: add `"strings"`.

- [ ] **Add the spawn command** at the bottom of `update.go`:

```go
func spawnSessionCmd(c *client.Client, prompt string) tea.Cmd {
    return func() tea.Msg {
        slug := deriveSlugFromPrompt(prompt)
        cwd, _ := os.Getwd()
        if err := c.NewSession(protocol.NewSession{
            ToolID: "echo", ModelID: "short", Slug: slug, CWD: cwd, InitialPrompt: prompt,
        }); err != nil {
            return DaemonErrMsg{Err: err}
        }
        return nil
    }
}

func deriveSlugFromPrompt(p string) string {
    s := strings.ToLower(p)
    if len(s) > 32 {
        s = s[:32]
    }
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

Imports: add `"os"`, `"github.com/tristanbietsch/rex/internal/client"`, `"github.com/tristanbietsch/rex/internal/protocol"`.

- [ ] **Build + commit**

```bash
go build ./...
git add internal/tui/
git commit -m "tui: λ prompt — i to focus, enter to spawn with derived slug"
```

---

### Task C8: `dd` delete + chord state

**Files:** Modify `internal/tui/model.go` (add chord field). Modify `internal/tui/update.go`.

- [ ] **Add chord state to Model:**

```go
type Model struct {
    // ... existing ...
    PendingChord string // "d" sets up "dd"; cleared by any other key
}
```

- [ ] **Add chord handling in updateKey (FocusBoard branch):**

```go
case "d":
    if m.PendingChord == "d" {
        m.PendingChord = ""
        if m.SelectedID != "" {
            return m, deleteSessionCmd(m.Client, m.SelectedID)
        }
        return m, nil
    }
    m.PendingChord = "d"
    return m, nil
default:
    m.PendingChord = ""
```

(The `default` clears any pending chord on any other key. Place it at the end of the switch on `k.String()`.)

- [ ] **Add deleteSessionCmd at the bottom of update.go:**

```go
func deleteSessionCmd(c *client.Client, sessionID string) tea.Cmd {
    return func() tea.Msg {
        if err := c.Delete(sessionID); err != nil {
            return DaemonErrMsg{Err: err}
        }
        return nil
    }
}
```

- [ ] **Build + commit**

```bash
go build ./...
git add internal/tui/
git commit -m "tui: dd chord delete on selected session"
```

---

## Phase 4 — Command mode + quit confirm (3 tasks)

### Task C9: `:` command mode skeleton

**Files:** Create `internal/tui/command.go`. Modify `update.go`.

- [ ] **Create `internal/tui/command.go`:**

```go
package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tristanbietsch/rex/internal/client"
)

// executeCommand parses and runs a `:` command. Returns the updated model + an optional cmd.
func executeCommand(m Model, line string) (Model, tea.Cmd) {
	line = strings.TrimSpace(line)
	if line == "" {
		return m, nil
	}
	parts := strings.Fields(line)
	verb := parts[0]
	args := parts[1:]

	switch verb {
	case "q", "quit":
		m.Focus = FocusConfirmQuit
		return m, nil
	case "q!":
		m.Quitting = true
		return m, tea.Quit
	case "bg", "detach":
		m.Quitting = true
		return m, savePersistAndQuit(m)
	case "help":
		m.Focus = FocusHelp
		return m, nil
	case "reload":
		// Sends SIGHUP via the daemon — Plan D will wire daemon-side handler.
		// For now, this is a no-op with a status note.
		m.Err = "reload sent (handler is Plan D)"
		return m, nil
	case "filter":
		if len(args) == 1 {
			m.Filter = args[0]
		}
		return m, nil
	case "rm":
		if len(args) == 1 {
			id := resolveLocal(m, args[0])
			if id == "" {
				m.Err = "no match for " + args[0]
				return m, nil
			}
			return m, deleteSessionCmd(m.Client, id)
		}
	case "rename":
		if len(args) == 2 {
			id := resolveLocal(m, args[0])
			if id == "" {
				m.Err = "no match for " + args[0]
				return m, nil
			}
			return m, renameCmd(m.Client, id, args[1])
		}
	case "new":
		// Plan C wires the wizard via the keybind 'n'. Command-mode :new is an alias.
		m.Focus = FocusWizard
		return m, nil
	default:
		m.Err = "unknown command: " + verb
	}
	return m, nil
}

func resolveLocal(m Model, sel string) string {
	for _, s := range m.Sessions {
		if s.ID == sel || s.ShortID == sel || s.Slug == sel {
			return s.ID
		}
	}
	return ""
}

func renameCmd(c *client.Client, id, slug string) tea.Cmd {
	return func() tea.Msg {
		if err := c.Rename(id, slug, ""); err != nil {
			return DaemonErrMsg{Err: err}
		}
		return nil
	}
}

func savePersistAndQuit(m Model) tea.Cmd {
	return func() tea.Msg {
		_ = SaveTUIState(m)
		return tea.Quit()
	}
}
```

- [ ] **Add `:` handling to updateKey (in FocusBoard branch):**

```go
case ":":
    m.Focus = FocusCommand
    m.CmdText = ""
    return m, nil
```

- [ ] **Add a new FocusCommand branch** in `updateKey` (similar to the FocusPrompt branch):

```go
if m.Focus == FocusCommand {
    switch k.Type {
    case tea.KeyEsc:
        m.Focus = FocusBoard
        m.CmdText = ""
        return m, nil
    case tea.KeyEnter:
        cmd := m.CmdText
        m.CmdText = ""
        m.Focus = FocusBoard
        return executeCommand(m, cmd)
    case tea.KeyBackspace:
        if len(m.CmdText) > 0 {
            m.CmdText = m.CmdText[:len(m.CmdText)-1]
        }
        return m, nil
    case tea.KeyRunes:
        m.CmdText += string(k.Runes)
        return m, nil
    case tea.KeySpace:
        m.CmdText += " "
        return m, nil
    }
}
```

- [ ] **Add the SaveTUIState stub** in `internal/tui/persist.go` (full impl in Task C13):

```go
package tui

// SaveTUIState is a placeholder — Task C13 implements writing to ~/.local/state/rex/tui-state.json.
func SaveTUIState(m Model) error { _ = m; return nil }

// LoadTUIState is a placeholder — Task C13 implements reading + cleanup.
func LoadTUIState() (selection string, filter string, ok bool) { return "", "", false }
```

- [ ] **Build + commit**

```bash
go build ./...
git add internal/tui/
git commit -m "tui: : command mode — q/q!/bg/help/reload/filter/rm/rename/new"
```

---

### Task C10: Quit confirmation overlay

**Files:** Create `internal/tui/confirm.go`. Modify `update.go`, `tui.go`.

- [ ] **Create `internal/tui/confirm.go`:**

```go
package tui

import "github.com/charmbracelet/lipgloss"

func renderQuitConfirm(m Model) string {
	prompt := styleStateNeeds.Render("?") + " " +
		styleSlug.Render("quit rex? running sessions stay alive in the daemon — ") +
		styleDim.Render("y / N")
	hint := styleDim.Render("y or enter to quit · n or esc to cancel")
	return lipgloss.NewStyle().Padding(1, 2).Render(prompt + "\n" + hint)
}
```

- [ ] **Add FocusConfirmQuit branch in `updateKey`:**

```go
if m.Focus == FocusConfirmQuit {
    switch k.String() {
    case "y", "Y", "enter":
        m.Quitting = true
        return m, tea.Quit
    case "n", "N", "esc":
        m.Focus = FocusBoard
        return m, nil
    }
    return m, nil
}
```

- [ ] **Inject into View** in `tui.go`:

Replace the View function:

```go
func (m Model) View() string {
    if m.Quitting {
        return ""
    }
    base := renderHeader(m) + "\n\n" + renderBoard(m) + "\n" + renderPrompt(m) + "\n" + renderHelpLine(m) + "\n"
    if m.Focus == FocusConfirmQuit {
        return base + "\n" + renderQuitConfirm(m)
    }
    return base
}
```

- [ ] **Build + commit**

```bash
go build ./...
git add internal/tui/
git commit -m "tui: quit confirmation overlay (yields on :q, bypassed by :q! and q keybind)"
```

---

### Task C11: Persist + restore TUI state on `:bg`

**Files:** Replace `internal/tui/persist.go` stub with real impl. Modify `tui.go` Run to restore on start.

- [ ] **Replace `internal/tui/persist.go`:**

```go
package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type persistedState struct {
	Selection    string    `json:"selection"`
	Filter       string    `json:"filter"`
	ScrollOffset int       `json:"scroll_offset"`
	SavedAt      time.Time `json:"saved_at"`
}

func statePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "rex", "tui-state.json")
}

// SaveTUIState writes selection + filter to ~/.local/state/rex/tui-state.json.
func SaveTUIState(m Model) error {
	path := statePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(persistedState{
		Selection: m.SelectedID,
		Filter:    m.Filter,
		SavedAt:   time.Now().UTC(),
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// LoadTUIState reads + deletes the persisted state. Returns ok=false when the file doesn't exist.
func LoadTUIState() (selection string, filter string, ok bool) {
	path := statePath()
	b, err := os.ReadFile(path)
	if err != nil {
		return "", "", false
	}
	var st persistedState
	if err := json.Unmarshal(b, &st); err != nil {
		return "", "", false
	}
	_ = os.Remove(path)
	return st.Selection, st.Filter, true
}
```

- [ ] **Restore at startup** — modify `Run` in `tui.go`:

After constructing the model, before starting the program:

```go
if sel, filt, ok := LoadTUIState(); ok {
    m.SelectedID = sel
    if filt != "" {
        m.Filter = filt
    }
}
```

- [ ] **Build + commit**

```bash
go build ./...
git add internal/tui/
git commit -m "tui: persist + restore selection/filter on :bg → rex round-trip"
```

---

## Phase 5 — Modal attach + wizard (3 tasks)

### Task C12: Modal attach (PTY viewport)

**Files:** Create `internal/tui/modal.go`. Modify `update.go` to handle Enter + Esc.

- [ ] **Create `internal/tui/modal.go`:**

```go
package tui

import (
	"encoding/json"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"

	"github.com/tristanbietsch/rex/internal/client"
	"github.com/tristanbietsch/rex/internal/protocol"
)

// ModalState lives on Model when Focus == FocusModal.
type ModalState struct {
	SessionID string
	Viewport  viewport.Model
	Buffer    strings.Builder
}

func openModal(m Model, sessionID string) (Model, tea.Cmd) {
	w := m.Width
	h := m.Height
	if w == 0 {
		w = 80
	}
	if h == 0 {
		h = 24
	}
	mw := w - 8
	mh := h - 6
	vp := viewport.New(mw, mh)
	m.Modal = &ModalState{SessionID: sessionID, Viewport: vp}
	m.Focus = FocusModal
	return m, subscribeSessionCmd(m.Client, sessionID)
}

func subscribeSessionCmd(c *client.Client, sessionID string) tea.Cmd {
	return func() tea.Msg {
		if err := c.Subscribe(sessionID); err != nil {
			return DaemonErrMsg{Err: err}
		}
		return nil
	}
}

func closeModal(m Model) Model {
	if m.Client != nil {
		_ = m.Client.Subscribe("") // unpin from session output
	}
	m.Modal = nil
	m.Focus = FocusBoard
	return m
}

func handleModalOutput(m Model, env protocol.Envelope) Model {
	if m.Modal == nil || env.Type != protocol.EventSessionOutput {
		return m
	}
	var so protocol.SessionOutput
	if err := json.Unmarshal(env.Data, &so); err != nil {
		return m
	}
	if so.SessionID != m.Modal.SessionID {
		return m
	}
	m.Modal.Buffer.Write(so.Bytes)
	m.Modal.Viewport.SetContent(m.Modal.Buffer.String())
	m.Modal.Viewport.GotoBottom()
	return m
}

func renderModal(m Model) string {
	if m.Modal == nil {
		return ""
	}
	var sess protocol.SessionSummary
	for _, s := range m.Sessions {
		if s.ID == m.Modal.SessionID {
			sess = s
			break
		}
	}
	header := lipgloss.NewStyle().Padding(0, 1).Render(
		styleSlug.Render(sess.Slug) + "  " + styleDim.Render(sess.ToolID+" · "+sess.ModelID) + "  " + styleStateWorking.Render("["+string(sess.State)+"]"),
	)
	body := m.Modal.Viewport.View()
	footer := styleDim.Render("esc to close")
	border := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorBorder).Padding(0, 1)
	return border.Render(header + "\n" + body + "\n" + footer)
}
```

- [ ] **Add `Modal *ModalState` field to `Model`** in `model.go`:

```go
type Model struct {
    // ... existing ...
    Modal *ModalState
}
```

- [ ] **Handle Enter to open modal in `updateKey` (FocusBoard branch):**

```go
case "enter":
    if m.SelectedID != "" {
        nm, cmd := openModal(m, m.SelectedID)
        return nm, cmd
    }
    return m, nil
```

- [ ] **Add FocusModal branch in updateKey:**

```go
if m.Focus == FocusModal {
    switch k.String() {
    case "esc":
        return closeModal(m), nil
    }
    // TODO: forward keystrokes as SendInput (skipped in Plan C; lands in Plan D
    // along with raw-mode handling). For now, esc to detach.
    return m, nil
}
```

- [ ] **Wire output to modal** in the `DaemonEventMsg` handler in `Update`:

```go
case DaemonEventMsg:
    m = m.applyEvent(msg.Env)
    if m.Modal != nil {
        m = handleModalOutput(m, msg.Env)
    }
    return m, listenDaemon(m.Client)
```

- [ ] **Inject into View** in `tui.go`:

```go
if m.Focus == FocusModal {
    return renderModal(m)
}
```

- [ ] **Build + commit**

```bash
go build ./...
git add internal/tui/
git commit -m "tui: modal — Enter opens session, Subscribe streams output, Esc closes (no input forward yet — Plan D)"
```

---

### Task C13: New-agent wizard (collapsed 3-step)

**Files:** Create `internal/tui/wizard.go`. Modify `update.go`.

To keep Plan C tractable, the wizard collapses to **3 steps** in v0: provider, model, confirm. Effort selection and the slug/title/cwd step land in Plan D. The result is a working spawn flow with sensible defaults.

- [ ] **Create `internal/tui/wizard.go`:**

```go
package tui

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tristanbietsch/rex/internal/client"
	"github.com/tristanbietsch/rex/internal/protocol"
	"github.com/tristanbietsch/rex/internal/registry"
)

type wizardStep int

const (
	wizProvider wizardStep = iota
	wizModel
	wizConfirm
)

// WizardState lives on Model when Focus == FocusWizard.
type WizardState struct {
	Step     wizardStep
	Reg      *registry.Registry
	Tools    []registry.Tool
	ToolIdx  int
	ModelIdx int
}

func openWizard(m Model) (Model, tea.Cmd) {
	reg, err := registry.Load(toolsConfigPath())
	if err != nil {
		m.Err = err.Error()
		return m, nil
	}
	visible := visibleTools(reg.Tools)
	if len(visible) == 0 {
		m.Err = "no tools enabled"
		return m, nil
	}
	m.Wizard = &WizardState{Step: wizProvider, Reg: reg, Tools: visible}
	m.Focus = FocusWizard
	return m, nil
}

func toolsConfigPath() string {
	home, _ := os.UserHomeDir()
	return home + "/.config/rex/tools.yaml"
}

func visibleTools(tools []registry.Tool) []registry.Tool {
	out := make([]registry.Tool, 0, len(tools))
	for _, t := range tools {
		if t.EnabledByDefault != nil && !*t.EnabledByDefault {
			continue
		}
		out = append(out, t)
	}
	return out
}

func updateWizardKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	if m.Wizard == nil {
		return m, nil
	}
	switch k.String() {
	case "esc":
		m.Wizard = nil
		m.Focus = FocusBoard
		return m, nil
	case "j", "down":
		switch m.Wizard.Step {
		case wizProvider:
			if m.Wizard.ToolIdx+1 < len(m.Wizard.Tools) {
				m.Wizard.ToolIdx++
				m.Wizard.ModelIdx = 0
			}
		case wizModel:
			tool := m.Wizard.Tools[m.Wizard.ToolIdx]
			if m.Wizard.ModelIdx+1 < len(tool.Models) {
				m.Wizard.ModelIdx++
			}
		}
		return m, nil
	case "k", "up":
		switch m.Wizard.Step {
		case wizProvider:
			if m.Wizard.ToolIdx > 0 {
				m.Wizard.ToolIdx--
				m.Wizard.ModelIdx = 0
			}
		case wizModel:
			if m.Wizard.ModelIdx > 0 {
				m.Wizard.ModelIdx--
			}
		}
		return m, nil
	case "b":
		if m.Wizard.Step > wizProvider {
			m.Wizard.Step--
		}
		return m, nil
	case "enter":
		switch m.Wizard.Step {
		case wizProvider, wizModel:
			m.Wizard.Step++
			return m, nil
		case wizConfirm:
			tool := m.Wizard.Tools[m.Wizard.ToolIdx]
			model := tool.Models[m.Wizard.ModelIdx]
			cwd, _ := os.Getwd()
			cmd := wizardLaunchCmd(m.Client, tool.ID, model.ID, cwd)
			m.Wizard = nil
			m.Focus = FocusBoard
			return m, cmd
		}
	}
	return m, nil
}

func wizardLaunchCmd(c *client.Client, toolID, modelID, cwd string) tea.Cmd {
	return func() tea.Msg {
		err := c.NewSession(protocol.NewSession{
			ToolID:  toolID,
			ModelID: modelID,
			Slug:    deriveSlugFromPrompt(modelID + "-" + currentTimestamp()),
			CWD:     cwd,
		})
		if err != nil {
			return DaemonErrMsg{Err: err}
		}
		return nil
	}
}

func currentTimestamp() string {
	return fmt.Sprintf("%d", os.Getpid())
}

func renderWizard(m Model) string {
	if m.Wizard == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(styleHeaderApp.Render(fmt.Sprintf("step %d / 3 — ", int(m.Wizard.Step)+1)))
	switch m.Wizard.Step {
	case wizProvider:
		b.WriteString(styleHeaderApp.Render("choose a provider\n\n"))
		for i, t := range m.Wizard.Tools {
			cursor := "  "
			if i == m.Wizard.ToolIdx {
				cursor = styleArrow.Render("▸ ")
			}
			b.WriteString(cursor + styleSlug.Render(t.Name) + "  " + styleDim.Render("("+t.Category+")") + "\n")
		}
	case wizModel:
		tool := m.Wizard.Tools[m.Wizard.ToolIdx]
		b.WriteString(styleHeaderApp.Render("choose a model — " + tool.Name + "\n\n"))
		for i, mm := range tool.Models {
			cursor := "  "
			if i == m.Wizard.ModelIdx {
				cursor = styleArrow.Render("▸ ")
			}
			b.WriteString(cursor + styleSlug.Render(mm.Name) + "\n")
		}
	case wizConfirm:
		tool := m.Wizard.Tools[m.Wizard.ToolIdx]
		model := tool.Models[m.Wizard.ModelIdx]
		b.WriteString(styleHeaderApp.Render("confirm and launch\n\n"))
		b.WriteString(styleSlug.Render(tool.Name) + " · " + styleSlug.Render(model.Name) + "\n\n")
		b.WriteString(styleArrow.Render("▸ ") + styleHeaderApp.Render("[ launch ]") + "\n")
	}
	b.WriteString("\n" + styleDim.Render("j/k select · enter next · b back · esc cancel"))
	return b.String()
}
```

- [ ] **Add WizardState field to Model:**

```go
type Model struct {
    // ... existing ...
    Wizard *WizardState
}
```

- [ ] **Wire `n` keybind to open wizard** in updateKey (FocusBoard branch):

```go
case "n":
    return openWizard(m)
```

- [ ] **Add FocusWizard branch in updateKey:**

```go
if m.Focus == FocusWizard {
    return updateWizardKey(m, k)
}
```

- [ ] **Render wizard in View:**

```go
if m.Focus == FocusWizard {
    return renderWizard(m)
}
```

- [ ] **Build + commit**

```bash
go build ./...
git add internal/tui/
git commit -m "tui: new-agent wizard (3-step: provider → model → confirm) — n keybind"
```

---

### Task C14: Help overlay (`?`)

**Files:** Create `internal/tui/help.go`. Modify `update.go` for `?`. Modify `tui.go` View.

- [ ] **Create `internal/tui/help.go`:**

```go
package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func renderHelp() string {
	rows := []string{
		bold("Navigation"),
		"  j k           move row selection",
		"  g G           top / bottom",
		"  1 2 3         jump to Needs input / Working / Completed",
		"  t             cycle tool filter",
		"",
		bold("Actions"),
		"  enter         open modal on selected session",
		"  n             new-agent wizard",
		"  dd            delete selected",
		"  i             focus λ prompt (new-session text)",
		"",
		bold("Modes"),
		"  :             command mode",
		"  ?             this help",
		"  q             quit (no confirm)",
		"",
		bold("Commands"),
		"  :q :quit      quit (confirm)",
		"  :q!           force quit",
		"  :bg :detach   detach, save place",
		"  :new          new-agent wizard",
		"  :filter <t>   set filter chip",
		"  :rm <sel>     delete",
		"  :rename <s> <new>",
		"  :reload       reload tools.yaml",
		"  :help         this overlay",
		"",
		dim("esc to close"),
	}
	return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(rows, "\n"))
}

func bold(s string) string { return styleSlug.Render(s) }
func dim(s string) string  { return styleDim.Render(s) }
```

- [ ] **Add `?` handling in updateKey (FocusBoard):**

```go
case "?":
    m.Focus = FocusHelp
    return m, nil
```

- [ ] **Add FocusHelp branch:**

```go
if m.Focus == FocusHelp {
    switch k.String() {
    case "esc", "?":
        m.Focus = FocusBoard
        return m, nil
    }
    return m, nil
}
```

- [ ] **Render in View:**

```go
if m.Focus == FocusHelp {
    return renderHelp()
}
```

- [ ] **Build + commit**

```bash
go build ./...
git add internal/tui/
git commit -m "tui: help overlay — ? keybind, four titled sections, esc closes"
```

---

## Phase 6 — Mouse + acceptance (3 tasks)

### Task C15: Mouse click selects row, double-click opens modal

**Files:** Modify `update.go`.

- [ ] **Add tea.MouseMsg handling in Update:**

```go
case tea.MouseMsg:
    return updateMouse(m, msg)
```

- [ ] **Add the handler at the bottom of update.go:**

```go
import bbtea "github.com/charmbracelet/bubbletea" // alias to avoid clash if needed; or just use tea

var lastClickRow = -1
var lastClickTime = time.Time{}

func updateMouse(m Model, msg tea.MouseMsg) (Model, tea.Cmd) {
    if msg.Type != tea.MouseLeft && msg.Action != tea.MouseActionPress {
        return m, nil
    }
    row := msg.Y
    rows := orderedSessions(m)
    // Best-effort: the board starts at approximately Y=3 (header takes ~3 lines).
    // Lines per section: 1 title + N rows + 1 blank. We collapse this into a flat
    // ordered list and use the row offset.
    sessionIdx := row - 4
    if sessionIdx < 0 || sessionIdx >= len(rows) {
        lastClickRow = -1
        return m, nil
    }
    m.SelectedID = rows[sessionIdx].ID
    now := time.Now()
    if lastClickRow == sessionIdx && now.Sub(lastClickTime) < 350*time.Millisecond {
        // double-click
        return openModal(m, rows[sessionIdx].ID)
    }
    lastClickRow = sessionIdx
    lastClickTime = now
    return m, nil
}
```

Imports: add `"time"`.

(Note: row-offset heuristic is approximate. Plan D will compute exact row coordinates after layout.)

- [ ] **Build + commit**

```bash
go build ./...
git add internal/tui/
git commit -m "tui: mouse — click selects row, double-click opens modal (heuristic row offset)"
```

---

### Task C16: Done-blink animation

**Files:** Modify `internal/tui/model.go` (add blink state). Modify `internal/tui/board.go` for render. Modify `update.go` for transitions.

- [ ] **Track which sessions are mid-blink + when blink started.** Add to Model:

```go
type Model struct {
    // ... existing ...
    BlinkUntil map[string]time.Time
}
```

(Initialize the map in `tui.go` Run: `m.BlinkUntil = make(map[string]time.Time)`.)

- [ ] **Set blink on transition to done.** In `applyEvent`'s `EventSessionUpdated` branch, after `applyPatch`:

```go
if s, ok := patch["state"].(string); ok && protocol.State(s) == protocol.StateDone {
    if m.BlinkUntil == nil {
        m.BlinkUntil = make(map[string]time.Time)
    }
    m.BlinkUntil[upd.SessionID] = time.Now().Add(1600 * time.Millisecond)
}
```

(Add `"time"` to model.go imports.)

- [ ] **Render blink** in `renderRow`:

```go
if until, ok := m.BlinkUntil[s.ID]; ok && time.Now().Before(until) {
    // Blink at 2.5Hz: alternate background tint.
    if time.Now().UnixMilli()/200%2 == 0 {
        row = lipgloss.NewStyle().Background(colorDone).Render(row)
    }
}
```

(Add lipgloss and time imports to board.go if not already there.)

- [ ] **Build + commit**

```bash
go build ./...
git add internal/tui/
git commit -m "tui: done-blink — 1.6s 4-cycle background flash on transition to done"
```

---

### Task C17: Smoke test recipe + acceptance

**Files:** Create `docs/superpowers/plans/2026-05-14-rex-tui-smoke.md`.

- [ ] **Create the doc:**

````markdown
# Plan C smoke test

Manual end-to-end check of the TUI.

## Setup

```sh
make build-all
rm -f /tmp/rex.sock; rm -rf /tmp/rex-state
./rex-daemon --socket /tmp/rex.sock --state-dir /tmp/rex-state &
export REX_SOCKET=/tmp/rex.sock
```

## 1. Open the TUI

```sh
./rex   # opens the Bubble Tea board
```

Expect: `∴ REX` header, three sections (Needs input / Working / Completed), each currently `(none)`, λ prompt at the bottom, help bar showing keybind hints.

## 2. Spawn via λ prompt

- Press `i` — cursor enters the λ prompt.
- Type `hello world` and press enter.
- A new session appears under Working (the echo tool's short script), spinner ticks.
- Within a few seconds, it migrates to Completed.

## 3. Spawn via wizard

- Press `n` — wizard opens. Select Echo, enter. Select a model, enter. Confirm.
- A new session appears.

## 4. Selection + jump

- j / k moves selection.
- 1 / 2 / 3 jumps between sections.
- Watch the spinner tick on Working rows.

## 5. Filter

- Press `t` to cycle the filter chip. Only sessions for the selected tool show.

## 6. Delete

- Select a session. Press `d` then `d` (chord). Session disappears.

## 7. Modal

- Select a Working session. Press `enter` — modal opens with PTY output.
- Press `esc` — back to the board.

## 8. Command mode + quit

- Press `:` to enter command mode. Type `q` and enter.
- Confirm prompt appears. Press `n` — back to board.
- Press `:` again, type `q!` and enter — quits immediately.

## 9. Background + reattach

- Open TUI again. Select a running session.
- Press `:`, type `bg`, enter.
- TUI exits. Run `rex` again — board reopens with same selection and filter restored.

## Acceptance

Plan C is **done** when steps 1–9 all work and `make test` is green.
````

- [ ] **Build + final tests + commit**

```bash
go test ./... -count=1 -race
make build-all
git add docs/superpowers/plans/2026-05-14-rex-tui-smoke.md
git commit -m "docs: Plan C smoke test recipe"
```

NO Claude trailer.

---

## Acceptance criteria

Plan C is **done** when:

1. `make test` is green (all packages including `internal/tui` if/where unit tests exist).
2. `make build-all` produces `rex` and `rex-daemon`.
3. `./rex` (no args) opens the TUI.
4. The smoke recipe at `docs/superpowers/plans/2026-05-14-rex-tui-smoke.md` passes steps 1–9.
5. λ prompt, wizard, modal, command mode, quit confirm, background, help overlay all work.

## Self-review notes

- **Spec coverage**: header, board sections, rows with spinner/needs/done glyphs, λ prompt, dd chord delete, `:` command mode, quit confirm, background+restore, modal PTY view (read-only in Plan C), 3-step wizard (collapsed from 5; effort + name steps in Plan D), help overlay, mouse click + double-click.
- **Deferred to Plan D**: input-forwarding inside the modal (raw-mode key capture); `/` slash palette UI; settings page UI + Lua config layer + persistent registry; section-slide / modal-scale-in animations; theme variants (`noir`, `paper`); row-density variants; full 5-step wizard with effort + name + cwd steps.
- **Known approximations**: mouse row-offset heuristic; spinner is 10fps tick-driven (good enough); done-blink uses wall-clock not exact frame timing.
- **Commit hygiene**: zero `Co-Authored-By: Claude` trailers anywhere — every commit message reviewed manually.
