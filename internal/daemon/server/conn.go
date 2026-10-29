package server

import (
	"errors"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// clientQueueLen bounds a client's outbound queue, in events. A client that
// falls this far behind is disconnected rather than allowed to stall the
// producers feeding it (store broadcasts, PTY output).
const clientQueueLen = 2048

// clientWriteTimeout bounds one socket write: a client that accepts no bytes
// for this long is disconnected.
const clientWriteTimeout = 10 * time.Second

// clientFlushTimeout bounds how long a closing connection waits for queued
// events (such as a final Error) to go out.
const clientFlushTimeout = time.Second

// client is one connected socket. Its handler goroutine reads intents;
// everything sent to it goes through a bounded queue drained by a dedicated
// writer goroutine, so senders never block on the socket.
type client struct {
	conn       net.Conn
	out        chan []byte
	writerDone chan struct{}

	mu sync.Mutex
	// closed stops further sends: the client was dropped or is finishing.
	closed bool
	// phase gates board events on Hello; held keeps the ones that arrive
	// while Hello takes its Snapshot.
	phase helloPhase
	held  []heldEvent
}

// helloPhase is where a client is in the Hello handshake. Board events
// (store changes, summarizer health) reach a client only after its Snapshot.
type helloPhase int

const (
	helloPending  helloPhase = iota // no Hello yet: board events are dropped
	helloSnapshot                   // taking the Snapshot: board events are held
	helloDone                       // board events are sent as they happen
)

// heldEvent is a board event waiting for the Snapshot to go out first.
type heldEvent struct {
	typ  string
	data any
	// addedID is the session id of a SessionAdded, so one the Snapshot
	// already contains can be skipped.
	addedID string
}

func newClient(conn net.Conn) *client {
	c := &client{
		conn:       conn,
		out:        make(chan []byte, clientQueueLen),
		writerDone: make(chan struct{}),
	}
	go c.writeLoop()
	return c
}

// send queues an event for the client. It never blocks.
func (c *client) send(eventType, id string, data any) {
	line, err := protocol.EncodeEvent(eventType, id, data)
	if err != nil {
		slog.Error("server: encode event failed", "type", eventType, "id", id, "err", err)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.enqueueLocked(line)
}

// sendBoard queues a board event once the client has its Snapshot. Before
// Hello it is dropped (the Snapshot will reflect it); during Hello it is held.
func (c *client) sendBoard(eventType string, data any, addedID string) {
	c.mu.Lock()
	switch c.phase {
	case helloPending:
		c.mu.Unlock()
		return
	case helloSnapshot:
		if len(c.held) >= clientQueueLen {
			slog.Warn("server: client fell behind during hello, disconnecting", "held", len(c.held))
			c.dropLocked()
		} else {
			c.held = append(c.held, heldEvent{typ: eventType, data: data, addedID: addedID})
		}
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()
	c.send(eventType, "", data)
}

// hello answers a Hello: the Snapshot, then every board event that arrived
// while it was taken, so none are lost in the gap or sent ahead of it. Board
// events are held from before snapshot runs, so a change lands in the
// Snapshot, in a held event, or both — a duplicate patch is harmless, and a
// SessionAdded for a session the Snapshot already holds is skipped.
func (c *client) hello(id string, snapshot func() protocol.Snapshot) {
	c.mu.Lock()
	c.phase = helloSnapshot
	c.mu.Unlock()

	snap := snapshot()
	inSnap := make(map[string]bool, len(snap.Sessions))
	for _, s := range snap.Sessions {
		inSnap[s.ID] = true
	}
	line, err := protocol.EncodeEvent(protocol.EventSnapshot, id, snap)
	if err != nil {
		slog.Error("server: encode snapshot failed", "err", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if line != nil {
		c.enqueueLocked(line)
	}
	for _, h := range c.held {
		if h.addedID != "" && inSnap[h.addedID] {
			continue
		}
		if b, err := protocol.EncodeEvent(h.typ, "", h.data); err == nil {
			c.enqueueLocked(b)
		}
	}
	c.held = nil
	c.phase = helloDone
}

// sendError queues an Error event correlated to intent id.
func (c *client) sendError(id, code, msg string) {
	c.send(protocol.EventError, id, protocol.ErrorEvent{ID: id, Code: code, Message: msg})
}

// enqueueLocked queues an encoded line, dropping the client when its queue
// is full. Caller holds c.mu.
func (c *client) enqueueLocked(line []byte) {
	if c.closed {
		return
	}
	select {
	case c.out <- line:
	default:
		slog.Warn("server: client not reading; outbound queue full, disconnecting", "queued", len(c.out))
		c.dropLocked()
	}
}

// dropLocked stops further sends and closes the socket, which ends the
// handler's Read and any write in flight. Caller holds c.mu.
func (c *client) dropLocked() {
	c.closed = true
	_ = c.conn.Close()
}

func (c *client) writeLoop() {
	defer close(c.writerDone)
	for line := range c.out {
		_ = c.conn.SetWriteDeadline(time.Now().Add(clientWriteTimeout))
		if _, err := c.conn.Write(line); err != nil {
			c.mu.Lock()
			if !c.closed {
				if errors.Is(err, os.ErrDeadlineExceeded) {
					slog.Warn("server: client not reading; write timed out, disconnecting", "timeout", clientWriteTimeout)
				} else {
					slog.Debug("server: write to client failed", "err", err)
				}
			}
			c.dropLocked()
			c.mu.Unlock()
			return
		}
	}
}

// finish ends the connection: a bounded wait for queued events to flush,
// then close. Later sends are ignored. The handler calls it once, on exit.
func (c *client) finish() {
	c.mu.Lock()
	c.closed = true
	close(c.out)
	c.mu.Unlock()
	select {
	case <-c.writerDone:
	case <-time.After(clientFlushTimeout):
	}
	_ = c.conn.Close()
	<-c.writerDone
}
