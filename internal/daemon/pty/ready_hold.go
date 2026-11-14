package pty

import (
	"time"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// readyHoldDefault is how long an adapter's working→needs_input change must
// hold before the supervisor publishes it. A session that goes back to
// working inside the window never shows READY (and never plays its sound).
// A question (wait reason input) skips the hold.
const readyHoldDefault = 2 * time.Second

// readyHold debounces working→needs_input. It is used from the supervisor's
// tick goroutine only. hold 0 publishes at once.
type readyHold struct {
	hold  time.Duration
	since time.Time // start of the pending entry; zero when none
}

// pending reports whether a working→needs_input change waits out the hold.
func (h *readyHold) pending() bool { return !h.since.IsZero() }

// reset drops a pending entry (the adapter had no answer this tick).
func (h *readyHold) reset() { h.since = time.Time{} }

// admit decides whether the change current→next is published now. why is
// the reason logged with the transition: "question", "held" or "adapter"
// for a published change; "hold" when a hold starts, "flap" when the
// adapter went back before the hold ran out, "" otherwise.
func (h *readyHold) admit(current, next protocol.State, question bool, now time.Time) (publish bool, why string) {
	entering := current == protocol.StateWorking && next == protocol.StateNeedsInput
	if !entering {
		why := ""
		if h.pending() {
			why = "flap"
		}
		h.since = time.Time{}
		if next == current {
			return false, why
		}
		return true, "adapter"
	}
	switch {
	case question:
		h.since = time.Time{}
		return true, "question"
	case h.hold <= 0:
		return true, "adapter"
	case !h.pending():
		h.since = now
		return false, "hold"
	case now.Sub(h.since) >= h.hold:
		h.since = time.Time{}
		return true, "held"
	}
	return false, ""
}
