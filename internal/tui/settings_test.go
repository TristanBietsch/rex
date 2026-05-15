package tui

import (
	"path/filepath"
	"testing"

	"github.com/tristanbietsch/rex/internal/settings"
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
