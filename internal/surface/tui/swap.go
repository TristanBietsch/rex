package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/runtime/rexlog"
	"github.com/tristanbietsch/rex/internal/version"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Board hot swap (R20): the board re-execs itself into a newly installed
// binary and the new image restores its view from a swap state file.

// SwapEnv names the swap state file the new image restores (set by the
// old image on its exec).
const SwapEnv = "REX_TUI_SWAP"

const (
	// binCheckEvery is how often the board stats its executable.
	binCheckEvery = 10 * time.Second
	// swapIdle is how long the board must be idle on the board before it
	// swaps on its own (auto_swap).
	swapIdle = 60 * time.Second
	// newBuildNotice is the status-line notice for a new build.
	newBuildNotice = "new rex build installed · :swap to load it"
)

// swapState is the view a swap carries over.
type swapState struct {
	PID       int       `json:"pid"`
	SavedAt   time.Time `json:"saved_at"`
	Selection string    `json:"selection,omitempty"`
	Filter    string    `json:"filter,omitempty"`
	Search    string    `json:"search,omitempty"`
	Hidden    []string  `json:"hidden_sections"`
	Scroll    int       `json:"scroll,omitempty"`
	// Page is the full-screen page or overlay that was open: help,
	// settings, usage, stats, overview, loops; "" for the board.
	Page string `json:"page,omitempty"`
	// Attach is the session whose attach popup was open.
	Attach string `json:"attach,omitempty"`
	// DraftDropped is set when a kept new-agent draft was left behind:
	// drafts are memory only (R16 D4) and never go into this file, so the
	// new image says so instead.
	DraftDropped bool `json:"draft_dropped,omitempty"`
}

// draftDroppedNotice tells the user a swap left the new-agent draft behind.
const draftDroppedNotice = "board swapped: the kept new-agent draft was dropped (drafts stay in memory only)"

// SwapRequest is returned by Run when the board quit to swap: the caller
// execs the new binary with SwapEnv=Path.
type SwapRequest struct{ Path string }

func (s *SwapRequest) Error() string { return "board swap requested (" + s.Path + ")" }

// pageOfFocus maps a focus that a swap reopens to its page name.
var pageOfFocus = map[Focus]string{
	FocusHelp: "help", FocusSettings: "settings", FocusUsage: "usage",
	FocusStats: "stats", FocusOverview: "overview", FocusLoops: "loops",
}

// pageRunners reopen a page by name.
var pageRunners = map[string]func(Model, cmdCall) (Model, tea.Cmd){
	"help": runHelp, "settings": runSettings, "usage": runUsage,
	"stats": runStats, "overview": runOverview, "loops": runLoops,
}

// swapStateOf captures m's view.
func swapStateOf(m Model) swapState {
	st := swapState{
		PID: os.Getpid(), SavedAt: time.Now().UTC(),
		Selection: m.SelectedID, Filter: m.Filter, Search: m.Search, Scroll: m.ScrollOffset,
		Page: pageOfFocus[m.Focus], Hidden: []string{},
		DraftDropped: m.newAgentDraft != nil || m.NewAgent != nil,
	}
	for _, g := range boardGroups {
		if m.Hidden[g.ID] {
			st.Hidden = append(st.Hidden, g.ID)
		}
	}
	if m.Attach != nil && !m.Attach.Ended {
		st.Attach = m.Attach.SessionID
	}
	return st
}

// swapPath is the swap state file of process pid.
func swapPath(pid int) (string, error) {
	state, err := paths.TUIStateFile()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(state), "tui-swap-"+strconv.Itoa(pid)+".json"), nil
}

// writeSwapState saves st (0600, atomic) and returns its path.
func writeSwapState(st swapState) (string, error) {
	path, err := swapPath(st.PID)
	if err != nil {
		return "", err
	}
	if err := rexlog.PrivateDir(filepath.Dir(path)); err != nil {
		return "", err
	}
	b, err := json.Marshal(st)
	if err != nil {
		return "", err
	}
	return path, writeFileAtomic(path, b)
}

