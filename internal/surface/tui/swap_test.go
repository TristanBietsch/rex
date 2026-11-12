package tui

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
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
	require.Equal(t, os.FileMode(0o600), fi.Mode().Perm())

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
	for _, f := range []Focus{FocusWizard, FocusPrompt, FocusConfirmDelete, FocusRename} {
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
