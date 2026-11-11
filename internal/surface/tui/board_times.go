package tui

import (
	"time"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// completedAt is when the row entered the Done column: the earlier of the
// time the user marked it done and the time it reached a terminal state.
// When a terminal session has no ended_at (an older daemon), its last event
// stands in. It is zero for a row still live and not marked.
func completedAt(s protocol.SessionSummary) time.Time {
	ended := s.EndedAt
	if ended.IsZero() && isTerminalState(s.State) {
		ended = s.LastEventAt
	}
	switch {
	case ended.IsZero():
		return s.RetiredAt
	case s.RetiredAt.IsZero() || ended.Before(s.RetiredAt):
		return ended
	}
	return s.RetiredAt
}

func isTerminalState(st protocol.State) bool {
	return st == protocol.StateDone || st == protocol.StateFailed || st == protocol.StateCrashed
}

// clockStamp formats t in now's zone: the time of day for today, "Jan 2"
// for this year, else the year.
func clockStamp(now, t time.Time) string {
	t = t.In(now.Location())
	switch {
	case t.Year() == now.Year() && t.YearDay() == now.YearDay():
		return t.Format("15:04")
	case t.Year() == now.Year():
		return t.Format("Jan 2")
	}
	return t.Format("2006")
}

// spanText is the start→end text of the detailed view's span column:
// "14:02→15:30", or "14:02→" while the row is live. It is "" without a
// start time.
func spanText(now time.Time, s protocol.SessionSummary) string {
	if s.StartedAt.IsZero() {
		return ""
	}
	out := clockStamp(now, s.StartedAt) + "→"
	if end := completedAt(s); !end.IsZero() {
		out += clockStamp(now, end)
	}
	return out
}