// readSwapState loads and removes the swap state at path. It must be a
// regular file closed to group and other (Unix; Windows reports every file
// as 0666, so there the per-user state folder is the guard, as in
// privfile), written for this pid (exec keeps the pid), at most an hour old.
func readSwapState(path string) (swapState, error) {
	// Only this board's own file (REX_TUI_SWAP is an environment value):
	// anything else is neither read nor removed.
	want, err := swapPath(os.Getpid())
	if err != nil {
		return swapState{}, err
	}
	if filepath.Clean(path) != filepath.Clean(want) {
		return swapState{}, fmt.Errorf("swap state %s: not this board's swap file", path)
	}
	defer func() { _ = os.Remove(path) }()
	fi, err := os.Lstat(path)
	if err != nil {
		return swapState{}, err
	}
	if !fi.Mode().IsRegular() || (runtime.GOOS != "windows" && fi.Mode().Perm()&0o077 != 0) {
		return swapState{}, fmt.Errorf("swap state %s: not a private regular file", path)
	}
	if fi.Size() > 1<<20 {
		return swapState{}, errors.New("swap state too large")
	}
	b, err := os.ReadFile(path) //nolint:gosec // G304: path checked above.
	if err != nil {
		return swapState{}, err
	}
	var st swapState
	if err := json.Unmarshal(b, &st); err != nil {
		return swapState{}, err
	}
	if st.PID != os.Getpid() {
		return swapState{}, fmt.Errorf("swap state is for pid %d", st.PID)
	}
	if time.Since(st.SavedAt) > time.Hour {
		return swapState{}, errors.New("swap state is stale")
	}
	return st, nil
}

// binStamp identifies the installed binary at path: size and mtime.
type binStamp struct {
	path string
	size int64
	mod  time.Time
}

func stampBinary(path string) (binStamp, bool) {
	fi, err := os.Stat(path)
	if err != nil {
		return binStamp{}, false
	}
	return binStamp{path: path, size: fi.Size(), mod: fi.ModTime()}, true
}

// ownBinary is the board's executable stamp at start.
func ownBinary() (binStamp, bool) {
	exe, err := os.Executable()
	if err != nil {
		return binStamp{}, false
	}
	return stampBinary(exe)
}

// binTickMsg is the binary check timer.
type binTickMsg struct{}

// binChangedMsg reports the check: changed is true when the installed
// binary is no longer the one this board runs.
type binChangedMsg struct{ changed bool }

func binTick(done <-chan struct{}) tea.Cmd {
	return func() tea.Msg {
		t := time.NewTimer(binCheckEvery)
		defer t.Stop()
		select {
		case <-t.C:
			return binTickMsg{}
		case <-done:
			return nil
		}
	}
}

// armBinWatch starts the binary check once the board runs.
func armBinWatch(m Model) (Model, tea.Cmd) {
	if m.idleDone == nil || m.binTicking || m.Focus == FocusBoot || m.bin.path == "" {
		return m, nil
	}
	m.binTicking = true
	return m, binTick(m.idleDone)
}

func checkBinary(b binStamp) tea.Cmd {
	return func() tea.Msg {
		cur, ok := stampBinary(b.path)
		return binChangedMsg{changed: ok && (cur.size != b.size || !cur.mod.Equal(b.mod))}
	}
}

// swapBlockingFocus are views a swap must never interrupt: something is
// being typed or confirmed.
var swapBlockingFocus = map[Focus]bool{
	FocusBoot: true, FocusFail: true, FocusWizard: true, FocusPrompt: true, FocusCommand: true,
	FocusConfirmQuit: true, FocusConfirmDelete: true, FocusConfirmLoopStop: true,
	FocusRename: true, FocusSearch: true, FocusUndo: true, FocusNewAgent: true,
}

// swapSafe reports whether a swap would lose nothing being typed.
func swapSafe(m Model) bool { return !swapBlockingFocus[m.Focus] && m.reconnect == nil }

// autoSwapReady reports whether an unattended swap may run now: on the
// board itself, idle for swapIdle, with no kept new-agent draft (a swap
// drops it, so only an explicit :swap or rex upgrade may).
func autoSwapReady(m Model) bool {
	return m.Focus == FocusBoard && m.Attach == nil && m.newAgentDraft == nil && swapSafe(m) &&
		m.now().Sub(m.lastInput) >= swapIdle
}

