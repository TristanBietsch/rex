package tui

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/hinshun/vt10x"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// frameRec is one View() captured after an Update inside the real program.
type frameRec struct {
	at      time.Time
	tick    int
	running bool
	view    string
}

// viewRecorder collects frames from the program goroutine.
type viewRecorder struct {
	mu     sync.Mutex
	frames []frameRec
}

func (r *viewRecorder) add(f frameRec) {
	r.mu.Lock()
	r.frames = append(r.frames, f)
	r.mu.Unlock()
}

func (r *viewRecorder) since(t time.Time) []frameRec {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []frameRec
	for _, f := range r.frames {
		if !f.at.Before(t) {
			out = append(out, f)
		}
	}
	return out
}

func (r *viewRecorder) last() frameRec {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.frames) == 0 {
		return frameRec{}
	}
	return r.frames[len(r.frames)-1]
}

// idleInput is a keyboard that never types. Not WithInput(nil): bubbletea's
// Windows input loop reads a nil input and panics. Not a pipe either: on
// Windows, toggling mouse mode restarts the input reader, and a pipe read
// cannot be canceled, so the event loop would stall 500ms each time. Waking
// every few ms lets a canceled read return at once.
type idleInput struct{ done chan struct{} }

func newIdleInput() *idleInput { return &idleInput{done: make(chan struct{})} }

func (r *idleInput) Read([]byte) (int, error) {
	select {
	case <-r.done:
		return 0, io.EOF
	case <-time.After(5 * time.Millisecond):
		return 0, nil
	}
}

func (r *idleInput) Close() error { close(r.done); return nil }

// mutateMsg lets the test poke state the headless program can't reach via
// input (e.g. an attach popup without a daemon connection).
type mutateMsg struct{ fn func(Model) Model }

// probeModel wraps the real Model, delegating Init/Update/View and recording
// the rendered view after every message.
type probeModel struct {
	inner Model
	rec   *viewRecorder
}

func (p probeModel) Init() tea.Cmd { return p.inner.Init() }

func (p probeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if mm, ok := msg.(mutateMsg); ok {
		p.inner = mm.fn(p.inner)
	} else {
		var next tea.Model
		next, cmd = p.inner.Update(msg)
		p.inner = next.(Model)
	}
	p.rec.add(frameRec{
		at: time.Now(), tick: p.inner.SpinnerTick, running: p.inner.spinnerRunning, view: p.inner.View(),
	})
	return p, cmd
}

func (p probeModel) View() string { return p.inner.View() }

// spinnerGlyphsIn returns the distinct braille spinner frames found on lines
// containing marker across frames.
func spinnerGlyphsIn(frames []frameRec, marker string) map[string]bool {
	out := map[string]bool{}
	for _, f := range frames {
		for _, ln := range strings.Split(ansi.Strip(f.view), "\n") {
			if !strings.Contains(ln, marker) {
				continue
			}
			for _, g := range spinnerFrameSets["braille"] {
				if strings.Contains(ln, g) {
					out[g] = true
				}
			}
		}
	}
	return out
}

// rawLineVariants returns the distinct raw (ANSI-styled) lines containing
// every marker across frames — used for color-only animations (cursor, blink).
func rawLineVariants(frames []frameRec, markers ...string) map[string]bool {
	out := map[string]bool{}
	for _, f := range frames {
	lines:
		for _, ln := range strings.Split(f.view, "\n") {
			plain := ansi.Strip(ln)
			for _, mk := range markers {
				if !strings.Contains(plain, mk) {
					continue lines
				}
			}
			out[ln] = true
		}
	}
	return out
}

// fakeDaemon is a minimal UDS peer: the TUI's real client dials it and the
// test pushes SessionUpdated events down the wire to the listenDaemon loop.
type fakeDaemon struct {
	ln   net.Listener
	conn chan net.Conn
	w    *protocol.Writer
}

