package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func attachedModel() Model {
	m := boardModel(protocol.SessionSummary{ID: "a", Slug: "a", State: protocol.StateNeedsInput})
	m.Attach = &AttachState{SessionID: "a"}
	m.Focus = FocusAttach
	return m
}

func TestAttach_DoubleColonOpensCommandBar(t *testing.T) {
	m := attachedModel()
	m, cmd := updateKey(m, key(":"))
	require.NotNil(t, cmd, "first ':' is held with a flush timer")
	require.Equal(t, ':', m.Attach.held)
	require.Equal(t, FocusAttach, m.Focus)

	m, _ = updateKey(m, key(":"))
	require.Equal(t, FocusCommand, m.Focus)
	require.Zero(t, m.Attach.held)

	m, _ = updateKey(m, key("esc"))
	require.Equal(t, FocusAttach, m.Focus)
	require.NotNil(t, m.Attach)
}

func TestAttach_CommandBarQuitDetaches(t *testing.T) {
	m := attachedModel()
	m, _ = updateKey(m, key(":"))
	m, _ = updateKey(m, key(":"))
	m = typeText(m, "q")
	m, _ = updateKey(m, key("enter"))
	require.Nil(t, m.Attach)
	require.Equal(t, FocusBoard, m.Focus)
}

func TestAttach_CommandBarDoneMarksAttachedSession(t *testing.T) {
	fr := &fakeRetirer{}
	m := attachedModel()
	m.retirer = fr
	m, _ = updateKey(m, key(":"))
	m, _ = updateKey(m, key(":"))
	m = typeText(m, "done")
	m, cmd := updateKey(m, key("enter"))
	runCmd(cmd)
	require.Equal(t, []string{"a"}, fr.retired)
	require.Equal(t, FocusAttach, m.Focus, "stays attached")
}

func TestAttach_CommandBarUnknownVerbFallsBackToBoard(t *testing.T) {
	m := attachedModel()
	m, _ = updateKey(m, key(":"))
	m, _ = updateKey(m, key(":"))
	m = typeText(m, "stats")
	m, _ = updateKey(m, key("enter"))
	require.Nil(t, m.Attach)
	require.Equal(t, FocusStats, m.Focus)
}

func TestAttach_ColonFlushIgnoresStaleOrEnded(t *testing.T) {
	m := attachedModel()
	m, _ = updateKey(m, key(":"))
	seq := m.Attach.holdSeq

	m, _ = flushAttachHold(m, attachHoldTimeoutMsg{SessionID: "a", Seq: seq - 1})
	require.Equal(t, ':', m.Attach.held, "stale timer must not flush")

	m.Attach.Ended = true // nothing can be sent to an ended session
	m, _ = flushAttachHold(m, attachHoldTimeoutMsg{SessionID: "a", Seq: seq})
	require.Zero(t, m.Attach.held)
}

func TestAttach_PastedColonsPassThrough(t *testing.T) {
	require.False(t, isLoneColon(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("::"), Paste: true}))
	require.False(t, isLoneColon(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":"), Paste: true}))
	require.True(t, isLoneColon(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")}))
}

func TestAttach_DoubleQDetaches(t *testing.T) {
	m := attachedModel()
	m, cmd := updateKey(m, key("q"))
	require.NotNil(t, cmd, "first 'q' is held with a flush timer")
	require.Equal(t, 'q', m.Attach.held)
	require.Equal(t, FocusAttach, m.Focus)

	m, _ = updateKey(m, key("q"))
	require.Nil(t, m.Attach)
	require.Equal(t, FocusBoard, m.Focus)
}

func TestAttach_PastedQQPassesThrough(t *testing.T) {
	_, ok := loneRune(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("qq"), Paste: true})
	require.False(t, ok)
	_, ok = loneRune(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q"), Paste: true})
	require.False(t, ok)
}

// D8: a page is never drawn over the attach PTY. `::settings` and `::help`
// detach first, then open the full-screen page.
func TestAttachSettingsDetachesThenOpens(t *testing.T) {
	for verb, want := range map[string]struct {
		focus Focus
		title string
	}{
		"settings": {FocusSettings, "∴ REX › SETTINGS"},
		"help":     {FocusHelp, "∴ REX › HELP"},
	} {
		t.Run(verb, func(t *testing.T) {
			m := bannerModel(t, 120, 40, bannerSession())
			require.True(t, pagesOn(m))
			m, _ = executeAttachCommand(m, verb)
			require.Nil(t, m.Attach)
			require.Equal(t, want.focus, m.Focus)
			view := ansi.Strip(m.View())
			require.Contains(t, view, want.title)
			require.NotContains(t, view, "fix billing tests", "the attach popup is gone")
		})
	}
}

// With the pages off, the same verbs still detach, then open the overlay.
func TestAttachSettingsOffDetaches(t *testing.T) {
	for verb, focus := range map[string]Focus{"settings": FocusSettings, "help": FocusHelp} {
		t.Run(verb, func(t *testing.T) {
			m := overlayMode(t, bannerModel(t, 120, 40, bannerSession()))
			m, _ = executeAttachCommand(m, verb)
			require.Nil(t, m.Attach)
			require.Equal(t, focus, m.Focus)
			require.NotContains(t, ansi.Strip(m.View()), "∴ REX ›")
		})
	}
}
