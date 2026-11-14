package tui

import (
	"encoding/json"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// storeChoice is the quick-spawn choice a store's defaults give, without a
// registry check.
func storeChoice(store *settings.Store) quickChoice {
	tool, _ := store.Get("default_spawn_tool").(string)
	model, _ := store.Get("default_spawn_model").(string)
	effort, _ := store.Get("default_spawn_effort").(string)
	return quickChoice{Tool: tool, Model: model, Effort: effort}
}

func quickModel(t *testing.T, tool string, installed ...string) Model {
	t.Helper()
	m := pickerModel(t, installed...)
	require.NoError(t, m.Store.Set("default_spawn_tool", tool))
	return m
}

// A saved default tool that is hidden or not installed falls back to the
// first offered tool, with a notice, for the quick-spawn and the wizard.
func TestQuickSpawn_FallsBackWhenTheDefaultToolIsNotOffered(t *testing.T) {
	t.Run("not installed", func(t *testing.T) {
		m := quickModel(t, "claude", "qwen")
		c, notice := quickChoiceOf(m)
		require.Equal(t, "qwen", c.Tool)
		require.Equal(t, "claude isn't available; using qwen", notice)
		require.NotEmpty(t, c.Model, "that tool's own first model, not claude's")
		require.NotEqual(t, "opus", c.Model)
	})
	t.Run("hidden by extra_agents", func(t *testing.T) {
		m := quickModel(t, "grok", "claude", "grok")
		require.NoError(t, m.Store.Set("extra_agents", false))
		c, notice := quickChoiceOf(m)
		require.Equal(t, "claude", c.Tool)
		require.Equal(t, "grok isn't available; using claude", notice)
	})
	t.Run("offered keeps the saved choice", func(t *testing.T) {
		m := quickModel(t, "claude", "claude", "qwen")
		require.NoError(t, m.Store.Set("default_spawn_model", "opus"))
		c, notice := quickChoiceOf(m)
		require.Equal(t, quickChoice{Tool: "claude", Model: "opus", Effort: m.Store.String("default_spawn_effort")}, c)
		require.Empty(t, notice)
	})
	t.Run("the i key spawns the fallback and says so", func(t *testing.T) {
		m := quickModel(t, "claude", "qwen")
		fake := &recordingClient{}
		m.spawner = fake
		m.PromptText = "fix it"
		m.Focus = FocusPrompt
		next, cmd := updatePromptKey(m, tea.KeyMsg{Type: tea.KeyEnter})
		require.Equal(t, "claude isn't available; using qwen", next.Notice)
		require.NotNil(t, cmd)
		require.Nil(t, cmd())
		require.Equal(t, "qwen", fake.last.ToolID)
		require.NotEqual(t, "claude", fake.last.ToolID)
	})
}

func TestWizard_NoticeWhenItsDefaultToolIsHidden(t *testing.T) {
	m := quickModel(t, "claude", "qwen")
	m, _ = executeCommand(m, "new")
	require.Equal(t, "claude isn't available; using qwen", m.Notice)
	require.Equal(t, "qwen", m.Wizard.selectedTool().ID)

	m = quickModel(t, "qwen", "qwen")
	m, _ = executeCommand(m, "new")
	require.Empty(t, m.Notice)
}

// ToolsChanged on the effort step: the model the cursor was on may have no
// effort block any more. The wizard steps back instead of panicking.
func TestWizard_ToolsChangedOnTheEffortStep(t *testing.T) {
	m := pickerModel(t, "claude", "qwen")
	m, _ = executeCommand(m, "new")
	require.Equal(t, "claude", m.Wizard.selectedTool().ID)
	// claude's models have effort; walk to its effort step.
	for m.Wizard.Step != wizEffort {
		before := m.Wizard.Step
		m = sendKey(m, key("enter"))
		require.NotEqual(t, before, m.Wizard.Step)
	}

	// claude's models are replaced by one with no effort block.
	infos := daemonTools(t, "claude", "qwen")
	for i := range infos {
		if infos[i].ID == "claude" {
			infos[i].Models = []protocol.ModelInfo{{ID: "plain", Name: "plain", Detected: true}}
		}
	}
	data, err := json.Marshal(protocol.ToolsChanged{Tools: infos})
	require.NoError(t, err)
	require.NotPanics(t, func() {
		m = m.applyEvent(protocol.Envelope{Type: protocol.EventToolsChanged, Data: data})
		_ = m.View()
		m = sendKey(m, key("j"))
		m = sendKey(m, key("k"))
	})
	require.Less(t, m.Wizard.Step, wizEffort, "stepped back to a step that applies")
	require.True(t, m.Wizard.stepApplies(m.Wizard.Step))
	require.Equal(t, wizProvider, m.Wizard.Step, "one model and no effort: only the provider step is left")
}

func TestWizard_ClosesWhenNoAgentIsLeft(t *testing.T) {
	m := pickerModel(t, "claude")
	m, _ = executeCommand(m, "new")
	data, err := json.Marshal(protocol.ToolsChanged{Tools: daemonTools(t)})
	require.NoError(t, err)
	m = m.applyEvent(protocol.Envelope{Type: protocol.EventToolsChanged, Data: data})
	require.Equal(t, FocusBoard, m.Focus)
	require.Nil(t, m.Wizard)
	require.Contains(t, m.Notice, "no agent is available")
}

// Turning extra_agents off in Settings takes the three out of the open
// pickers at once.
func TestSettings_ExtraAgentsSwitchRefreshesPickers(t *testing.T) {
	m := pickerModel(t, "claude", "qwen", "grok")
	m, _ = openSettings(m)
	require.Contains(t, toolIDs(m.Settings.Tools), "qwen")
	m.Settings.CursorID = "extra_agents"
	m = sendKey(m, key("enter")) // toggles on -> off
	require.False(t, m.featureOn("extra_agents"))
	require.Equal(t, []string{"claude"}, toolIDs(m.Settings.Tools))
	for _, r := range m.Settings.Onboarding {
		require.NotContains(t, []string{"qwen", "cursor", "grok"}, r.Tool.ID)
	}
}
