package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/require"
)

// The lit phase of the done flash must change what is on screen. Every
// cell of a board row sets its own color, so wrapping the styled row in an
// outer green colored only the padding: the flash was invisible.
func TestDoneFlash_LitPhaseRecolorsTheText(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)
	rebuildStyles()
	defer rebuildStyles()
	defer func(f func() time.Time) { blinkNow = f }(blinkNow)

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	m := layoutModel(now, 120, layoutSessions(now)...)
	s := m.Sessions[0]
	cols := boardLayout(m, 120)
	m.BlinkUntil = map[string]time.Time{s.ID: time.Now().Add(time.Hour)}

	blinkNow = func() time.Time { return time.UnixMilli(200) } // dark phase
	dark := renderRow(m, s, cols)
	blinkNow = func() time.Time { return time.UnixMilli(0) } // lit phase
	lit := renderRow(m, s, cols)

	require.Equal(t, ansi.Strip(dark), ansi.Strip(lit), "same text")
	done := strings.TrimSuffix(styleDone.Render("x"), "x\x1b[0m")
	require.NotEmpty(t, done)
	// Every visible character of the lit row is drawn in the done color:
	// no other color sequence follows the done one.
	i := strings.Index(lit, done)
	require.GreaterOrEqual(t, i, 0, "done color used: %q", lit)
	require.Equal(t, strings.TrimSpace(ansi.Strip(lit[:i])), "", "nothing visible before the done color: %q", lit)
	rest := lit[i+len(done):]
	require.Equal(t, 1, strings.Count(rest, "\x1b["), "one reset, no other color: %q", rest)
}
