package tui

import (
	"encoding/json"
	"errors"
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
	// Search is the board search text, restored once on the next start.
	Search string `json:"search,omitempty"`
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
		Search:         m.Search,
	}, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, b)
}

// writeFileAtomic writes b to a private temp file next to path and renames
// it over path, so a crash or a full disk mid-write leaves the old file (or
// none), never a truncated one. Two TUIs quitting at once each write their
// own temp file; the last rename wins.
func writeFileAtomic(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp") // mode 0600
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, werr := f.Write(b)
	cerr := f.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
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
