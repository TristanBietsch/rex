package tui

import (
	"encoding/json"
	"time"

	"github.com/tristanbietsch/rex/internal/audio"
	"github.com/tristanbietsch/rex/internal/client"
	"github.com/tristanbietsch/rex/internal/protocol"
)

// Focus is what currently has keyboard focus.
type Focus int

const (
	FocusBoard Focus = iota
	FocusPrompt
	FocusCommand
	FocusWizard
	FocusHelp
	FocusConfirmQuit
	FocusConfirmDelete
	FocusSettings
)

// Model is the root Bubble Tea model.
type Model struct {
	Client       *client.Client
	Socket       string // path to daemon UDS, forwarded to child `rex attach`
	Focus        Focus
	Width        int
	Height       int
	Sessions     []protocol.SessionSummary
	SelectedID   string
	Filter       string
	PromptText   string
	CmdText      string
	PendingChord string
	Err          string
	SpinnerTick  int
	Quitting     bool

	Wizard   *WizardState
	Settings *SettingsState
	Audio    *audio.Player

	// PendingDeleteID is the session targeted by an in-progress delete confirmation.
	PendingDeleteID string

	// BlinkUntil tracks done-blink expiry per session.
	BlinkUntil map[string]time.Time

	// ScrollOffset is how many board lines to skip from the top (for scroll).
	ScrollOffset int
}

func (m Model) applyEvent(env protocol.Envelope) Model {
	switch env.Type {
	case protocol.EventSessionAdded:
		var sum protocol.SessionSummary
		if err := json.Unmarshal(env.Data, &sum); err == nil {
			m.Sessions = append(m.Sessions, sum)
			if m.Audio != nil {
				m.Audio.Play(audio.EventCreate)
			}
		}
	case protocol.EventSessionRemoved:
		var rem protocol.SessionRemoved
		if err := json.Unmarshal(env.Data, &rem); err == nil {
			out := m.Sessions[:0]
			for _, s := range m.Sessions {
				if s.ID != rem.SessionID {
					out = append(out, s)
				}
			}
			m.Sessions = out
			if m.Audio != nil {
				m.Audio.Play(audio.EventDelete)
			}
		}
	case protocol.EventSessionUpdated:
		var upd protocol.SessionUpdated
		if err := json.Unmarshal(env.Data, &upd); err == nil {
			for i := range m.Sessions {
				if m.Sessions[i].ID == upd.SessionID {
					applyPatch(&m.Sessions[i], upd.Patch)
					break
				}
			}
			if s, ok := upd.Patch["state"].(string); ok && protocol.State(s) == protocol.StateDone {
				if m.BlinkUntil == nil {
					m.BlinkUntil = make(map[string]time.Time)
				}
				m.BlinkUntil[upd.SessionID] = time.Now().Add(1600 * time.Millisecond)
				if m.Audio != nil {
					m.Audio.Play(audio.EventDone)
				}
			}
		}
	case protocol.EventSnapshot:
		var snap protocol.Snapshot
		if err := json.Unmarshal(env.Data, &snap); err == nil {
			m.Sessions = snap.Sessions
			if snap.Filter != "" {
				m.Filter = snap.Filter
			}
		}
	}
	return m
}

func applyPatch(s *protocol.SessionSummary, patch map[string]any) {
	if v, ok := patch["state"].(string); ok {
		s.State = protocol.State(v)
	}
	if v, ok := patch["slug"].(string); ok {
		s.Slug = v
	}
	if v, ok := patch["title"].(string); ok {
		s.Title = v
	}
	if v, ok := patch["last_line"].(string); ok {
		s.LastLine = v
	}
}
