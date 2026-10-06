package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/catalog/settings"
)

func TestApplySettingsAction_CyclesPromptGlyphPresets(t *testing.T) {
	tmp := t.TempDir()
	m := Model{
		Store:     settings.NewStore(),
		StorePath: filepath.Join(tmp, "config.yaml"),
		Settings:  &SettingsState{CursorID: "prompt_glyph"},
	}

	if got := m.Store.Get("prompt_glyph"); got != "λ" {
		t.Fatalf("default prompt_glyph = %v, want λ", got)
	}

	preset, _ := settings.Find("prompt_glyph")
	if len(preset.Options) < 2 {
		t.Fatalf("prompt_glyph has no preset list to cycle")
	}

	m = applySettingsAction(m, +1)
	if got, want := m.Store.Get("prompt_glyph"), preset.Options[1]; got != want {
		t.Fatalf("after +1 step: got %v, want %v", got, want)
	}

	m = applySettingsAction(m, -1)
	if got, want := m.Store.Get("prompt_glyph"), preset.Options[0]; got != want {
		t.Fatalf("after -1 step back: got %v, want %v", got, want)
	}

	m = applySettingsAction(m, -1)
	if got, want := m.Store.Get("prompt_glyph"), preset.Options[len(preset.Options)-1]; got != want {
		t.Fatalf("after wraparound -1: got %v, want %v", got, want)
	}
}

func TestApplySettingsAction_PromptGlyphCycleFromCustomString(t *testing.T) {
	tmp := t.TempDir()
	m := Model{
		Store:     settings.NewStore(),
		StorePath: filepath.Join(tmp, "config.yaml"),
		Settings:  &SettingsState{CursorID: "prompt_glyph"},
	}

	if err := m.Store.Set("prompt_glyph", "★"); err != nil {
		t.Fatalf("set custom glyph: %v", err)
	}

	preset, _ := settings.Find("prompt_glyph")
	m = applySettingsAction(m, +1)
	if got, want := m.Store.Get("prompt_glyph"), preset.Options[0]; got != want {
		t.Fatalf("cycle from non-preset value should land on first preset: got %v, want %v", got, want)
	}
}

func spawnPickerModel(t *testing.T) Model {
	t.Helper()
	reg, err := registry.Load("")
	require.NoError(t, err)
	m := Model{
		Store:     settings.NewStore(),
		StorePath: filepath.Join(t.TempDir(), "config.yaml"),
		Settings:  &SettingsState{Tools: visibleTools(reg.Tools)},
	}
	return m
}

// Changing the quick-spawn tool resets model and effort to ones that tool
// offers, so `i` never spawns an invalid combination.
func TestSpawnPicker_ToolChangeResetsModelAndEffort(t *testing.T) {
	m := spawnPickerModel(t)
	m.Settings.CursorID = "default_spawn_tool"
	require.Equal(t, "claude", m.Store.Get("default_spawn_tool"))

	m = applySettingsAction(m, +1)
	toolID := m.Store.Get("default_spawn_tool").(string)
	require.NotEqual(t, "claude", toolID)

	tool, model, ok := currentSpawnChoice(m)
	require.True(t, ok)
	require.Equal(t, toolID, tool.ID)
	require.Equal(t, tool.Models[0].ID, m.Store.Get("default_spawn_model"))
	if model.Effort == nil {
		require.Equal(t, "", m.Store.Get("default_spawn_effort"))
	} else {
		require.Contains(t, model.Effort.Options, m.Store.Get("default_spawn_effort"))
	}
}

func TestSpawnPicker_ModelCyclesWithinToolAndFixesEffort(t *testing.T) {
	m := spawnPickerModel(t)
	m.Settings.CursorID = "default_spawn_model"
	seen := map[string]bool{}
	for i := 0; i < 6; i++ {
		m = applySettingsAction(m, +1)
		id := m.Store.Get("default_spawn_model").(string)
		seen[id] = true
		_, model, _ := currentSpawnChoice(m)
		require.Equal(t, id, model.ID, "model must belong to the claude tool")
		if model.Effort != nil {
			require.Contains(t, model.Effort.Options, m.Store.Get("default_spawn_effort"),
				"effort must be valid for %s", id)
		}
	}
	require.True(t, seen["haiku"], "cycled models: %v", seen)
}

func TestSpawnPicker_EffortCyclesModelOptions(t *testing.T) {
	m := spawnPickerModel(t)
	m.Settings.CursorID = "default_spawn_effort"
	_, model, _ := currentSpawnChoice(m)
	m = applySettingsAction(m, +1)
	require.Contains(t, model.Effort.Options, m.Store.Get("default_spawn_effort"))
}

func TestWindowLines_KeepsFocusVisible(t *testing.T) {
	lines := make([]string, 40)
	for i := range lines {
		lines[i] = fmt.Sprintf("row %d", i)
	}
	got := windowLines(lines, 35, 10)
	require.Len(t, got, 10)
	require.Contains(t, strings.Join(got, "\n"), "row 35")
	require.Contains(t, got[0], "more")
	require.Equal(t, lines, windowLines(lines, 3, 0), "no height: unchanged")
}
