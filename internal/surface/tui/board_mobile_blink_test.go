package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/require"
)

// A done row's blink colours both stacked lines, and only while it is on.
func TestMobileRowKeepsDoneBlink(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)
	defer func(f func() time.Time) { blinkNow = f }(blinkNow)

	m := mobileModel(t, 40, 24, mobileSessions()...)
	m.BlinkUntil = map[string]time.Time{"2": time.Now().Add(time.Hour)}
	s := m.Sessions[1]

	at := func(ms int64) { blinkNow = func() time.Time { return time.UnixMilli(ms) } }
	at(0) // lit phase
	lit1, lit2 := renderRowStacked(m, s, 40)
	at(200) // dark phase
	dark1, dark2 := renderRowStacked(m, s, 40)
	require.NotEqual(t, lit1, dark1)
	require.NotEqual(t, lit2, dark2)
	flat := strings.TrimSpace
	require.Contains(t, flat(lit1), "\x1b[", "line 1 is coloured while lit")
	require.True(t, strings.HasPrefix(lit2, "\x1b["), "line 2 is coloured while lit")

	m.BlinkUntil = nil
	n1, _ := renderRowStacked(m, s, 40)
	require.Equal(t, dark1, n1, "no blink in progress: plain")
}
