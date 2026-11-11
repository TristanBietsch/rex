package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/hinshun/vt10x"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/pty"
	"github.com/tristanbietsch/rex/internal/testutil/uvfixture"
)

// popupRows renders the attach popup and returns the terminal rows between
// the title rule and the footer rule, with colors stripped.
func popupRows(t *testing.T, m Model) []string {
	t.Helper()
	lines := strings.Split(renderAttach(m), "\n")
	cols, rows := m.Attach.Term.Size()
	require.Len(t, lines, rows+4, "title, rule, rows, rule, footer")
	out := make([]string, 0, rows)
	for i, l := range lines {
		require.LessOrEqual(t, ansi.StringWidth(l), cols, "line %d is wider than the popup: %q", i, l)
	}
	for _, l := range lines[2 : 2+rows] {
		out = append(out, ansi.Strip(l))
	}
	return out
}

// requireLogoCentered checks the popup shows the fixture screen exactly, and
// that the logo block has equal margins (within one cell) on both sides: the
// reported bug was a logo shifted left and cut off on the right.
func requireLogoCentered(t *testing.T, got []string, cols, rows int, msg string) {
	t.Helper()
	l := uvfixture.LayoutFor(cols, rows)
	first, last := cols, -1
	for i := range uvfixture.Logo() {
		for x, r := range []rune(got[l.Top+i]) {
			if r != ' ' {
				first, last = min(first, x), max(last, x)
			}
		}
	}
	require.GreaterOrEqual(t, last, 0, "%s: the logo is blank", msg)
	left, right := first, cols-1-last
	require.LessOrEqual(t, abs(left-right), 1, "%s: logo margins are %d left and %d right", msg, left, right)
	require.Equal(t, uvfixture.Expected(cols, rows), got, msg)
}

// uvAttachModel is a model with the attach popup open at cols x rows.
func uvAttachModel(t *testing.T, cols, rows int) Model {
	t.Helper()
	return benchAttachModel(t, cols, rows)
}

func feed(t *testing.T, m Model, b []byte) {
	t.Helper()
	_, err := m.Attach.Term.Write(b)
	require.NoError(t, err)
}

// An ultraviolet agent drawing straight into the popup's emulator.
func TestAttachView_UVLogoCentered(t *testing.T) {
	for _, size := range [][2]int{{120, 32}, {177, 44}, {215, 55}} {
		cols, rows := size[0], size[1]
		m := uvAttachModel(t, cols, rows)
		feed(t, m, uvfixture.Frame(cols, rows))
		requireLogoCentered(t, popupRows(t, m), cols, rows, "live")
	}
}

// The daemon mirror starts at 120x32 and is grown with the PTY. Its repaint
// is what a new popup shows until the agent redraws, so the popup must show
// the logo centered after the repaint and after a further resize and redraw.
func TestAttachView_UVLogoCenteredThroughRepaintAndResize(t *testing.T) {
	const cols, rows = 177, 44
	mirror := vt10x.New(vt10x.WithSize(120, 32))
	mirror.Resize(cols, rows)
	_, err := mirror.Write(uvfixture.Frame(cols, rows))
	require.NoError(t, err)
	mirror.Lock()
	repaint := pty.Repaint(mirror)
	mirror.Unlock()

	m := uvAttachModel(t, cols, rows)
	feed(t, m, repaint)
	requireLogoCentered(t, popupRows(t, m), cols, rows, "after repaint")

	// The user enlarges the terminal: popup and PTY grow, the agent redraws.
	big := uvAttachModel(t, 215, 55)
	feed(t, big, repaint)
	big.Attach.Term.Resize(215, 55)
	feed(t, big, uvfixture.Frame(215, 55))
	requireLogoCentered(t, popupRows(t, big), 215, 55, "after resize and redraw")

	// And back down.
	small := uvAttachModel(t, 150, 40)
	small.Attach.Term.Resize(120, 32)
	small.Attach.Term.Resize(150, 40)
	feed(t, small, uvfixture.Frame(150, 40))
	requireLogoCentered(t, popupRows(t, small), 150, 40, "after shrink and grow")
}
