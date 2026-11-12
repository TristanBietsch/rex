package tui

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
)

func wizEffortOpts(def string, opts ...string) *registry.Effort {
	return &registry.Effort{Options: opts, Default: def}
}

// wizTools: a multi-model tool with per-model effort, a one-model tool, and
// a one-model tool with effort.
func wizTools() []registry.Tool {
	return []registry.Tool{
		{ID: "claude", Name: "Claude Code", Models: []registry.Model{
			{ID: "opus", Name: "Opus", Effort: wizEffortOpts("high", "low", "high")},
			{ID: "sonnet", Name: "Sonnet"},
			{ID: "haiku", Name: "Haiku", Effort: wizEffortOpts("low", "low", "mid", "max")},
		}},
		{ID: "solo", Name: "Solo", Models: []registry.Model{{ID: "only", Name: "Only"}}},
		{ID: "solofx", Name: "Solo effort", Models: []registry.Model{{ID: "m", Name: "M", Effort: wizEffortOpts("a", "a", "b")}}},
	}
}

func wizardOn(w *WizardState) Model {
	return Model{Focus: FocusWizard, Wizard: w, Width: 120, Height: 40}
}

func wizKey(m Model, keys ...string) Model {
	for _, k := range keys {
		m = sendKey(m, key(k))
	}
	return m
}

func TestWizard_ModelStepSkippedForSingleModel(t *testing.T) {
	m := wizardOn(&WizardState{Tools: wizTools(), ToolIdx: 1})
	m = wizKey(m, "enter")
	require.Equal(t, wizDescribe, m.Wizard.Step, "one model, no effort: straight to describe")

	m = wizardOn(&WizardState{Tools: wizTools(), ToolIdx: 2})
	m = wizKey(m, "enter")
	require.Equal(t, wizEffort, m.Wizard.Step, "one model with effort levels: effort next")
	m = wizKey(m, "b")
	require.Equal(t, wizProvider, m.Wizard.Step, "back skips the model step too")
}

func TestWizard_ModelStepShownForSeveralModels(t *testing.T) {
	m := wizardOn(&WizardState{Tools: wizTools()})
	m = wizKey(m, "enter")
	require.Equal(t, wizModel, m.Wizard.Step)
	require.Equal(t, "opus", m.Wizard.selectedModel().ID)

	m = wizKey(m, "j")
	require.Equal(t, "sonnet", m.Wizard.selectedModel().ID)
	m = wizKey(m, "j", "j") // clamps at the last model
	require.Equal(t, "haiku", m.Wizard.selectedModel().ID)
	m = wizKey(m, "k")
	require.Equal(t, "sonnet", m.Wizard.selectedModel().ID)

	// sonnet has no effort levels: describe follows, and back returns to the model step.
	m = wizKey(m, "enter")
	require.Equal(t, wizDescribe, m.Wizard.Step)
	m = wizKey(m, "b")
	require.Equal(t, wizModel, m.Wizard.Step)
	require.Equal(t, "sonnet", m.Wizard.selectedModel().ID, "the choice is kept")

	m = wizKey(m, "j", "enter")
	require.Equal(t, wizEffort, m.Wizard.Step, "haiku has effort levels")
	require.Equal(t, "low", m.Wizard.currentEffort(), "effort snaps to the chosen model's default")
	m = wizKey(m, "esc")
	require.Equal(t, FocusBoard, m.Focus)
	require.Nil(t, m.Wizard)
}

func TestWizard_ModelDefaultsToSpawnDefault(t *testing.T) {
	w := &WizardState{Tools: wizTools(), SpawnTool: "claude", SpawnModel: "haiku"}
	presetModelDefault(w)
	require.Equal(t, "haiku", w.selectedModel().ID)

	// Another tool ignores the quick-spawn model; so does an unknown model.
	w.ToolIdx = 2
	presetModelDefault(w)
	require.Equal(t, "m", w.selectedModel().ID)
	w.ToolIdx, w.SpawnModel = 0, "gone"
	presetModelDefault(w)
	require.Equal(t, "opus", w.selectedModel().ID)

	// Moving the provider cursor re-derives it, and keys reach the model step.
	m := wizardOn(&WizardState{Tools: wizTools(), SpawnTool: "claude", SpawnModel: "sonnet"})
	m = wizKey(m, "j", "k", "enter")
	require.Equal(t, wizModel, m.Wizard.Step)
	require.Equal(t, "sonnet", m.Wizard.selectedModel().ID)
}

func TestWizard_OpenSeedsSpawnDefaults(t *testing.T) {
	m := commandModel(t)
	require.NoError(t, m.Store.Set("default_spawn_tool", "claude"))
	require.NoError(t, m.Store.Set("default_spawn_model", "sonnet"))
	m, _ = openWizard(m)
	require.NotNil(t, m.Wizard)
	require.Equal(t, "claude", m.Wizard.SpawnTool)
	require.Equal(t, "sonnet", m.Wizard.SpawnModel)
	if m.Wizard.selectedTool().ID == "claude" {
		require.Equal(t, "sonnet", m.Wizard.selectedModel().ID)
	}

	// No store yet: the wizard still opens, on the first model.
	n := Model{Focus: FocusBoard}
	n, _ = openWizard(n)
	require.NotNil(t, n.Wizard)
	require.Empty(t, n.Wizard.SpawnTool)
}

func TestWizard_RequestUsesSelectedModelAndEffort(t *testing.T) {
	w := &WizardState{Tools: wizTools(), ModelIdx: 2, TaskText: "  fix auth  "}
	w.EffortIdx = 1
	req := w.request("/work", []string{"cc.haiku.fix-auth"})
	require.Equal(t, "claude", req.ToolID)
	require.Equal(t, "haiku", req.ModelID)
	require.Equal(t, "mid", req.Effort)
	require.Equal(t, "cc.haiku.fix-auth-2", req.Slug)
	require.Equal(t, "fix auth", req.Title)
	require.Equal(t, "fix auth", req.InitialPrompt)
	require.Equal(t, "/work", req.CWD)
}

// b goes back from the describe step only while the text is empty; after
// that it is a letter (it used to be typed even on an empty field).
func TestWizard_DescribeBIsBackOnlyWhenEmpty(t *testing.T) {
	m := wizardOn(&WizardState{Step: wizDescribe, Tools: wizTools(), ToolIdx: 1})
	m = wizKey(m, "b")
	require.Equal(t, wizProvider, m.Wizard.Step)
	require.Empty(t, m.Wizard.TaskText)

	m = wizardOn(&WizardState{Step: wizDescribe, Tools: wizTools(), ToolIdx: 1})
	m = wizKey(m, "a", "b")
	require.Equal(t, wizDescribe, m.Wizard.Step)
	require.Equal(t, "ab", m.Wizard.TaskText)
}

func TestWizard_ModelStepRenders(t *testing.T) {
	m := wizardOn(&WizardState{Step: wizModel, Tools: wizTools(), ModelIdx: 1})
	out := renderWizard(m)
	for _, want := range []string{"which model?", "Opus", "Sonnet", "Haiku"} {
		require.Contains(t, out, want)
	}
	require.Contains(t, wizardFooter(wizModel), "b back")
}
