package server

import (
	"log/slog"
	"sync"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// streamOutput starts forwarding a session's PTY output to c, preceded by
// the transcript replay or the mode preamble p asks for. The sink is
// registered first and holds chunks until that preamble is queued, so
// output produced in between is neither lost nor sent ahead of it. (With
// replay, a chunk the PTY reader writes to the transcript just before the
// tail is read can also arrive held, once: a repeat beats a gap.) Returns
// the func that stops the stream.
func (s *Server) streamOutput(c *client, p protocol.Subscribe) func() {
	st := &outputStream{c: c, sessionID: p.SessionID}
	stop := s.SubscribeSessionOutput(p.SessionID, st.deliver)

	var preambleBytes int
	if p.Replay {
		if tail, err := state.TranscriptTail(s.TranscriptDir(), p.SessionID, transcriptReplayMax); err == nil && len(tail) > 0 {
			c.send(protocol.EventSessionOutput, "", protocol.SessionOutput{
				SessionID: p.SessionID, Bytes: tail,
			})
		}
	} else if p.ModeSync {
		// The replay already carries the child's mode switches;
		// without one, send them as a preamble.
		if pre := s.ModePreamble(p.SessionID); len(pre) > 0 {
			preambleBytes = len(pre)
			c.send(protocol.EventSessionOutput, "", protocol.SessionOutput{
				SessionID: p.SessionID, Bytes: pre,
			})
		}
	}
	held := st.goLive()
	slog.Info("server: subscribe", "session", p.SessionID, "replay", p.Replay,
		"mode_sync", p.ModeSync, "preamble_bytes", preambleBytes, "held_chunks", held)
	return stop
}

// outputStream is one client's sink for one session's PTY output. Until
// goLive it holds chunks instead of sending them.
type outputStream struct {
	c         *client
	sessionID string

	mu   sync.Mutex
	live bool
	held [][]byte
}

// deliver runs on the PTY reader; it never blocks on the socket.
func (o *outputStream) deliver(b []byte) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.live {
		o.held = append(o.held, b)
		return
	}
	o.send(b)
}

// goLive sends the held chunks, then lets later ones through directly.
// Returns how many were held.
func (o *outputStream) goLive() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	n := len(o.held)
	for _, b := range o.held {
		o.send(b)
	}
	o.held = nil
	o.live = true
	return n
}

func (o *outputStream) send(b []byte) {
	o.c.send(protocol.EventSessionOutput, "", protocol.SessionOutput{SessionID: o.sessionID, Bytes: b})
}
