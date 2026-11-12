package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// pasteKey is a bracketed paste: one KeyRunes event carrying newlines, tabs
// and escapes verbatim.
func pasteKey(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s), Paste: true}
}

const hostilePaste = "one\r\ntwo\tthree\x1b[31mred\x1b[0m\x07"

// The command bar is one line: a paste is flattened as it is typed.
func TestPaste_CommandBarFlattens(t *testing.T) {
	m := simpleBoardStore(t, true)
	m.Focus = FocusCommand
	m, _ = updateKey(m, pasteKey(hostilePaste))
	require.Equal(t, "one  two threered", m.CmdText)
}

// The attach command bar ("::") is one line too.
func TestPaste_AttachCommandBarFlattens(t *testing.T) {
	m := attachedModel()
	m.Focus = FocusCommand
	m, _ = updateKey(m, pasteKey(hostilePaste))
	require.Equal(t, "one  two threered", m.CmdText)
}

// The λ prompt becomes the agent's initial prompt: line breaks and tabs
// stay, escapes and other controls go, and the bar still draws one row so
// the bottom bars stay on screen.
func TestPaste_PromptKeepsLineBreaksDrawsOneRow(t *testing.T) {
	m := simpleBoardStore(t, true)
	m.Focus = FocusPrompt
	m, _ = updateKey(m, pasteKey(hostilePaste))
	require.Equal(t, "one\ntwo\tthreered", m.PromptText)

	full := renderFullScreen(m, 80, 24)
	lines := strings.Split(ansi.Strip(full), "\n")
	require.Len(t, lines, 24)
	prompt := lines[len(lines)-2]
	require.Contains(t, prompt, "one two three")
	require.Equal(t, ansi.Strip(renderHelpLine(m, 80)), lines[len(lines)-1], "help line pinned to the bottom")
}

// The wizard's task text keeps line breaks for the agent and draws flat.
func TestPaste_WizardTaskKeepsLineBreaksDrawsFlat(t *testing.T) {
	m := simpleBoardStore(t, true)
	m, _ = openWizard(m)
	m.Wizard.Step = wizDescribe
	m, _ = updateKey(m, pasteKey(hostilePaste))
	require.Equal(t, "one\ntwo\tthreered", m.Wizard.TaskText)
	box := strings.Join(wizInputBoxLines(m.Wizard.TaskText, m), "\n")
	require.Len(t, strings.Split(box, "\n"), 3, "border, one text row, border")
}
