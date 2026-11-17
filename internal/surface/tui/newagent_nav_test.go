package tui

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Key-sequence tests of the new-agent page's lists under the shared
// navigator (nav.go): j/k, gg/G, ctrl+d and the wheel on each step.

func TestNewAgentNav_ModelListMotions(t *testing.T) {
	m := naModel(t)
	m.DaemonTools = cursorModels(t)
	m = naPickTool(t, naAgentOpen(t, m), "cursor")
	m = navPress(m, "enter", "ctrl+d")
	require.Greater(t, m.NewAgent.modelIdx(), 2, "ctrl+d moves half a window of models")
	m = navPress(m, "G")
	require.Equal(t, 199, m.NewAgent.modelIdx())
	require.Contains(t, naView(m), "model-199")
	m = navPress(m, "j")
	require.Equal(t, 199, m.NewAgent.modelIdx(), "j clamps at the last model")
	require.Equal(t, naStepModel, m.NewAgent.Step)
	m = navPress(m, "g", "g")
	require.Zero(t, m.NewAgent.modelIdx(), "gg: the first model")
	m = navPress(m, "/", "s", "o", "n", "enter", "j")
	require.Equal(t, "sonnet-4.6-thinking", m.NewAgent.choice().Model, "j after enter moves through the matches")
}

func TestNewAgentNav_EffortKeysOnTheModelStep(t *testing.T) {
	m := naModelOpen(t, naModel(t))
	effort := m.NewAgent.choice().Effort
	m = navPress(m, "j", "k")
	require.Equal(t, effort, m.NewAgent.choice().Effort, "j/k never change the effort")
	m = pressN(m, "left", 6)
	require.Zero(t, m.NewAgent.EffortIdx, "← steps the effort down to the first")
	require.Equal(t, naStepModel, m.NewAgent.Step, "and stays on the step")
	m = navPress(m, "right")
	require.Equal(t, 1, m.NewAgent.EffortIdx, "→ steps it up")
}

func TestNewAgentNav_BackFromMissingKeepsModel(t *testing.T) {
	m := naModel(t)
	m.DaemonTools = daemonTools(t, "claude") // codex and the rest are missing
	m = naAgentOpen(t, m)
	require.NotEmpty(t, m.NewAgent.Missing)
	model := m.NewAgent.choice().Model
	m = navPress(m, "G")
	require.True(t, m.NewAgent.OnMissing)
	m = navPress(m, "k")
	require.False(t, m.NewAgent.OnMissing)
	require.Equal(t, model, m.NewAgent.choice().Model, "the model stays")
}

func TestNewAgentNav_ImportReturnsToTheStep(t *testing.T) {
	m := naOpen(t, naModel(t))
	m = navPress(m, "x", "ctrl+o")
	require.Equal(t, naImportScreen, m.NewAgent.Screen)
	m = navPress(m, "esc")
	require.Equal(t, naSteps, m.NewAgent.Screen)
	require.Equal(t, naStepTask, m.NewAgent.Step, "esc comes back to the step it came from")
	require.Equal(t, "x", m.NewAgent.Task.String())

	// From the context step the picker is the same, and esc stays there.
	m = naAt(m, naStepContext)
	m = navPress(m, "ctrl+o")
	require.Equal(t, naImportScreen, m.NewAgent.Screen)
	m = navPress(m, "esc")
	require.Equal(t, naStepContext, m.NewAgent.Step)
	m = naAt(m, naStepTrust)
	m = navPress(m, "ctrl+o")
	require.Equal(t, naSteps, m.NewAgent.Screen, "ctrl+o is for the task and context steps")
}

func TestNewAgentNav_GThenOtherKeyIsNotGg(t *testing.T) {
	m := naModel(t)
	m.DaemonTools = daemonTools(t, "claude", "codex", "ollama")
	m = naAgentOpen(t, m)
	m = navPress(m, "j", "j", "g", "h", "g")
	require.Equal(t, "ollama", m.NewAgent.choice().Tool, "g h g is no gg")
}
