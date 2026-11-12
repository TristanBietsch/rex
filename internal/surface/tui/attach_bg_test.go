package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// The board behind the attach popup is reused across agent output and
// redrawn after anything else; either way the frame is the one a fresh
// render gives.
func TestAttachBackgroundCache(t *testing.T) {
	m := benchAttachModel(t, benchAttachCols, benchAttachRows)
	_, _ = m.Attach.Term.Write(claudeLikeFrame(0, benchAttachCols, benchAttachRows))
	first := m.View()
	require.True(t, m.Attach.bg.ok)

	dropAttachBackground(m, AttachOutputMsg{SessionID: m.Attach.SessionID})
	require.True(t, m.Attach.bg.ok, "agent output keeps the board")

	_, _ = m.Attach.Term.Write(claudeLikeFrame(1, benchAttachCols, benchAttachRows))
	cached := m.View()
	m.Attach.bg = attachBgCache{}
	require.Equal(t, m.View(), cached, "cached frame equals a fresh one")
	require.NotEqual(t, first, cached, "the popup still shows new output")

	for _, msg := range []tea.Msg{SpinnerTickMsg{}, key("j"), tea.WindowSizeMsg{Width: 80, Height: 24}, DaemonEventMsg{}} {
		dropAttachBackground(m, msg)
		require.False(t, m.Attach.bg.ok, "%T drops the board", msg)
		_ = m.View()
	}
}

// A resize renders a new background even if no message dropped the old one.
func TestAttachBackgroundCacheKeyedBySize(t *testing.T) {
	m := benchAttachModel(t, benchAttachCols, benchAttachRows)
	_ = m.View()
	w, h := m.Attach.bg.w, m.Attach.bg.h
	m.Width, m.Height = w-10, h-4
	_ = m.View()
	require.Equal(t, w-10, m.Attach.bg.w)
	require.Equal(t, h-4, m.Attach.bg.h)
}

func TestFitsWidth(t *testing.T) {
	for _, s := range []string{"", "abc", "日本語", "\x1b[31mred\x1b[0m", "⏺ é", "👍🏽", "☺️"} {
		for w := 0; w <= 12; w++ {
			require.Equal(t, ansi.StringWidth(s) <= w, fitsWidth(s, w), "%q at %d", s, w)
		}
	}
}
