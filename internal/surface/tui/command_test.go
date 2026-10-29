package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// isolateHome points the home directory at a temp dir so code that reads
// ~/.config/rex (the wizard's tools.yaml) never sees the real one.
func isolateHome(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}

// ":new" used to set FocusWizard without building the wizard: every key
// (esc included) was ignored and the user was stuck.
func TestCommandNew_OpensWizard(t *testing.T) {
	isolateHome(t)
	m, _ := executeCommand(boardModel(), "new")
	require.Equal(t, FocusWizard, m.Focus)
	require.NotNil(t, m.Wizard)

	m, _ = updateKey(m, key("esc"))
	require.Equal(t, FocusBoard, m.Focus)
	require.Nil(t, m.Wizard)
}

func TestAttachCommandNew_OpensWizard(t *testing.T) {
	isolateHome(t)
	m := attachedModel()
	m.Focus = FocusCommand
	m.CmdText = "new"
	m, _ = updateKey(m, key("enter"))
	require.Nil(t, m.Attach)
	require.Equal(t, FocusWizard, m.Focus)
	require.NotNil(t, m.Wizard)
}

func TestWizardKey_NilWizardReturnsToBoard(t *testing.T) {
	m := boardModel()
	m.Focus = FocusWizard
	m, _ = updateKey(m, key("x"))
	require.Equal(t, FocusBoard, m.Focus)
}

// ctrl+c backs out of any overlay or input bar, even a broken one, and only
// quits from the board itself.
func TestCtrlC_EscapesEveryFocus(t *testing.T) {
	ctrlC := tea.KeyMsg{Type: tea.KeyCtrlC}
	for _, f := range []Focus{
		FocusPrompt, FocusCommand, FocusWizard, FocusHelp, FocusConfirmDelete,
		FocusSettings, FocusFail, FocusStats, FocusRename,
	} {
		m := boardModel(protocol.SessionSummary{ID: "a", Slug: "a", State: protocol.StateNeedsInput})
		m.Focus = f // overlay state deliberately nil
		m.PromptText, m.CmdText, m.PendingDeleteID = "x", "y", "a"
		m, cmd := updateKey(m, ctrlC)
		require.Equal(t, FocusBoard, m.Focus, "focus %d", f)
		require.False(t, m.Quitting, "focus %d", f)
		require.Nil(t, cmd, "focus %d", f)
		switch f {
		case FocusPrompt:
			require.Empty(t, m.PromptText)
		case FocusCommand:
			require.Empty(t, m.CmdText)
		case FocusConfirmDelete:
			require.Empty(t, m.PendingDeleteID)
		}
	}

	m, cmd := updateKey(boardModel(), ctrlC)
	require.True(t, m.Quitting)
	require.NotNil(t, cmd)

	m = boardModel()
	m.Focus = FocusConfirmQuit
	m, _ = updateKey(m, ctrlC)
	require.True(t, m.Quitting)
}

func TestCtrlC_AttachCommandBarReturnsToPopup(t *testing.T) {
	m := attachedModel()
	m.Focus = FocusCommand
	m.CmdText = "na"
	m, _ = updateKey(m, tea.KeyMsg{Type: tea.KeyCtrlC})
	require.Equal(t, FocusAttach, m.Focus)
	require.NotNil(t, m.Attach)
	require.Empty(t, m.CmdText)
}
