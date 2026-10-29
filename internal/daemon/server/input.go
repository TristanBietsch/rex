package server

import (
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// replySubmitDelay separates a Reply's text from its Enter keystroke so TUIs
// commit the text to their input box before submitting (matches the initial
// prompt paste path in pty.Supervisor). A var so tests can stretch it.
var replySubmitDelay = 120 * time.Millisecond

// sessionInput is a running session's PTY input channel plus the gate that
// orders client writes to it: a Reply's Enter goes out replySubmitDelay
// after its text, and input sent meanwhile (from any connection) must not
// overtake it.
type sessionInput struct {
	ch chan []byte

	mu sync.Mutex
	// enterDue is closed once a pending Reply's Enter has been written; nil
	// when none is pending.
	enterDue chan struct{}
}

// lock waits out a pending Enter, then holds the gate. Callers unlock in.mu.
func (in *sessionInput) lock() {
	for {
		in.mu.Lock()
		due := in.enterDue
		if due == nil {
			return
		}
		in.mu.Unlock()
		<-due
	}
}

// trySend writes b without blocking; false means the buffer is full.
// Caller holds the gate.
func (in *sessionInput) trySend(b []byte) bool {
	select {
	case in.ch <- b:
		return true
	default:
		return false
	}
}

// reply types text, then presses Enter after replySubmitDelay without
// blocking the caller. Terminals send Enter as \r (the PTY maps it to \n for
// line REPLs); full-screen TUIs like Claude Code insert a newline on \n
// instead of submitting, and treat text+\r in one burst as a paste. Returns
// false (nothing written) when the buffer is full; entered reports, later,
// whether the Enter made it.
func (in *sessionInput) reply(text string, entered func(ok bool)) bool {
	in.lock()
	if !in.trySend([]byte(text)) {
		in.mu.Unlock()
		return false
	}
	due := make(chan struct{})
	in.enterDue = due
	in.mu.Unlock()

	time.AfterFunc(replySubmitDelay, func() {
		in.mu.Lock()
		ok := in.trySend([]byte{'\r'})
		in.enterDue = nil
		in.mu.Unlock()
		close(due)
		entered(ok)
	})
	return true
}

// sanitizeReplyText drops what could drive the terminal or submit early
// from Reply text: C0 controls except tab and newline (ESC, CR, ...), DEL
// and C1 controls. Invalid UTF-8 is dropped too.
func sanitizeReplyText(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n':
			return r
		case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f, r == utf8.RuneError:
			return -1
		}
		return r
	}, s)
}
