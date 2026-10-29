package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/hinshun/vt10x"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// attachWithScrollback opens a popup model (no daemon client: anything that
// tried to reach the agent would panic) with scrollback capture on.
func attachWithScrollback(t *testing.T, limit int) Model {
	t.Helper()
	m := boardModel(protocol.SessionSummary{ID: "a", Slug: "a", State: protocol.StateNeedsInput})
	m.Width, m.Height = 120, 40
	cols, rows := attachPopupInnerSize(screenSize(m))
	st := &AttachState{SessionID: "a", Cols: cols, Rows: rows}
	st.Term = newAttachTerm(st, cols, rows, limit)
	m.Attach = st
	m.Focus = FocusAttach
	return m
}

func feedAttach(t *testing.T, m Model, s string) Model {
	t.Helper()
	out, _ := m.Update(AttachOutputMsg{SessionID: "a", Bytes: []byte(s)})
	return out.(Model)
}

func feedLines(t *testing.T, m Model, from, to int) Model {
	t.Helper()
	var b strings.Builder
	for i := from; i < to; i++ {
		fmt.Fprintf(&b, "L%03d\r\n", i)
	}
	return feedAttach(t, m, b.String())
}

// viewTopRow is the text of the popup's first terminal row.
func viewTopRow(t *testing.T, m Model) string {
	t.Helper()
	ox, oy := attachInnerOrigin(m)
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	require.Greater(t, len(lines), oy)
	return strings.TrimSpace(ansi.Cut(lines[oy], ox, ox+m.Attach.Cols))
}

func pressAttach(t *testing.T, m Model, k tea.KeyMsg) Model {
	t.Helper()
	out, _ := m.Update(k)
	return out.(Model)
}

func TestAttachScroll_CapturesPrimaryScreenHistory(t *testing.T) {
	m := attachWithScrollback(t, 5000)
	rows := m.Attach.Rows
	m = feedLines(t, m, 0, 100)
	// The last rows-1 lines plus the empty cursor row stay on screen.
	require.Equal(t, 100-(rows-1), m.Attach.sb.Len())
	require.Equal(t, "L000", lineText(m.Attach.sb.Line(0)))
	require.False(t, m.Attach.Scrolling())
	require.Contains(t, ansi.Strip(m.View()), "^PgUp scroll")
}

func lineText(l []vt10x.Glyph) string {
	var b strings.Builder
	for _, g := range l {
		b.WriteRune(g.Char)
	}
	return b.String()
}

func TestAttachScroll_KeysNavigateWithoutReachingAgent(t *testing.T) {
	m := attachWithScrollback(t, 5000)
	rows := m.Attach.Rows
	m = feedLines(t, m, 0, 100)
	total := m.Attach.sb.Len()
	liveTop := viewTopRow(t, m)

	m = pressAttach(t, m, tea.KeyMsg{Type: tea.KeyCtrlPgUp})
	require.Equal(t, rows-1, m.Attach.ScrollOff)
	require.Equal(t, fmt.Sprintf("L%03d", total-(rows-1)), viewTopRow(t, m))
	view := strings.Split(ansi.Strip(m.View()), "\n")
	_, oy := attachInnerOrigin(m)
	require.Contains(t, view[oy-2], fmt.Sprintf("SCROLL ↑%d/%d", rows-1, total), "title chip")
	require.Contains(t, view[oy+rows+1], "G/esc live", "scroll footer")

	steps := []struct {
		key  tea.KeyMsg
		want int
	}{
		{key("k"), rows},
		{key("j"), rows - 1},
		{tea.KeyMsg{Type: tea.KeyUp}, rows},
		{tea.KeyMsg{Type: tea.KeyCtrlU}, rows + rows/2},
		{tea.KeyMsg{Type: tea.KeyCtrlD}, rows},
		{tea.KeyMsg{Type: tea.KeyPgUp}, rows + rows - 1},
		{tea.KeyMsg{Type: tea.KeyCtrlF}, rows},
		{key("g"), total},
		{key("x"), total},                       // swallowed
		{tea.KeyMsg{Type: tea.KeyCtrlC}, total}, // swallowed too
		{key("G"), 0},
	}
	for i, s := range steps {
		m = pressAttach(t, m, s.key)
		require.Equal(t, s.want, m.Attach.ScrollOff, "step %d (%s)", i, s.key)
	}
	require.False(t, m.Attach.Scrolling())
	require.Equal(t, liveTop, viewTopRow(t, m))

	m = pressAttach(t, m, tea.KeyMsg{Type: tea.KeyCtrlPgUp})
	m = pressAttach(t, m, key("g"))
	require.Equal(t, "L000", viewTopRow(t, m))
	m = pressAttach(t, m, key("esc"))
	require.False(t, m.Attach.Scrolling())
}

