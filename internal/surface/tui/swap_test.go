package tui

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/version"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func swapBoard() Model {
	m := boardModel(
		protocol.SessionSummary{ID: "s1", Slug: "one", State: protocol.StateWorking},
		protocol.SessionSummary{ID: "s2", Slug: "two", State: protocol.StateNeedsInput},
	)
	m.SelectedID = "s2"
	m.Filter = "claude"
	m.Search = "fix"
	m.ScrollOffset = 3
	m.Hidden = map[string]bool{boardGroups[0].ID: true}
	return m
}

func TestSwapStateRoundTrip(t *testing.T) {
	m := swapBoard()
	m.Focus = FocusSettings
	path, err := writeSwapState(swapStateOf(m))
	require.NoError(t, err)
	fi, err := os.Stat(path)
	require.NoError(t, err)
	if runtime.GOOS != "windows" { // no Unix modes there
		require.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
	}

	st, err := readSwapState(path)
	require.NoError(t, err)
	require.Equal(t, "s2", st.Selection)
	require.Equal(t, "claude", st.Filter)
	require.Equal(t, "fix", st.Search)
	require.Equal(t, 3, st.Scroll)
	require.Equal(t, "settings", st.Page)
	require.Equal(t, []string{boardGroups[0].ID}, st.Hidden)
	_, err = os.Stat(path)
	require.True(t, os.IsNotExist(err), "read removes the file")
}

func TestSwapStateRefusesOtherPIDAndOpenMode(t *testing.T) {
	st := swapStateOf(swapBoard())
	st.PID = os.Getpid() + 1
	path, err := writeSwapState(st)
	require.NoError(t, err)
	_, err = readSwapState(path)
	require.Error(t, err, "another board's file")

	st.PID = os.Getpid()
	path, err = writeSwapState(st)
	require.NoError(t, err)
	if runtime.GOOS == "windows" {
		return // no Unix modes: the per-user state folder is the guard
	}
	require.NoError(t, os.Chmod(path, 0o644)) //nolint:gosec // G302: the test needs a group/world-readable file.
	_, err = readSwapState(path)
	require.Error(t, err)
}

// A new build shows on the status line; with auto_swap on and the board
// idle for a minute, the board quits to swap.
func TestNewBuildNoticeThenIdleSwap(t *testing.T) {
	m := swapBoard()
	now := time.Now()
	m.Clock = func() time.Time { return now }
	m.lastInput = now // busy
	nm, cmd := m.Update(binChangedMsg{changed: true})
	got := nm.(Model)
	require.True(t, got.newBuild)
	require.Equal(t, newBuildNotice, got.Notice)
	require.Empty(t, got.swapPath, "not idle yet")
	require.NotNil(t, cmd)

	got.lastInput = now.Add(-2 * swapIdle)
	nm, cmd = got.Update(binChangedMsg{})
	got = nm.(Model)
	require.NotEmpty(t, got.swapPath, "idle on the board: swap")
	require.True(t, got.Quitting)
	requireQuit(t, cmd)
	t.Cleanup(func() { _ = os.Remove(got.swapPath) })
}

// No unattended swap while something is being typed or the attach popup
// is open.
func TestNoAutoSwapWhileBusy(t *testing.T) {
	for _, f := range []Focus{FocusWizard, FocusNewAgent, FocusPrompt, FocusConfirmDelete, FocusRename} {
		m := swapBoard()
		m.Focus = f
		m.newBuild = true
		m.lastInput = time.Now().Add(-time.Hour)
		nm, _ := m.Update(binChangedMsg{})
		require.Empty(t, nm.(Model).swapPath, "focus %v", f)
	}
}

// rex upgrade's swap event: a board on another build swaps at once; one
// on the target build ignores it; a busy board waits.
func TestSwapEvent(t *testing.T) {
	ev := func(build string) DaemonEventMsg {
		b, _ := json.Marshal(protocol.SwapBoards{Build: build})
		return DaemonEventMsg{Env: protocol.Envelope{Kind: protocol.KindEvent, Type: protocol.EventSwap, Data: b}}
	}
	m := swapBoard()
	nm, _ := m.Update(ev(version.Build()))
	require.Empty(t, nm.(Model).swapPath, "already on that build")

	nm, _ = m.Update(ev("another"))
	got := nm.(Model)
	require.NotEmpty(t, got.swapPath)
	_ = os.Remove(got.swapPath)

	m.Focus = FocusWizard
	nm, _ = m.Update(ev("another"))
	got = nm.(Model)
	require.Empty(t, got.swapPath)
	require.True(t, got.swapWanted)
	got.Focus = FocusBoard
	nm, _ = got.Update(binChangedMsg{})
	require.NotEmpty(t, nm.(Model).swapPath, "swaps once the wizard is closed")
	_ = os.Remove(nm.(Model).swapPath)
}

// A swap reopens the page that was open.
func TestApplySwapPageReopensPage(t *testing.T) {
	m := swapBoard()
	m.swapIn = &swapState{Page: "help", Scroll: 2}
	nm, _ := applySwapPage(m)
	require.Equal(t, FocusHelp, nm.Focus)
	require.Equal(t, 2, nm.ScrollOffset)
	require.Nil(t, nm.swapIn)
}

