package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSplashInitialFrame(t *testing.T) {
	m := Model{
		Focus:     FocusBoot,
		Width:     80,
		Height:    24,
		BootStart: time.Now().Add(-470 * time.Millisecond), // 0.47s elapsed
	}
	out := renderSplash(m, m.Width, m.Height)
	lines := strings.Split(out, "\n")

	require.GreaterOrEqual(t, len(lines), 4)
	require.Contains(t, lines[1], "レックス")
	require.Contains(t, lines[1], "rex runtime executive")
	require.Contains(t, lines[2], "起動中")
	require.Contains(t, lines[2], "booting")
	// No log rows yet — the third row should be blank.
	require.Equal(t, "", strings.TrimSpace(lines[3]))
}
