package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/hinshun/vt10x"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestEncodeMouse(t *testing.T) {
	sgr := vt10x.ModeMouseButton | vt10x.ModeMouseSgr
	up := tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress}
	down := tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress}
	require.Equal(t, "\x1b[<64;5;3M", string(encodeMouse(up, 4, 2, sgr)))
	require.Equal(t, "\x1b[<65;1;1M", string(encodeMouse(down, 0, 0, sgr)))
	left := tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, Ctrl: true}
	require.Equal(t, "\x1b[<16;2;2M", string(encodeMouse(left, 1, 1, sgr)))
	rel := tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease}
	require.Equal(t, "\x1b[<0;2;2m", string(encodeMouse(rel, 1, 1, sgr)))

	x10 := vt10x.ModeMouseButton
	require.Equal(t, []byte{0x1b, '[', 'M', 32 + 64, 32 + 5, 32 + 3}, encodeMouse(up, 4, 2, x10))
	require.Equal(t, []byte{0x1b, '[', 'M', 32 + 3, 32 + 2, 32 + 2}, encodeMouse(rel, 1, 1, x10))
	require.Nil(t, encodeMouse(up, 300, 2, x10), "legacy encoding can't address col > 223")
}

func TestWheelAsArrows(t *testing.T) {
	require.Equal(t, strings.Repeat("\x1b[A", 3), string(wheelAsArrows(true, vt10x.ModeAltScreen)))
	require.Equal(t, strings.Repeat("\x1bOB", 3), string(wheelAsArrows(false, vt10x.ModeAltScreen|vt10x.ModeAppCursor)))
}

func attachWithTerm(t *testing.T, w, h int, setup string) Model {
	t.Helper()
	m := boardModel(protocol.SessionSummary{ID: "a", Slug: "a", State: protocol.StateNeedsInput})
	m.Width, m.Height = w, h
	cols, rows := attachPopupInnerSize(w, h)
	term := vt10x.New(vt10x.WithSize(cols, rows))
	_, _ = term.Write([]byte(setup))
	m.Attach = &AttachState{SessionID: "a", Term: term, Cols: cols, Rows: rows}
	m.Focus = FocusAttach
	return m
}

func TestAttachInnerOrigin_MatchesRenderedPopup(t *testing.T) {
	for _, sz := range [][2]int{{120, 40}, {101, 33}, {200, 60}} {
		m := attachWithTerm(t, sz[0], sz[1], "Q")
		ox, oy := attachInnerOrigin(m)
		lines := strings.Split(ansi.Strip(m.View()), "\n")
		require.Greater(t, len(lines), oy)
		cell := ansi.Cut(lines[oy], ox, ox+1)
		require.Equal(t, "Q", cell, "size %v: origin (%d,%d) must be the terminal's top-left cell", sz, ox, oy)
	}
}

func TestAttachMouse_WheelBatchedForTrackingAgent(t *testing.T) {
	m := attachWithTerm(t, 120, 40, "\x1b[?1049h\x1b[?1000h\x1b[?1006h")
	ox, oy := attachInnerOrigin(m)
	wheel := tea.MouseMsg{X: ox + 2, Y: oy + 1, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress}

	m, cmd := updateMouse(m, wheel)
	require.NotNil(t, cmd, "first notch schedules a flush")
	m, cmd = updateMouse(m, wheel)
	require.Nil(t, cmd, "flush already pending")
	require.Equal(t, 2, m.Attach.wheelNotches)
	require.Equal(t, strings.Repeat("\x1b[<64;3;2M", 2), string(m.Attach.wheelBuf))

	// Outside the terminal area: ignored.
	m, _ = updateMouse(m, tea.MouseMsg{X: 0, Y: 0, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	require.Equal(t, 2, m.Attach.wheelNotches)

	// Stale flush ignored; current flush drains (ended session: nothing sent).
	m.Attach.Ended = true
	m, _ = flushAttachWheel(m, attachWheelFlushMsg{SessionID: "a", Seq: m.Attach.wheelSeq - 1})
	require.Equal(t, 2, m.Attach.wheelNotches)
	m, _ = flushAttachWheel(m, attachWheelFlushMsg{SessionID: "a", Seq: m.Attach.wheelSeq})
	require.Zero(t, m.Attach.wheelNotches)
	require.False(t, m.Attach.wheelFlushPending)
}

func TestAttachMouse_PrimaryScreenWheelWithoutHistoryIgnored(t *testing.T) {
	m := attachWithTerm(t, 120, 40, "")
	ox, oy := attachInnerOrigin(m)
	m, cmd := updateMouse(m, tea.MouseMsg{X: ox, Y: oy, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	require.Nil(t, cmd)
	require.Zero(t, m.Attach.wheelNotches)
}

func TestAttachMouse_AltScreenNoTrackingUsesArrows(t *testing.T) {
	m := attachWithTerm(t, 120, 40, "\x1b[?1049h")
	ox, oy := attachInnerOrigin(m)
	m, _ = updateMouse(m, tea.MouseMsg{X: ox, Y: oy, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	require.Equal(t, strings.Repeat("\x1b[B", 3), string(m.Attach.wheelBuf))
}

// Detach keeps the capture the board wants and releases it otherwise.
func TestDetach_MouseFollowsBoard(t *testing.T) {
	// Board wants the mouse (default): detach leaves it on.
	m := attachWithTerm(t, 120, 40, "")
	m.mouseOn = true
	m2, cmd := detach(m)
	require.Nil(t, m2.Attach)
	require.Nil(t, cmd)
	require.True(t, m2.mouseOn)

	// mouse_enabled turned off while attached: detach releases the capture.
	m = attachWithTerm(t, 120, 40, "")
	m.Store = settings.NewStore()
	require.NoError(t, m.Store.Set("mouse_enabled", false))
	m.mouseOn = true
	m2, cmd = detach(m)
	require.False(t, m2.mouseOn)
	require.IsType(t, tea.DisableMouse(), cmd())

	// Mouse off and never captured: nothing to release.
	m.mouseOn = false
	_, cmd = detach(m)
	require.Nil(t, cmd)
}

func TestAttachInnerOrigin_NarrowTerminalLongTitle(t *testing.T) {
	m := attachWithTerm(t, 48, 20, "Q")
	m.Sessions[0].Name = strings.Repeat("very long name ", 4)
	m.Sessions[0].ModelID = "a-model-with-a-long-identifier"
	ox, oy := attachInnerOrigin(m)
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	require.Equal(t, "Q", ansi.Cut(lines[oy], ox, ox+1))
}