// The attach connection dropped by the daemon (a hot restart) closes the
// popup and schedules a re-attach instead of showing "session ended".
func TestAttachConnLossReattaches(t *testing.T) {
	m := swapBoard()
	m.Attach = &AttachState{SessionID: "s1"}
	m.Focus = FocusAttach
	nm, cmd := m.Update(AttachClosedMsg{SessionID: "s1", Err: io.EOF})
	got := nm.(Model)
	require.Nil(t, got.Attach)
	require.Equal(t, "s1", got.reattachFor)
	require.NotNil(t, cmd)

	// The user moved on: no re-attach.
	got.Focus = FocusHelp
	nm, _ = got.Update(reattachMsg{session: "s1"})
	require.Nil(t, nm.(Model).Attach)
}

func requireQuit(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	require.NotNil(t, cmd)
	found := false
	var walk func(tea.Msg)
	walk = func(msg tea.Msg) {
		switch v := msg.(type) {
		case tea.QuitMsg:
			found = true
		case tea.BatchMsg:
			for _, c := range v {
				if c != nil {
					walk(c())
				}
			}
		}
	}
	walk(cmd())
	require.True(t, found, "want tea.Quit")
}

// R20 review: REX_TUI_SWAP may only name this board's own swap file; any
// other path is refused and left alone (not removed).
func TestSwapStateRefusesAnotherPath(t *testing.T) {
	other := filepath.Join(t.TempDir(), "precious.json")
	require.NoError(t, os.WriteFile(other, []byte("{}"), 0o600))
	_, err := readSwapState(other)
	require.ErrorContains(t, err, "not this board's swap file")
	_, err = os.Stat(other)
	require.NoError(t, err, "the named file is not removed")
}

// The binary watch sees an install (size or mtime change) of the stamped
// path, and nothing when it is unchanged.
func TestCheckBinarySeesAnInstall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rex")
	require.NoError(t, os.WriteFile(path, []byte("v1"), 0o600))
	st, ok := stampBinary(path)
	require.True(t, ok)
	require.Equal(t, binChangedMsg{changed: false}, checkBinary(st)())
	require.NoError(t, os.WriteFile(path, []byte("v2-longer"), 0o600))
	require.Equal(t, binChangedMsg{changed: true}, checkBinary(st)())
	_, ok = stampBinary(filepath.Join(t.TempDir(), "missing"))
	require.False(t, ok)
	_, ok = ownBinary()
	require.True(t, ok)
	require.Contains(t, (&SwapRequest{Path: "/p"}).Error(), "/p")
}

// After a swap, the attach popup comes back only for a session that still
// runs; otherwise the saved page reopens.
func TestApplySwapPage(t *testing.T) {
	m := swapBoard()
	m.swapIn = &swapState{Scroll: 2, Page: "help", Attach: "gone"}
	nm, _ := applySwapPage(m)
	require.Equal(t, 2, nm.ScrollOffset)
	require.Equal(t, FocusHelp, nm.Focus)
	require.Nil(t, nm.swapIn)
	nm, _ = applySwapPage(nm)
	require.Equal(t, FocusHelp, nm.Focus, "nothing to apply twice")
	require.True(t, liveState(protocol.StateQueued))
	require.False(t, liveState(protocol.StateDone))
}

// A re-attach is dropped once the user moved on, and gives up after its
// last try.
func TestHandleReattach(t *testing.T) {
	m := swapBoard()
	m.reattachFor = "s1"
	m.Focus = FocusHelp
	nm, cmd := handleReattach(m, reattachMsg{session: "s1"})
	require.Nil(t, cmd)
	require.Nil(t, nm.Attach)

	m.Focus = FocusBoard
	m.reattachFor = "nope"
	nm, cmd = handleReattach(m, reattachMsg{session: "nope", attempt: reattachTries - 1})
	require.Nil(t, cmd)
	require.Empty(t, nm.reattachFor, "last try: give up")

	m.reattachFor = "nope"
	_, cmd = handleReattach(m, reattachMsg{session: "nope", attempt: 0})
	require.NotNil(t, cmd, "another try is scheduled")
	require.NotNil(t, scheduleReattach("x", 9))
}

// A kept new-agent draft never goes into the swap file (R16 D4: memory
// only). An idle auto swap waits while one is kept; an explicit swap drops
// it and the new image says so.
func TestSwapDropsNewAgentDraftWithNotice(t *testing.T) {
	m := swapBoard()
	draft := &NewAgentState{}
	draft.Task.Insert("secret task text")
	m.newAgentDraft = draft
	m.newBuild = true
	m.lastInput = time.Now().Add(-time.Hour)
	require.True(t, swapSafe(m))
	nm, _ := m.Update(binChangedMsg{})
	require.Empty(t, nm.(Model).swapPath, "no unattended swap over a kept draft")

	st := swapStateOf(m)
	require.True(t, st.DraftDropped)
	path, err := writeSwapState(st)
	require.NoError(t, err)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "secret task text", "draft text never on disk")
	got, err := readSwapState(path)
	require.NoError(t, err)

	b := swapBoard()
	b.swapIn = &got
	nb, _ := applySwapPage(b)
	require.Equal(t, draftDroppedNotice, nb.Notice)

	require.False(t, swapStateOf(swapBoard()).DraftDropped, "no draft, no notice")
}
