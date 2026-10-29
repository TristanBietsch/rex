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
