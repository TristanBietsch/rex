package tui

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/tristanbietsch/rex/internal/audio"
	"github.com/tristanbietsch/rex/internal/client"
	"github.com/tristanbietsch/rex/internal/protocol"
	"github.com/tristanbietsch/rex/internal/settings"
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
	FocusAttach
	FocusFail
	FocusBoot
	FocusStats
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
	Attach   *AttachState
	Fail     *FailState
	// Audio is the active soundset player. Interface so tests can swap a recording fake.
	Audio audioPlayer

	// Store is the live settings store. Renderers read from it; the settings
	// page mutates it and triggers live-apply side effects.
	Store     *settings.Store
	StorePath string

	// PendingDeleteID is the session targeted by an in-progress delete confirmation.
	PendingDeleteID string

	// BlinkUntil tracks done-blink expiry per session.
	BlinkUntil map[string]time.Time

	// DescAnim is the active per-session description animation. Entries are
	// pruned by the descTickMsg handler once the animation completes.
	DescAnim        map[string]DescAnim
	descTickPending bool

	// BackendUnavailable mirrors the daemon's summarizer health flag, delivered
	// via SummarizerHealth events.
	BackendUnavailable       bool
	BackendUnavailableReason string

	// ScrollOffset is how many board lines to skip from the top (for scroll).
	ScrollOffset int

	// Boot / splash state.
	BootLog     []bootLine
	BootStep    int
	BootStart   time.Time
	BootMinDone bool
	BootDone    bool
	BootFailed  bool
	BootError   error
}

// audioPlayer is the subset of *audio.Player the TUI uses.
type audioPlayer interface {
	Play(event string)
	SetVolume(v float64)
	SetEnabled(b bool)
	SetSoundset(name string)
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
					prevDesc := m.Sessions[i].Description
					applyPatch(&m.Sessions[i], upd.Patch)
					if m.Sessions[i].Description != prevDesc {
						m = registerDescAnim(m, m.Sessions[i].ID, prevDesc, m.Sessions[i].Description)
					}
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
	case protocol.EventSummarizerHealth:
		var h protocol.SummarizerHealth
		if err := json.Unmarshal(env.Data, &h); err == nil {
			m.BackendUnavailable = !h.Available
			m.BackendUnavailableReason = h.Reason
			slog.Info("tui: summarizer_health",
				"available", h.Available,
				"reason", h.Reason,
			)
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
	if v, ok := patch["description"].(string); ok {
		s.Description = v
	}
	if v, ok := patch["fleet"].(string); ok {
		s.Fleet = v
	}
	// Token fields come as float64 over JSON; coerce safely.
	if v, ok := patch["tokens"].(float64); ok {
		s.Tokens = int64(v)
	}
	if v, ok := patch["output_bytes"].(float64); ok {
		s.OutputBytes = int64(v)
	}
}

// registerDescAnim records a description-change animation. Respects
// reduce_motion and the desc_animation setting.
func registerDescAnim(m Model, id, from, to string) Model {
	if to == "" {
		return m
	}
	var effect string
	if m.Store != nil {
		effect, _ = m.Store.Get("desc_animation").(string)
		if rm, _ := m.Store.Get("reduce_motion").(bool); rm {
			effect = "off"
		}
	}
	if effect == "" || effect == "off" {
		return m
	}
	if m.DescAnim == nil {
		m.DescAnim = map[string]DescAnim{}
	}
	if _, replacing := m.DescAnim[id]; replacing {
		slog.Info("desc_anim: replaced_in_flight", "session", id)
	}
	m.DescAnim[id] = DescAnim{
		From: from, To: to, Effect: effect,
		StartedAt: time.Now(),
		Duration:  animDurationFor(effect),
	}
	slog.Debug("desc_anim: start",
		"session", id,
		"effect", effect,
		"duration_ms", animDurationFor(effect).Milliseconds(),
	)
	return m
}

func animDurationFor(effect string) time.Duration {
	switch effect {
	case "decode":
		return 400 * time.Millisecond
	case "wipe":
		return 250 * time.Millisecond
	default:
		return 300 * time.Millisecond
	}
}
