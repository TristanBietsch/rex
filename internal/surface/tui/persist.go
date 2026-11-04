package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/runtime/rexlog"
)

type persistedState struct {
	Selection    string    `json:"selection"`
	Filter       string    `json:"filter"`
	ScrollOffset int       `json:"scroll_offset"`
	SavedAt      time.Time `json:"saved_at"`
}

// SaveTUIState writes selection + filter to paths.TUIStateFile
// (~/.local/state/rex/tui-state.json). The state dir is rex's own and is
// created or tightened to 0700; the file is 0600.
func SaveTUIState(m Model) error {
	path, err := paths.TUIStateFile()
	if err != nil {
		return err
	}
	if err := rexlog.PrivateDir(filepath.Dir(path)); err != nil {
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
	return os.WriteFile(path, b, 0o600)
}

// LoadTUIState reads + deletes the persisted state. Returns ok=false when the file doesn't exist.
func LoadTUIState() (selection string, filter string, ok bool) {
	path, err := paths.TUIStateFile()
	if err != nil {
		return "", "", false
	}
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
