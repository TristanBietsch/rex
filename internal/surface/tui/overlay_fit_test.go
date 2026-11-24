package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// overlayModels opens each full-screen overlay (all but attach, which has its
// own geometry) on a w x h terminal.
func overlayModels(t *testing.T, w, h int) map[string]Model {
	t.Helper()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	exit := 1
	sessions := append(layoutSessions(now), protocol.SessionSummary{ID: "f", ShortID: "ff01", ToolID: "claude",
		ModelID: "opus", Slug: "a-very-long-failing-session-name", State: protocol.StateFailed, ExitCode: &exit,
		LastLine: "error: something went badly wrong in a place with a long path", LastEventAt: now})
	base := layoutModel(now, w, sessions...)
	base.Height = h
	base.Detailed = false
	base = overlayMode(t, base) // help and settings as overlays, not pages

	with := func(f func(m *Model)) Model {
		m := base
		f(&m)
		return m
	}
	return map[string]Model{
		"help":     with(func(m *Model) { m.Focus = FocusHelp }),
		"settings": with(func(m *Model) { m.Focus, m.Settings = FocusSettings, &SettingsState{} }),
		"wizard":   with(func(m *Model) { m.Focus, m.Wizard = FocusWizard, &WizardState{} }),
		"stats":    with(func(m *Model) { m.Focus = FocusStats }),
		"overview": with(func(m *Model) { m.Focus = FocusOverview }),
		"failures": with(func(m *Model) { m.Focus, m.Fail = FocusFail, &FailState{Sessions: sessions[len(sessions)-1:]} }),
	}
}

// No overlay is wider than the screen, whatever the terminal: Bubble Tea
// clips the right side of a wider line.
func TestOverlaysNeverWiderThanScreen(t *testing.T) {
	for w := 30; w <= 60; w++ {
		for name, m := range overlayModels(t, w, 30) {
			view := m.View()
			if w >= 40 { // below 40 settings drops its border
				require.True(t, strings.ContainsAny(view, "╭┌"), "%s at width %d draws a box", name, w)
			}
			for i, l := range strings.Split(view, "\n") {
				require.LessOrEqual(t, ansi.StringWidth(l), w, "%s at width %d, line %d: %q", name, w, i, ansi.Strip(l))
			}
		}
	}
}

func TestPanelWidthNeverPastTheScreen(t *testing.T) {
	for termW := 10; termW <= 140; termW++ {
		require.LessOrEqual(t, panelWidth(termW, 96, 56), termW-2, "width %d", termW)
	}
	require.Equal(t, 96, panelWidth(140, 96, 56))
	require.Equal(t, 56, panelWidth(60, 96, 56), "the floor still applies while it fits")
}
