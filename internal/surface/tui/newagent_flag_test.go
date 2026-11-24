package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// new_agent_page picks what n and :new open: the stepped page while on (the
// default), the popup wizard while off.
func TestNewAgent_FlagRoutesN(t *testing.T) {
	m := naModel(t)
	on, _ := naKey(m, key("n"))
	require.Equal(t, FocusNewAgent, on.Focus)
	require.NotNil(t, on.NewAgent)
	require.Nil(t, on.Wizard)

	cmdOn, _ := executeCommand(m, "new")
	require.Equal(t, FocusNewAgent, cmdOn.Focus)

	m = setFlag(t, m, "new_agent_page", false)
	off, _ := naKey(m, key("n"))
	require.Equal(t, FocusWizard, off.Focus)
	require.NotNil(t, off.Wizard)
	require.Nil(t, off.NewAgent)
	cmdOff, _ := executeCommand(m, "new")
	require.Equal(t, FocusWizard, cmdOff.Focus)
	fresh, _ := executeCommand(m, "new!")
	require.Equal(t, FocusWizard, fresh.Focus)
	require.Empty(t, fresh.Wizard.TaskText)

	// esc closes the wizard back to the board.
	off = sendKey(off, key("esc"))
	require.Equal(t, FocusBoard, off.Focus)
	require.Nil(t, off.Wizard)
}

// Turning new_agent_page off while the page is open closes it, keeping the
// draft; the wizard then gets the draft's task text only and says so.
func TestNewAgent_FlagOffKeepsDraft(t *testing.T) {
	m := naOpen(t, naModel(t))
	m.Width, m.Height = 80, 24
	naGo(m, naStepTask)
	m = naType(m, "keep this")
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlJ})
	m = naType(m, "and this")
	m = naPaste(m, nLines(12))
	require.Len(t, m.NewAgent.Items, 1, "a long paste becomes a context item")
	m = setFlag(t, m, "new_agent_page", false)
	require.NotContains(t, ansi.Strip(m.View()), "NEW AGENT", "never drawn while off")

	// Any message closes it, not only a key.
	m = naUpdate(m, SpinnerTickMsg{Gen: -1})
	require.Equal(t, FocusBoard, m.Focus)
	require.Nil(t, m.NewAgent)
	require.NotNil(t, m.newAgentDraft)
	require.Len(t, m.newAgentDraft.Items, 1)

	// The popup wizard gets the task text only, and says so.
	m, _ = naKey(m, key("n"))
	require.Equal(t, FocusWizard, m.Focus)
	require.Equal(t, "keep this and this[paste 1 · 12 lines]", m.Wizard.TaskText)
	require.Contains(t, m.Notice, "1 context item was dropped")
	require.NotNil(t, m.newAgentDraft, "the draft stays for when the page is back")
}

// A key that reaches the page after the flag went off closes it too.
func TestNewAgent_KeyAfterFlagOffCloses(t *testing.T) {
	m := naOpen(t, naModel(t))
	m = setFlag(t, m, "new_agent_page", false)
	m, _ = updateNewAgentKey(m, key("j"))
	require.Equal(t, FocusBoard, m.Focus)
	require.Nil(t, m.NewAgent)
	require.NotNil(t, m.newAgentDraft)
}

// With the flag off the workspace is never walked: the wizard has no
// context step.
func TestImport_NoWalkWithTheWizard(t *testing.T) {
	m := naModel(t)
	naFiles(t, map[string]string{"a.go": "package a\n"})
	walks, _ := countWalks(t)

	off := setFlag(t, m, "new_agent_page", false)
	off, cmd := naKey(off, key("n"))
	require.Equal(t, FocusWizard, off.Focus)
	if cmd != nil {
		_ = cmd()
	}
	_, _ = naKey(off, tea.KeyMsg{Type: tea.KeyCtrlO})
	require.Zero(t, walks.Load(), "flag off: no workspace walk")
}
