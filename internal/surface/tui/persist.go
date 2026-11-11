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
	// HiddenSections is a pointer so "none hidden" (an empty list, which
	// overrides board_hidden_sections) differs from a file written before
	// this field existed (nil, which leaves the setting alone).
	HiddenSections *[]string `json:"hidden_sections,omitempty"`
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
	hidden := []string{}
	for _, g := range boardGroups {
		if m.Hidden[g.ID] {
			hidden = append(hidden, g.ID)
		}
	}
	b, err := json.MarshalIndent(persistedState{
		Selection:      m.SelectedID,
		Filter:         m.Filter,
		SavedAt:        time.Now().UTC(),
		HiddenSections: &hidden,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// LoadTUIState reads + deletes the persisted state. Returns ok=false when the file doesn't exist.
func LoadTUIState() (st persistedState, ok bool) {
	path, err := paths.TUIStateFile()
	if err != nil {
		return persistedState{}, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return persistedState{}, false
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return persistedState{}, false
	}
	_ = os.Remove(path)
	return st, true
}