func newFakeDaemon(t *testing.T) *fakeDaemon {
	t.Helper()
	dir, err := os.MkdirTemp("", "rexsp") // short path: UDS names cap at ~104 bytes
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	ln, err := net.Listen("unix", filepath.Join(dir, "d.sock"))
	require.NoError(t, err)
	fd := &fakeDaemon{ln: ln, conn: make(chan net.Conn, 1)}
	go func() {
		c, err := ln.Accept()
		if err == nil {
			fd.conn <- c
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return fd
}

func (fd *fakeDaemon) socket() string { return fd.ln.Addr().String() }

// accept waits for the TUI's connection.
func (fd *fakeDaemon) accept(t *testing.T) {
	t.Helper()
	select {
	case c := <-fd.conn:
		fd.w = protocol.NewWriter(c)
		t.Cleanup(func() { _ = c.Close() })
	case <-time.After(5 * time.Second):
		t.Fatal("TUI never dialed the fake daemon")
	}
}

func (fd *fakeDaemon) setState(t *testing.T, id string, st protocol.State) {
	t.Helper()
	require.NoError(t, fd.w.WriteEvent(protocol.EventSessionUpdated, "",
		protocol.SessionUpdated{SessionID: id, Patch: map[string]any{"state": string(st)}}))
}

// TestSpinner_RuntimeHeadlessProgram runs the real Bubble Tea program loop
// (real tea.Tick timers, real Init/Update/View) through boot → board and
// every overlay, asserting the animations actually advance on screen.
func TestSpinner_RuntimeHeadlessProgram(t *testing.T) {
	if testing.Short() {
		t.Skip("runtime animation test takes a few seconds")
	}
	// Colors on so color-only animations (cursor blink, done blink) are
	// observable in View().
	prevProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prevProfile)

	sess := protocol.SessionSummary{
		ID: "s1", ShortID: "s1", Slug: "alpha", ToolID: "claude", ModelID: "opus",
		State: protocol.StateWorking, LastEventAt: time.Now(),
	}
	prevSeq := bootSequence
	defer func() { bootSequence = prevSeq }()
	fd := newFakeDaemon(t)
	bootSequence = []stepFunc{
		func(Model) tea.Cmd {
			return func() tea.Msg {
				c, err := client.Dial(fd.socket())
				return dialResultMsg{C: c, Err: err}
			}
		},
		func(Model) tea.Cmd {
			return func() tea.Msg {
				return snapshotResultMsg{Snap: &protocol.Snapshot{Sessions: []protocol.SessionSummary{sess}}}
			}
		},
	}

	store := settings.NewStore()
	rec := &viewRecorder{}
	m := Model{
		Focus: FocusBoot, Filter: "all", BootStart: time.Now(),
		Store: store, StorePath: filepath.Join(t.TempDir(), "settings.toml"),
	}
	in := newIdleInput()
	defer in.Close()
	p := tea.NewProgram(probeModel{inner: m, rec: rec},
		tea.WithInput(in), tea.WithOutput(io.Discard),
		tea.WithoutRenderer(), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() { _, err := p.Run(); done <- err }()
	defer func() {
		p.Quit()
		require.NoError(t, <-done)
	}()
	p.Send(tea.WindowSizeMsg{Width: 200, Height: 70})
	fd.accept(t)

	const window = 450 * time.Millisecond
	// requireSpinning asserts the alpha spinner shows several frames in the
	// next window, at ~10fps (no stacked chains).
	requireSpinning := func(phase, marker string) {
		t.Helper()
		start := time.Now()
		time.Sleep(window)
		fr := rec.since(start)
		require.NotEmpty(t, fr, "%s: no frames", phase)
		glyphs := spinnerGlyphsIn(fr, marker)
		require.GreaterOrEqual(t, len(glyphs), 3, "%s: spinner frozen (glyphs %v)", phase, glyphs)
		delta := fr[len(fr)-1].tick - fr[0].tick
		require.LessOrEqual(t, delta, 7, "%s: spinner ticking too fast (%d ticks in %v) — stacked chains", phase, delta, window)
	}
	send := func(msgs ...tea.Msg) {
		for _, msg := range msgs {
			p.Send(msg)
		}
	}
	waitIdleChain := func(phase string) {
		t.Helper()
		require.Eventually(t, func() bool { return !rec.last().running }, 2*time.Second, 20*time.Millisecond,
			"%s: spinner chain should stop when nothing animates", phase)
	}

	// 1. Boot → board (handoff after bootMinDuration) with a working session.
	require.Eventually(t, func() bool {
		return !strings.Contains(ansi.Strip(rec.last().view), "booting")
	}, 5*time.Second, 20*time.Millisecond, "boot never handed off to the board")
	requireSpinning("board after boot", "alpha")

	// 2. Session goes idle → chain stops.
	fd.setState(t, "s1", protocol.StateNeedsInput)
	waitIdleChain("idle")

	// 3. Works again → spinner resumes.
	fd.setState(t, "s1", protocol.StateWorking)
	requireSpinning("working again", "alpha")

	// 4. Help overlay open/close.
	send(key("?"))
	requireSpinning("help overlay", "alpha")
	send(key("esc"))

	// 5. Settings page open/close.
	send(key("S"))
	requireSpinning("settings", "alpha")
	send(key("esc"))

	// 6. Stats overlay via the command bar.
	send(key(":"))
	for _, r := range "stats" {
		send(key(string(r)))
	}
	send(key("enter"))
	requireSpinning("stats overlay", "alpha")
	send(key("esc"))

	// 7. Attach popup: the title-bar state chip spins.
	send(mutateMsg{fn: func(m Model) Model {
		cols, rows := attachPopupInnerSize(m.Width, m.Height)
		m.Attach = &AttachState{SessionID: "s1", Slug: "alpha", Term: vt10x.New(vt10x.WithSize(cols, rows)), Cols: cols, Rows: rows}
		m.Focus = FocusAttach
		return m
	}})
	requireSpinning("attach popup", " working")
	send(mutateMsg{fn: closeAttach})
	requireSpinning("attach closed", "alpha")

	// 8. Session finishes with nothing else working: the done row flashes.
	start := time.Now()
	fd.setState(t, "s1", protocol.StateDone)
	time.Sleep(700 * time.Millisecond) // flash toggles every 200ms for 1.6s
	v := rawLineVariants(rec.since(start), "alpha", "●")
	require.GreaterOrEqual(t, len(v), 2, "done blink frozen (variants %d)", len(v))
	waitIdleChain("after done blink")

	// 9. Nothing working: prompt / rename cursors still blink.
	cursorBlinks := func(phase, marker string) {
		t.Helper()
		start := time.Now()
		time.Sleep(700 * time.Millisecond) // > one 500ms on/off phase
		v := rawLineVariants(rec.since(start), marker)
		require.GreaterOrEqual(t, len(v), 2, "%s: cursor frozen", phase)
	}
	send(key("i"))
	cursorBlinks("quick-spawn prompt", "type then enter")
	send(key("esc"))
	waitIdleChain("prompt closed")
	send(key("g"), key("r")) // g selects the row (no state.restore in this boot)
	cursorBlinks("rename bar", "empty = show slug")
	send(key("esc"))
	waitIdleChain("rename closed")
}

func TestWantsSpinnerTick(t *testing.T) {
	reduced := settings.NewStore()
	require.NoError(t, reduced.Set("reduce_motion", true))
	idle := protocol.SessionSummary{ID: "a", Slug: "a", State: protocol.StateNeedsInput}
	working := protocol.SessionSummary{ID: "a", Slug: "a", State: protocol.StateWorking}
	cases := []struct {
		name string
		mod  func(Model) Model
		sess protocol.SessionSummary
		want bool
	}{
		{"idle board", nil, idle, false},
		{"working session", nil, working, true},
		{"working + reduce_motion", func(m Model) Model { m.Store = reduced; return m }, working, true},
		{"prompt cursor", func(m Model) Model { m.Focus = FocusPrompt; return m }, idle, true},
		{"command cursor", func(m Model) Model { m.Focus = FocusCommand; return m }, idle, true},
		{"rename cursor", func(m Model) Model { m.Focus = FocusRename; return m }, idle, true},
		{"wizard cursor", func(m Model) Model { m.Focus = FocusWizard; return m }, idle, true},
		{"prompt + reduce_motion", func(m Model) Model { m.Focus, m.Store = FocusPrompt, reduced; return m }, idle, false},
		{"help overlay idle", func(m Model) Model { m.Focus = FocusHelp; return m }, idle, false},
		{"done flash live", func(m Model) Model {
			m.BlinkUntil = map[string]time.Time{"a": time.Now().Add(time.Second)}
			return m
		}, idle, true},
		{"done flash expired", func(m Model) Model {
			m.BlinkUntil = map[string]time.Time{"a": time.Now().Add(-time.Second)}
			return m
		}, idle, false},
		{"done flash + reduce_motion", func(m Model) Model {
			m.Store = reduced
			m.BlinkUntil = map[string]time.Time{"a": time.Now().Add(time.Second)}
			return m
		}, idle, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := boardModel(tc.sess)
			if tc.mod != nil {
				m = tc.mod(m)
			}
			require.Equal(t, tc.want, m.wantsSpinnerTick())
		})
	}
}

// TestSpinner_UpdateArmsOnFocusChange: opening the prompt with nothing
// working starts the chain (cursor blink); closing it lets the chain stop.
func TestSpinner_UpdateArmsOnFocusChange(t *testing.T) {
	m := boardModel(protocol.SessionSummary{ID: "a", Slug: "a", State: protocol.StateNeedsInput})
	next, cmd := m.Update(key("i"))
	m = next.(Model)
	require.Equal(t, FocusPrompt, m.Focus)
	require.True(t, m.spinnerRunning)
	require.NotNil(t, cmd)
	gen := m.spinnerGen

	next, _ = m.Update(key("esc"))
	m = next.(Model)
	next, cmd = m.Update(SpinnerTickMsg{Gen: gen})
	m = next.(Model)
	require.Nil(t, cmd)
	require.False(t, m.spinnerRunning)
}
