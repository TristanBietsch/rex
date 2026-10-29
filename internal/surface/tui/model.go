package tui

import (
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/features/audio"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
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
	FocusRename
)

// Model is the root Bubble Tea model.
type Model struct {
	Client     *client.Client
	Socket     string // path to daemon UDS, forwarded to child `rex attach`
	Focus      Focus
	Width      int
	Height     int
	Sessions   []protocol.SessionSummary
	SelectedID string
	// PendingSelectSlug is the slug of a session this TUI just asked the
	// daemon to spawn; its SessionAdded selects it.
	PendingSelectSlug string
	Filter            string
	PromptText        string
	CmdText           string
	PendingChord      string
	Err               string
	// Notice is a transient, non-error status line message (cleared on the
	// next board key).
	Notice      string
	SpinnerTick int
	Quitting    bool

	Wizard   *WizardState
	Settings *SettingsState
	Attach   *AttachState
	Fail     *FailState
	Rename   *RenameState
	// Audio is the active soundset player. Interface so tests can swap a recording fake.
	Audio audioPlayer

	// Clock overrides time.Now for rendering (tests). nil = wall clock.
	Clock func() time.Time
	// namer / retirer / spawner override Client for those intents (tests).
	namer   sessionNamer
	retirer sessionRetirer
	spawner sessionSpawner

	// Store is the live settings store. Renderers read from it; the settings
	// page mutates it and triggers live-apply side effects.
	Store     *settings.Store
	StorePath string

	// PendingDeleteID is the session targeted by an in-progress delete confirmation.
	PendingDeleteID string

	// BlinkUntil tracks done-blink expiry per session.
	BlinkUntil map[string]time.Time

	// lastNotify is when each session last played a state-change sound
	// (debounce; see notify.go). Pruned on SessionRemoved.
	lastNotify map[string]time.Time

	// DescAnim is the active per-session description animation. Entries are
	// pruned by the descTickMsg handler once the animation completes.
	DescAnim        map[string]DescAnim
	descTickPending bool

	// spinnerGen / spinnerRunning track the single live spinner tick chain.
	spinnerGen     int
	spinnerRunning bool

	// BackendUnavailable mirrors the daemon's summarizer health flag, delivered
	// via SummarizerHealth events.
	BackendUnavailable       bool
	BackendUnavailableReason string

	// ScrollOffset is how many board lines to skip from the top (for scroll).
	ScrollOffset int

	// HelpScroll is how many help lines the help popup is scrolled past
	// (j/k, pgup/pgdn). The popup resets it when it closes.
	HelpScroll int

	// mouseOn is whether rex last told the terminal to report mouse events
	// (see syncMouse). lastClickID / lastClickAt detect board double-clicks.
	mouseOn     bool
	lastClickID string
	lastClickAt time.Time

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
	SetBootVoice(b bool)
}

