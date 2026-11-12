package tui

import (
	"log/slog"
	"time"

	"github.com/tristanbietsch/rex/internal/features/audio"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// notifyDebounce is the minimum gap between two state-change sounds for one
// session. Heuristic adapters can flicker needs_input↔working; without this a
// single redraw storm would chime repeatedly.
const notifyDebounce = 1500 * time.Millisecond

// stateChangeEvent maps a live state transition to its notification sound, or
// "" when the transition is silent. Transitions into done are not listed: the
// done sound (played for every → done) already covers needs_input → done, so
// one transition never plays two sounds.
func stateChangeEvent(prev, next protocol.State) string {
	switch {
	case prev == protocol.StateNeedsInput && next == protocol.StateWorking:
		return audio.EventResume
	case prev == protocol.StateWorking && next == protocol.StateNeedsInput:
		return audio.EventAttention
	}
	return ""
}

// notifyOnStateChange reads the notify_on_state_change setting live, so the
// settings page toggle applies without a restart. Defaults on.
func notifyOnStateChange(m Model) bool {
	if m.Store == nil {
		return true
	}
	b, ok := m.Store.Get("notify_on_state_change").(bool)
	if !ok {
		return true
	}
	return b
}

// notifyStateChange plays the transition sound for one SessionUpdated state
// patch, subject to the setting and the per-session debounce. known is false
// when the session wasn't in m.Sessions before the patch: its prior state is
// unknown (never seen live), so nothing plays.
func (m Model) notifyStateChange(id string, prev, next protocol.State, known bool) Model {
	ev := stateChangeEvent(prev, next)
	if ev == "" {
		return m
	}
	if !known {
		slog.Debug("tui: state notify suppressed", "session", id, "event", ev, "reason", "unknown_session")
		return m
	}
	if !notifyOnStateChange(m) {
		slog.Debug("tui: state notify suppressed", "session", id, "event", ev, "from", prev, "to", next, "reason", "setting_off")
		return m
	}
	prompts := m.sessionPrompts(id)
	if ev == audio.EventAttention && prompts > 0 && m.rungPrompt[id] == prompts {
		slog.Debug("tui: state notify suppressed", "session", id, "event", ev, "from", prev, "to", next,
			"reason", "same_turn", "prompts", prompts)
		return m
	}
	return m.playNotify(id, ev, prev, next, prompts)
}

// notifyQuestion plays the attention sound when a wait_reason patch turns a
// READY session's reason into input (a new question). The state patch of
// the same question comes first and, when it played, the debounce keeps
// this one silent; when the turn already rang, this is the only sound.
func (m Model) notifyQuestion(id, prevReason string) Model {
	s, ok := m.sessionByID(id)
	if !ok || s.State != protocol.StateNeedsInput || s.WaitReason != protocol.WaitInput || prevReason == protocol.WaitInput {
		return m
	}
	// A question is not the end of the turn: the READY that ends it after
	// the answer must still chime, so the turn is not marked as rung.
	delete(m.rungPrompt, id)
	if !notifyOnStateChange(m) {
		return m
	}
	return m.playNotify(id, audio.EventAttention, s.State, s.State, 0)
}

// playNotify plays ev for a session unless it played a sound within
// notifyDebounce, and records the turn an attention sound belongs to.
func (m Model) playNotify(id, ev string, prev, next protocol.State, prompts int) Model {
	now := m.now()
	if last, ok := m.lastNotify[id]; ok && now.Sub(last) < notifyDebounce {
		slog.Debug("tui: state notify suppressed", "session", id, "event", ev, "from", prev, "to", next,
			"reason", "debounce", "since_last", now.Sub(last))
		return m
	}
	m = m.stampNotify(id, now)
	if ev == audio.EventAttention && prompts > 0 {
		if m.rungPrompt == nil {
			m.rungPrompt = make(map[string]int)
		}
		m.rungPrompt[id] = prompts
	}
	slog.Info("tui: state notify", "session", id, "event", ev, "from", prev, "to", next, "prompts", prompts)
	if m.Audio != nil {
		m.Audio.Play(ev)
	}
	return m
}

// stampNotify records a played session sound so the debounce window starts
// from it.
func (m Model) stampNotify(id string, at time.Time) Model {
	if m.lastNotify == nil {
		m.lastNotify = make(map[string]time.Time)
	}
	m.lastNotify[id] = at
	return m
}

// sessionByID returns the session with that id.
func (m Model) sessionByID(id string) (protocol.SessionSummary, bool) {
	for _, s := range m.Sessions {
		if s.ID == id {
			return s, true
		}
	}
	return protocol.SessionSummary{}, false
}

// sessionPrompts is a session's prompts count, 0 when unknown.
func (m Model) sessionPrompts(id string) int {
	s, _ := m.sessionByID(id)
	return s.Prompts
}

// forgetStaleTurn re-arms the attention sound when a session's prompts
// count went below the rung turn (the agent restarted its count).
func (m Model) forgetStaleTurn(id string) Model {
	if r, ok := m.rungPrompt[id]; ok && m.sessionPrompts(id) < r {
		delete(m.rungPrompt, id)
	}
	return m
}
