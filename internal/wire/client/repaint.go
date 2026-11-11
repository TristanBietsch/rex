package client

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// How long a RepaintWatch waits for the Subscribe's reply before it gives
// up on it (a daemon that predates acks never sends one) and sends the
// redraw nudge. With a replay the reply comes after up to 256 KiB of
// transcript, which the attach also has to read and draw first.
const (
	RepaintReplyWait  = 250 * time.Millisecond
	RepaintReplayWait = 2 * time.Second
)

// RepaintWatch decides whether an attach that subscribed with
// Subscribe.Repaint (via SubscribeID) still needs the ^L redraw nudge,
// which asks a full-screen agent to redraw itself. A daemon that repaints
// sends the repaint before its reply, so the nudge is sent only when the
// reply comes without one (a daemon that predates repaints), or when no
// reply comes within the wait (one that predates acks). An Error reply (no
// such session) sends nothing.
//
// Feed it every event the attach reads, in order, with Observe. The nudge
// runs at most once, on the goroutine that calls Observe or on a timer's.
type RepaintWatch struct {
	sessionID string
	reqID     string
	nudge     func()

	done  atomic.Bool // a decision was made; Observe stops looking
	mu    sync.Mutex
	timer *time.Timer
	// nudged is set when the decision was to nudge. Guarded by mu.
	nudged bool
}

// WatchRepaint starts watching the Subscribe sent as reqID for sessionID.
// wait > 0 bounds the wait for its reply; 0 waits for it indefinitely.
func WatchRepaint(sessionID, reqID string, wait time.Duration, nudge func()) *RepaintWatch {
	w := &RepaintWatch{sessionID: sessionID, reqID: reqID, nudge: nudge}
	if wait > 0 {
		w.mu.Lock()
		w.timer = time.AfterFunc(wait, func() { w.decide(true) })
		w.mu.Unlock()
	}
	return w
}

// Observe looks at one event the attach read. It is safe on a nil watch.
func (w *RepaintWatch) Observe(env protocol.Envelope) {
	if w == nil || w.done.Load() {
		return
	}
	switch {
	case env.Type == protocol.EventSessionOutput:
		var so protocol.SessionOutput
		if json.Unmarshal(env.Data, &so) == nil && so.Repaint && so.SessionID == w.sessionID {
			w.decide(false)
		}
	case env.ID != "" && env.ID == w.reqID:
		w.decide(env.Type == protocol.EventAck)
	}
}

// Stop ends the watch without a nudge, unless one was already decided. It
// is safe on a nil watch.
func (w *RepaintWatch) Stop() {
	if w != nil {
		w.decide(false)
	}
}

// Nudged reports whether the watch sent (or is sending) the nudge.
func (w *RepaintWatch) Nudged() bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.nudged
}

func (w *RepaintWatch) decide(nudge bool) {
	w.mu.Lock()
	if w.done.Load() {
		w.mu.Unlock()
		return
	}
	w.done.Store(true)
	w.nudged = nudge
	if w.timer != nil {
		w.timer.Stop()
	}
	w.mu.Unlock()
	if nudge && w.nudge != nil {
		w.nudge()
	}
}