func (m Model) applyEvent(env protocol.Envelope) Model {
	switch env.Type {
	case protocol.EventSessionAdded:
		var sum protocol.SessionSummary
		if err := json.Unmarshal(env.Data, &sum); err == nil {
			sum = oneLineSummary(sum)
			m.Sessions = append(m.Sessions, sum)
			if m.PendingSelectSlug != "" && sum.Slug == m.PendingSelectSlug {
				m.SelectedID = sum.ID
				m.PendingSelectSlug = ""
			}
			if m.Audio != nil {
				m.Audio.Play(audio.EventCreate)
			}
		}
	case protocol.EventSessionRemoved:
		var rem protocol.SessionRemoved
		if err := json.Unmarshal(env.Data, &rem); err == nil {
			if m.Attach != nil && m.Attach.SessionID == rem.SessionID {
				m.Attach.Ended = true
				m.Attach.EndedMsg = "session deleted"
				if rem.Reason == protocol.RemovedRetired {
					m.Attach.EndedMsg = "session cleared (marked done)"
				}
				slog.Info("tui: session removed while attached", "session", rem.SessionID, "reason", rem.Reason)
			}
			if m.Rename != nil && m.Rename.SessionID == rem.SessionID {
				m.Rename = nil
				m.Focus = FocusBoard
				m.Err = "rename: session removed"
				slog.Info("tui: rename target removed", "session", rem.SessionID)
			}
			if rem.Reason == protocol.RemovedRetired {
				if sum, ok := findSession(m.Sessions, rem.SessionID); ok {
					m.Notice = "cleared " + displayName(sum) + " (marked done)"
				}
			}
			if m.SelectedID == rem.SessionID {
				m.SelectedID = neighborOf(m, rem.SessionID)
			}
			// A fresh slice: filtering in place would rewrite the backing
			// array that earlier copies of the Model still share.
			out := make([]protocol.SessionSummary, 0, len(m.Sessions))
			for _, s := range m.Sessions {
				if s.ID != rem.SessionID {
					out = append(out, s)
				}
			}
			m.Sessions = out
			delete(m.lastNotify, rem.SessionID)
			if m.Audio != nil {
				m.Audio.Play(audio.EventDelete)
			}
		}
	case protocol.EventSessionUpdated:
		var upd protocol.SessionUpdated
		if err := json.Unmarshal(env.Data, &upd); err == nil {
			// prevState is the state before this patch; known is false for a
			// session this TUI never saw (no live transition to announce).
			var prevState protocol.State
			known := false
			for i := range m.Sessions {
				if m.Sessions[i].ID == upd.SessionID {
					known = true
					prevState = m.Sessions[i].State
					prevDesc := m.Sessions[i].Description
					applyPatch(&m.Sessions[i], upd.Patch)
					if m.Sessions[i].Description != prevDesc {
						m = registerDescAnim(m, m.Sessions[i].ID, prevDesc, m.Sessions[i].Description)
					}
					break
				}
			}
			s, hasState := upd.Patch["state"].(string)
			next := protocol.State(s)
			// A repeated done patch is not a new completion; stay silent.
			if hasState && next == protocol.StateDone && (!known || prevState != protocol.StateDone) {
				if m.BlinkUntil == nil {
					m.BlinkUntil = make(map[string]time.Time)
				}
				m.BlinkUntil[upd.SessionID] = m.now().Add(1600 * time.Millisecond)
				// Covers needs_input → done too: one transition, one sound.
				if m.Audio != nil {
					m.Audio.Play(audio.EventDone)
				}
			}
			if hasState {
				m = m.notifyStateChange(upd.SessionID, prevState, next, known)
			}
		}
	case protocol.EventSnapshot:
		var snap protocol.Snapshot
		if err := json.Unmarshal(env.Data, &snap); err == nil {
			for i := range snap.Sessions {
				snap.Sessions[i] = oneLineSummary(snap.Sessions[i])
			}
			m.Sessions = snap.Sessions
			if snap.Filter != "" {
				m.Filter = snap.Filter
			}
		}
	case protocol.EventError:
		var ee protocol.ErrorEvent
		if err := json.Unmarshal(env.Data, &ee); err == nil {
			slog.Warn("tui: daemon error event", "code", ee.Code, "message", ee.Message, "id", ee.ID)
			m.Err = "daemon: " + protocol.SingleLine(friendlyDaemonError(ee))
			// A rejected spawn will never produce SessionAdded.
			m.PendingSelectSlug = ""
		}
	case protocol.EventSummarizerHealth:
		var h protocol.SummarizerHealth
		if err := json.Unmarshal(env.Data, &h); err == nil {
			m.BackendUnavailable = !h.Available
			// Often a raw HTTP error body; the header banner is one row.
			m.BackendUnavailableReason = protocol.SingleLine(h.Reason)
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
		s.Slug = protocol.SingleLine(v)
	}
	if v, ok := patch["title"].(string); ok {
		s.Title = protocol.SingleLine(v)
	}
	if v, ok := patch["last_line"].(string); ok {
		s.LastLine = protocol.SingleLine(v)
	}
	if v, ok := patch["description"].(string); ok {
		s.Description = protocol.SingleLine(v)
	}
	if v, ok := patch["fleet"].(string); ok {
		s.Fleet = protocol.SingleLine(v)
	}
	if v, ok := patch["name"].(string); ok {
		s.Name = protocol.SingleLine(v)
	}
	if t, ok := patchTime(patch, "last_event_at"); ok && !t.IsZero() {
		s.LastEventAt = t
	}
	if t, ok := patchTime(patch, "retire_at"); ok {
		s.RetireAt = t
	}
	if t, ok := patchTime(patch, "retired_at"); ok {
		s.RetiredAt = t
	}
	if t, ok := patchTime(patch, "working_since"); ok {
		s.WorkingSince = t
	}
	// Token fields come as float64 over JSON; coerce safely.
	if v, ok := patch["tokens"].(float64); ok {
		s.Tokens = int64(v)
	}
	if v, ok := patch["output_bytes"].(float64); ok {
		s.OutputBytes = int64(v)
	}
}

// oneLineSummary makes s's free-text fields one terminal row
// (protocol.SingleLine). The daemon relays them verbatim: a multi-line
// prompt as Title, agent output as LastLine, model output as Description.
func oneLineSummary(s protocol.SessionSummary) protocol.SessionSummary {
	s.Slug = protocol.SingleLine(s.Slug)
	s.Title = protocol.SingleLine(s.Title)
	s.LastLine = protocol.SingleLine(s.LastLine)
	s.Description = protocol.SingleLine(s.Description)
	s.Fleet = protocol.SingleLine(s.Fleet)
	s.Name = protocol.SingleLine(s.Name)
	return s
}

// patchTime reads an optional timestamp patch key: an RFC3339 string sets
// it, null clears it (zero time). ok is false when the key is absent or
// unparseable.
func patchTime(patch map[string]any, key string) (time.Time, bool) {
	v, present := patch[key]
	if !present {
		return time.Time{}, false
	}
	if v == nil {
		return time.Time{}, true
	}
	str, isStr := v.(string)
	if !isStr {
		slog.Warn("tui: patch time not a string", "key", key)
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, str)
	if err != nil {
		slog.Warn("tui: patch time parse failed", "key", key, "err", err)
		return time.Time{}, false
	}
	return t, true
}

// friendlyDaemonError maps protocol errors to actionable status text.
func friendlyDaemonError(ee protocol.ErrorEvent) string {
	switch {
	case ee.Code == protocol.ErrCodeBadIntent && ee.Message == "intent not implemented":
		return "daemon is older than rex — run 'rex daemon restart'"
	case ee.Code == protocol.ErrCodeBadState && strings.HasPrefix(ee.ID, "unretire-"):
		return "too late — session is already being cleared"
	}
	return ee.Message
}

// neighborOf picks the row to select after id leaves the board: the next row
// in board order, else the previous one, else none.
func neighborOf(m Model, id string) string {
	rows := orderedSessions(m)
	for i, s := range rows {
		if s.ID != id {
			continue
		}
		if i+1 < len(rows) {
			return rows[i+1].ID
		}
		if i > 0 {
			return rows[i-1].ID
		}
	}
	return ""
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
