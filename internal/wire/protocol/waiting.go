package protocol

import "time"

// What a working session can be waiting on (SessionSummary.WaitingOn).
const (
	WaitShell    = "shell"    // a shell command (Claude's Bash tool)
	WaitSubagent = "subagent" // a subagent (Claude's Task / Agent tool)
	WaitWeb      = "web"      // a web fetch or search
)

// WaitingAfter is how long a tool call must run before clients show the
// session as waiting rather than working. Quick calls never flicker.
const WaitingAfter = 3 * time.Second

// ValidWaitKind reports whether k is a WaitingOn value this version knows.
func ValidWaitKind(k string) bool {
	switch k {
	case WaitShell, WaitSubagent, WaitWeb:
		return true
	}
	return false
}

// WaitingFor is the kind the session is waiting on at now, or "": the
// session is working (not marked done), has a known WaitingOn, and its wait
// began at least WaitingAfter ago. It is the one definition of "waiting"
// the board and the CLI share.
func (s SessionSummary) WaitingFor(now time.Time) string {
	if s.State != StateWorking || !s.RetireAt.IsZero() || !ValidWaitKind(s.WaitingOn) || s.WaitingSince.IsZero() {
		return ""
	}
	if now.Sub(s.WaitingSince) < WaitingAfter {
		return ""
	}
	return s.WaitingOn
}
