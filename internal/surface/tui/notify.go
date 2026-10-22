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
	now := m.now()
	if last, ok := m.lastNotify[id]; ok && now.Sub(last) < notifyDebounce {
		slog.Debug("tui: state notify suppressed", "session", id, "event", ev, "from", prev, "to", next,
			"reason", "debounce", "since_last", now.Sub(last))
		return m
	}
	m = m.stampNotify(id, now)
	slog.Info("tui: state notify", "session", id, "event", ev, "from", prev, "to", next)
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