// updateSwap handles the swap messages; ok is false for any other message.
func updateSwap(m Model, msg tea.Msg) (Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case binTickMsg:
		return m, checkBinary(m.bin), true
	case binChangedMsg:
		if msg.changed && !m.newBuild {
			m.newBuild = true
			m.Notice = newBuildNotice
			slog.Info("tui: new rex build installed")
		}
		if m.newBuild && m.featureOn("auto_swap") && autoSwapReady(m) {
			return startSwap(m, "idle")
		}
		if m.swapWanted && swapSafe(m) {
			return startSwap(m, "requested")
		}
		return m, binTick(m.idleDone), true
	case DaemonEventMsg:
		if msg.Env.Type != protocol.EventSwap {
			return m, nil, false
		}
		var p protocol.SwapBoards
		_ = json.Unmarshal(msg.Env.Data, &p)
		cmd := listenDaemon(m.Client)
		if p.Build != "" && p.Build == version.Build() {
			return m, cmd, true
		}
		if swapSafe(m) {
			nm, swap, _ := startSwap(m, "rex upgrade")
			return nm, tea.Batch(swap, cmd), true
		}
		m.swapWanted = true
		m.Notice = "rex upgrade: the board swaps once you finish here"
		return m, cmd, true
	case reattachMsg:
		nm, cmd := handleReattach(m, msg)
		return nm, cmd, true
	}
	return m, nil, false
}

// startSwap saves the view and quits the program; Run returns a
// SwapRequest and the caller execs the new binary.
func startSwap(m Model, why string) (Model, tea.Cmd, bool) {
	path, err := writeSwapState(swapStateOf(m))
	if err != nil {
		m.Err = "swap: " + err.Error()
		return m, binTick(m.idleDone), true
	}
	slog.Info("tui: swapping into the installed build", "why", why)
	m.swapPath = path
	m.Quitting = true
	return m, tea.Quit, true
}

// runSwap is :swap.
func runSwap(m Model, _ cmdCall) (Model, tea.Cmd) {
	nm, cmd, _ := startSwap(m, ":swap")
	return nm, cmd
}

// applySwapPage reopens the page and attach popup a swap carried over,
// once the board is up.
func applySwapPage(m Model) (Model, tea.Cmd) {
	st := m.swapIn
	m.swapIn = nil
	if st == nil {
		return m, nil
	}
	m.ScrollOffset = st.Scroll
	if st.DraftDropped {
		m.Notice = draftDroppedNotice
	}
	if st.Attach != "" {
		for _, s := range m.Sessions {
			if s.ID == st.Attach && liveState(s.State) {
				return openAttach(m, st.Attach)
			}
		}
	}
	if run := pageRunners[st.Page]; run != nil {
		return run(m, cmdCall{})
	}
	return m, nil
}

func liveState(s protocol.State) bool {
	return s == protocol.StateWorking || s == protocol.StateNeedsInput || s == protocol.StateQueued
}

// Re-attach after the daemon dropped the attach connection (a hot
// restart): the session still runs, so the popup comes back.

const (
	reattachBase  = 250 * time.Millisecond
	reattachMax   = 2 * time.Second
	reattachTries = 10
)

type reattachMsg struct {
	session string
	attempt int
}

func scheduleReattach(id string, attempt int) tea.Cmd {
	d := reattachBase
	for i := 0; i < attempt && d < reattachMax; i++ {
		d *= 2
	}
	d = min(d, reattachMax)
	return tea.Tick(d, func(time.Time) tea.Msg { return reattachMsg{session: id, attempt: attempt} })
}

// handleReattach reopens the popup unless the user moved on.
func handleReattach(m Model, msg reattachMsg) (Model, tea.Cmd) {
	if m.Focus != FocusBoard || m.Attach != nil || m.reattachFor != msg.session {
		return m, nil
	}
	nm, cmd := openAttach(m, msg.session)
	if nm.Attach != nil {
		nm.reattachFor = ""
		nm.Err = ""
		return nm, cmd
	}
	if msg.attempt+1 >= reattachTries {
		nm.reattachFor = ""
		return nm, nil
	}
	return nm, scheduleReattach(msg.session, msg.attempt+1)
}
