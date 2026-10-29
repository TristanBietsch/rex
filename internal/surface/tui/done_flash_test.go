package tui

import (
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// The flash deadline is set and pruned by the model clock, the same clock
// renderRow reads, so an injected clock drives the whole flash.
func TestDoneFlash_UsesModelClock(t *testing.T) {
	at := time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC)
	m := Model{Filter: "all", Clock: func() time.Time { return at },
		Sessions: []protocol.SessionSummary{{ID: "a", State: protocol.StateWorking}}}
	m = m.applyEvent(eventEnv(t, protocol.EventSessionUpdated, protocol.SessionUpdated{SessionID: "a",
		Patch: map[string]any{"state": string(protocol.StateDone)}}))
	require.Equal(t, at.Add(1600*time.Millisecond), m.BlinkUntil["a"])

	next, _ := m.Update(SpinnerTickMsg{})
	require.Contains(t, next.(Model).BlinkUntil, "a", "still running by the model clock")

	at = at.Add(2 * time.Second)
	next, _ = m.Update(SpinnerTickMsg{})
	require.Empty(t, next.(Model).BlinkUntil, "expired by the model clock")
}

func TestRenderRow_DoneFlashColorsName(t *testing.T) {
	withTrueColor(t)
	lit := time.UnixMilli(1_000_000_000_000) // UnixMilli/200 even: lit phase
	dark := lit.Add(200 * time.Millisecond)  // odd: dark phase
	green := lipgloss.NewStyle().Bold(true).Foreground(colorDone).Render("alpha")
	s := protocol.SessionSummary{ID: "a", ShortID: "aaaa", Slug: "alpha", State: protocol.StateDone, LastEventAt: lit}
	render := func(at time.Time, store *settings.Store) string {
		m := Model{Filter: "all", Sessions: []protocol.SessionSummary{s}, Store: store,
			BlinkUntil: map[string]time.Time{"a": lit.Add(time.Second)},
			Clock:      func() time.Time { return at }}
		return renderRow(m, s, layoutRows(m.Sessions, at, 80))
	}

	on, off := render(lit, nil), render(dark, nil)
	require.Contains(t, on, green, "lit phase colors the name")
	require.NotContains(t, off, green)
	require.Equal(t, ansi.Strip(on), ansi.Strip(off))
	require.NotContains(t, render(lit.Add(2*time.Second), nil), green, "flash over (render clock, not wall clock)")

	calm := settings.NewStore()
	require.NoError(t, calm.Set("reduce_motion", true))
	require.NotContains(t, render(lit, calm), green, "reduce_motion: no flash")
}