func TestAttachScroll_ScrollingToBottomExits(t *testing.T) {
	m := attachWithScrollback(t, 5000)
	m = feedLines(t, m, 0, 100)
	m = pressAttach(t, m, tea.KeyMsg{Type: tea.KeyCtrlPgUp})
	m.Attach.ScrollOff = 2
	m = pressAttach(t, m, key("j"))
	require.True(t, m.Attach.Scrolling())
	m = pressAttach(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	require.False(t, m.Attach.Scrolling(), "reaching offset 0 leaves scroll mode")
}

func TestAttachScroll_NewOutputKeepsViewAnchored(t *testing.T) {
	m := attachWithScrollback(t, 5000)
	m = feedLines(t, m, 0, 100)
	m = pressAttach(t, m, tea.KeyMsg{Type: tea.KeyCtrlPgUp})
	top, off := viewTopRow(t, m), m.Attach.ScrollOff
	m = feedLines(t, m, 100, 105)
	require.Equal(t, off+5, m.Attach.ScrollOff)
	require.Equal(t, top, viewTopRow(t, m))
	require.Contains(t, ansi.Strip(m.View()), "↓ 5 new · G/esc to resume")

	// At the top of a full buffer the view can't stay put; offset clamps.
	m = pressAttach(t, m, key("g"))
	m = feedLines(t, m, 105, 110)
	require.Equal(t, m.Attach.sb.Len(), m.Attach.ScrollOff)
}

func TestAttachScroll_AltScreenExitsAndIsNotCaptured(t *testing.T) {
	m := attachWithScrollback(t, 5000)
	m = feedLines(t, m, 0, 100)
	n := m.Attach.sb.Len()
	m = pressAttach(t, m, tea.KeyMsg{Type: tea.KeyCtrlPgUp})
	m = feedAttach(t, m, "\x1b[?1049h")
	require.False(t, m.Attach.Scrolling(), "alt screen leaves scroll mode")
	m = feedLines(t, m, 0, 100)
	require.Equal(t, n, m.Attach.sb.Len(), "alt-screen output is not history")

	// ctrl+pgup on the alt screen does not enter scroll mode.
	m = pressAttach(t, m, tea.KeyMsg{Type: tea.KeyCtrlPgUp})
	require.False(t, m.Attach.Scrolling())
}

func TestAttachScroll_Wheel(t *testing.T) {
	m := attachWithScrollback(t, 5000)
	m = feedLines(t, m, 0, 100)
	ox, oy := attachInnerOrigin(m)
	up := tea.MouseMsg{X: ox, Y: oy, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress}
	down := up
	down.Button = tea.MouseButtonWheelDown

	m, cmd := updateMouse(m, down)
	require.Nil(t, cmd)
	require.False(t, m.Attach.Scrolling(), "wheel down at live does nothing")
	m, _ = updateMouse(m, up)
	m, _ = updateMouse(m, up)
	require.Equal(t, 2*attachScrollWheelLines, m.Attach.ScrollOff)
	require.Zero(t, m.Attach.wheelNotches, "nothing queued for the agent")
	m, _ = updateMouse(m, down)
	m, _ = updateMouse(m, down)
	require.False(t, m.Attach.Scrolling())

	// An agent with mouse tracking still gets the wheel.
	m = feedAttach(t, m, "\x1b[?1000h\x1b[?1006h")
	m, _ = updateMouse(m, up)
	require.False(t, m.Attach.Scrolling())
	require.Equal(t, 1, m.Attach.wheelNotches)
}

func TestAttachScroll_CommandBarAndColons(t *testing.T) {
	m := attachWithScrollback(t, 5000)
	m = feedLines(t, m, 0, 100)
	m = pressAttach(t, m, tea.KeyMsg{Type: tea.KeyCtrlPgUp})
	off := m.Attach.ScrollOff
	// A lone ':' followed by a scroll key: the colon is dropped, not sent.
	m = pressAttach(t, m, key(":"))
	m = pressAttach(t, m, key("k"))
	require.Equal(t, off+1, m.Attach.ScrollOff)
	m, _ = flushAttachHold(m, attachHoldTimeoutMsg{SessionID: "a", Seq: m.Attach.holdSeq})

	m = pressAttach(t, m, key(":"))
	m, _ = flushAttachHold(m, attachHoldTimeoutMsg{SessionID: "a", Seq: m.Attach.holdSeq})
	require.Zero(t, m.Attach.held, "expired colon dropped while scrolling")

	m = pressAttach(t, m, key(":"))
	m = pressAttach(t, m, key(":"))
	require.Equal(t, FocusCommand, m.Focus)

	m = attachWithScrollback(t, 5000)
	m = feedLines(t, m, 0, 100)
	m = pressAttach(t, m, tea.KeyMsg{Type: tea.KeyCtrlPgUp})
	m = pressAttach(t, m, tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})
	require.Nil(t, m.Attach, "ctrl+] still detaches")
}

