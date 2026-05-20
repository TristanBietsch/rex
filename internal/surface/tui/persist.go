package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type persistedState struct {
	Selection    string    `json:"selection"`
	Filter       string    `json:"filter"`
	ScrollOffset int       `json:"scroll_offset"`
	SavedAt      time.Time `json:"saved_at"`
}

func statePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "rex", "tui-state.json")
}

// SaveTUIState writes selection + filter to ~/.local/state/rex/tui-state.json.
func SaveTUIState(m Model) error {
	path := statePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(persistedState{
		Selection: m.SelectedID,
		Filter:    m.Filter,
		SavedAt:   time.Now().UTC(),
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// LoadTUIState reads + deletes the persisted state. Returns ok=false when the file doesn't exist.
func LoadTUIState() (selection string, filter string, ok bool) {
	path := statePath()
	b, err := os.ReadFile(path)
	if err != nil {
		return "", "", false
	}
	var st persistedState
	if err := json.Unmarshal(b, &st); err != nil {
		return "", "", false
	}
	_ = os.Remove(path)
	return st.Selection, st.Filter, true
}
