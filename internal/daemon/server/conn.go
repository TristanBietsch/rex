package server

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Outbound limits per connection. A client that falls clientQueueLen lines
// behind, or does not accept a line within clientWriteTimeout, is
// disconnected rather than allowed to stall a broadcaster; clients reconnect
// and re-snapshot.
const (
	defaultClientQueueLen     = 1024
	defaultClientWriteTimeout = 5 * time.Second
)

// Reasons a connection is dropped, logged once per connection.
var (
	errQueueFull      = errors.New("outbound queue full")
	errFlushTimeout   = errors.New("final flush timed out")
	errServerShutdown = errors.New("daemon shutting down")
)

// clientConn owns one accepted socket's write side. Every line bound for the
// client, direct replies and broadcast events alike, is enqueued on a bounded
// queue drained by a single writer goroutine. Lines therefore reach the
// client in enqueue order, and no caller ever blocks on the socket.
//
// Concurrency contract:
//   - enqueue and sendEvent may be called from any goroutine, including the
//     store, summarizer-health and PTY-output broadcast callbacks, which run
//     with subscriber locks held. They never block: a full queue drops the
//     connection instead.
//   - abort may be called from any goroutine, any number of times.
//   - finish is called exactly once, by the connection's handler goroutine,
//     after it has canceled every subscription that could still enqueue.
type clientConn struct {
	nc           net.Conn
	id           uint64
	writeTimeout time.Duration

	mu     sync.Mutex
	out    chan []byte // sent to and closed only under mu
	closed bool        // guarded by mu: out is closed and enqueue drops lines

	aborted    atomic.Bool // the socket is closed; the writer discards lines
	abortOnce  sync.Once
	writerDone chan struct{} // closed when writeLoop returns

	// board is set once the connection said Hello as a board; meta is
	// what it said about itself (R20 rex upgrade).
	board atomic.Bool
	meta  boardMeta
}

// newClientConn wraps nc and starts its writer goroutine. queueLen and
// writeTimeout fall back to the defaults when <= 0.
func newClientConn(nc net.Conn, id uint64, queueLen int, writeTimeout time.Duration) *clientConn {
	if queueLen <= 0 {
		queueLen = defaultClientQueueLen
	}
	if writeTimeout <= 0 {
		writeTimeout = defaultClientWriteTimeout
	}
	c := &clientConn{
		nc:           nc,
		id:           id,
		writeTimeout: writeTimeout,
		out:          make(chan []byte, queueLen),
		writerDone:   make(chan struct{}),
	}
	go c.writeLoop()
	return c
}

// sendEvent encodes an Event envelope and enqueues it. Encoding happens on
// the caller's goroutine, so the payload (e.g. a PTY chunk the caller may
// reuse) is not retained.
func (c *clientConn) sendEvent(typ, id string, payload any) {
	line, err := protocol.EncodeEvent(typ, id, payload)
	if err != nil {
		slog.Error("server: encode event", "conn", c.id, "type", typ, "err", err)
		return
	}
	c.enqueue(line)
}

// enqueue queues one encoded line without blocking. After the queue closes
// lines are dropped; on overflow the connection is aborted.
func (c *clientConn) enqueue(line []byte) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	select {
	case c.out <- line:
		c.mu.Unlock()
		return
	default:
	}
	c.mu.Unlock()
	c.abort(fmt.Errorf("%w (%d lines)", errQueueFull, cap(c.out)))
}

// closeQueue stops accepting lines; the writer drains what is queued.
func (c *clientConn) closeQueue() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.closed = true
		close(c.out)
	}
}

// abort drops the connection now: it closes the queue and the socket, which
// fails any in-flight write and unblocks the handler's read. Only the first
// call has an effect, and it logs why.
func (c *clientConn) abort(reason error) {
	c.abortOnce.Do(func() {
		c.aborted.Store(true)
		c.closeQueue()
		_ = c.nc.Close()
		if isStallReason(reason) {
			slog.Warn("server: dropping client", "conn", c.id, "reason", reason)
		} else {
			slog.Debug("server: dropping client", "conn", c.id, "reason", reason)
		}
	})
}

// isStallReason reports whether the client was dropped for not keeping up,
// the case worth a warning; a peer that already hung up is routine.
func isStallReason(err error) bool {
	return errors.Is(err, errQueueFull) || errors.Is(err, errFlushTimeout) ||
		errors.Is(err, os.ErrDeadlineExceeded)
}

// finish ends the connection gracefully: no new lines are accepted, queued
// lines are flushed (bounded by one write timeout overall), then the socket
// closes. Callers must have canceled every subscription first.
func (c *clientConn) finish() {
	c.closeQueue()
	t := time.NewTimer(c.writeTimeout)
	defer t.Stop()
	select {
	case <-c.writerDone:
	case <-t.C:
		c.abort(errFlushTimeout)
		<-c.writerDone
	}
	_ = c.nc.Close()
}

// writeLoop is the connection's only socket writer. It exits once the queue
// is closed and drained; after an abort it discards the remaining lines.
func (c *clientConn) writeLoop() {
	defer close(c.writerDone)
	for line := range c.out {
		if c.aborted.Load() {
			continue
		}
		if err := c.write(line); err != nil {
			c.abort(err)
		}
	}
}

func (c *clientConn) write(line []byte) error {
	if err := c.nc.SetWriteDeadline(time.Now().Add(c.writeTimeout)); err != nil {
		return fmt.Errorf("set write deadline: %w", err)
	}
	if _, err := c.nc.Write(line); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}