func TestAttachScroll_RingBounds(t *testing.T) {
	m := attachWithScrollback(t, 10)
	m = feedLines(t, m, 0, 100)
	sb := m.Attach.sb
	require.Equal(t, 10, sb.Len())
	require.Equal(t, 100-(m.Attach.Rows-1)-10, sb.evicted)
	require.Equal(t, fmt.Sprintf("L%03d", 100-(m.Attach.Rows-1)-10), lineText(sb.Line(0)))

	// The byte cap holds no matter the line limit.
	big := newAttachScrollback("a", attachScrollbackMax)
	wide := make([]vt10x.Glyph, 100_000)
	for i := 0; i < 10; i++ {
		big.push(wide)
	}
	require.LessOrEqual(t, big.bytes, attachScrollbackMaxBytes)
	require.Equal(t, attachScrollbackMaxBytes/lineBytes(wide), big.Len())

	// A single over-cap line is still kept.
	huge := newAttachScrollback("a", 10)
	huge.push(make([]vt10x.Glyph, attachScrollbackMaxBytes))
	require.Equal(t, 1, huge.Len())
}

func TestAttachScroll_Setting(t *testing.T) {
	require.Equal(t, attachScrollbackDefault, attachScrollbackLimit(Model{}))
	store := settings.NewStore()
	require.Equal(t, attachScrollbackDefault, attachScrollbackLimit(Model{Store: store}))
	require.NoError(t, store.Set("attach_scrollback_lines", 0))
	require.Zero(t, attachScrollbackLimit(Model{Store: store}))
	require.Error(t, store.Set("attach_scrollback_lines", attachScrollbackMax+1))

	m := attachWithScrollback(t, 0)
	m = feedLines(t, m, 0, 100)
	require.Nil(t, m.Attach.sb)
	m = pressAttach(t, m, tea.KeyMsg{Type: tea.KeyCtrlPgUp})
	require.False(t, m.Attach.Scrolling())
	require.NotContains(t, ansi.Strip(m.View()), "^PgUp")
}

func TestAttachScroll_ShortLinesPadded(t *testing.T) {
	m := attachWithScrollback(t, 5000)
	m = feedAttach(t, m, "\x1b[31mred\x1b[0m\r\n")
	m = feedLines(t, m, 0, 60)
	m = pressAttach(t, m, tea.KeyMsg{Type: tea.KeyCtrlPgUp})
	m = pressAttach(t, m, key("g"))
	require.Equal(t, "red", viewTopRow(t, m))
	ox, oy := attachInnerOrigin(m)
	row := strings.Split(ansi.Strip(m.View()), "\n")[oy]
	require.Equal(t, m.Attach.Cols, ansi.StringWidth(ansi.Cut(row, ox, ox+m.Attach.Cols)))
}

func TestAttachScroll_EndedSessionStillScrolls(t *testing.T) {
	m := attachWithScrollback(t, 5000)
	m = feedLines(t, m, 0, 100)
	m.Attach.Ended = true
	ox, oy := attachInnerOrigin(m)
	m, _ = updateMouse(m, tea.MouseMsg{X: ox, Y: oy, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	require.Equal(t, attachScrollWheelLines, m.Attach.ScrollOff)
	require.Zero(t, m.Attach.wheelNotches)
	m, _ = updateMouse(m, tea.MouseMsg{X: ox, Y: oy, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = pressAttach(t, m, key("G"))
	require.False(t, m.Attach.Scrolling())
}
