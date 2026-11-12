package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"runtime/debug"
	"sync"
	"sync/atomic"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// client is one accepted connection's handler state. The handler goroutine
// (handleClient) owns it; broadcast callbacks on other goroutines touch only
// subscribed and the send methods.
type client struct {
	srv *Server
	// ctx is the server's serve context. Sessions spawned over this
	// connection derive from it, so they outlive the connection.
	ctx context.Context
	out *clientConn
	// subscribed gates store and health events until the Hello snapshot has
	// been sent. Read by broadcast callbacks, so atomic.
	subscribed atomic.Bool
	// From the start of Hello until its Snapshot is queued, board events
	// are held (in order) instead of dropped: a change made after the
	// Snapshot read would otherwise reach neither the Snapshot nor the
	// client. gateMu guards holding and held.
	gateMu  sync.Mutex
	holding bool
	held    []func()
	// cleanups run when the connection ends (output sinks registered by
	// Subscribe). Handler goroutine only.
	cleanups []func()
	// viewer is set once this connection said Hello as a board viewer and
	// counted in Server.boardClients. Handler goroutine only.
	viewer bool
}

// joinBoard counts the connection as a board viewer (once, however many
// Hellos it sends) and releases the count when the connection ends.
func (c *client) joinBoard() {
	if c.viewer {
		return
	}
	c.viewer = true
	c.srv.boardClients.Add(1)
	c.cleanups = append(c.cleanups, func() { c.srv.boardClients.Add(-1) })
}

// intentHandler serves one intent type. It runs on the connection's handler
// goroutine. It returns the intent's outcome and never sends an Ack or an
// Error for the intent itself: dispatch turns the result into the intent's
// one terminal reply (see reply.go).
type intentHandler func(c *client, env protocol.Envelope) (outcome, error)

func handleClient(ctx context.Context, conn net.Conn, srv *Server) {
	out := newClientConn(conn, srv.nextConnID.Add(1), srv.clientQueueLen, srv.clientWriteTimeout)
	if !srv.trackConn(out) {
		out.abort(errServerShutdown) // accepted while Serve was shutting down
	}
	defer srv.untrackConn(out)
	c := &client{srv: srv, ctx: ctx, out: out}
	// Deferred calls run in reverse: subscriptions are canceled first, so no
	// broadcast can enqueue any more, then queued lines are flushed and the
	// socket closes.
	defer out.finish()
	defer c.runCleanups()
	c.subscribeBoard()

	r := protocol.NewReaderSize(conn, srv.maxLineBytes)
	for {
		if ctx.Err() != nil {
			return
		}
		env, err := r.Read()
		if err != nil {
			c.readFailed(err)
			return
		}
		if !c.dispatch(env) {
			return
		}
	}
}

// readFailed tells the client why the daemon is about to close on it when
// the cause is a protocol violation (the error line is flushed before the
// close). A clean EOF or a dead socket closes silently.
func (c *client) readFailed(err error) {
	code, ok := protocolErrorCode(err)
	if !ok {
		slog.Debug("server: client read ended", "conn", c.out.id, "err", err)
		return
	}
	slog.Warn("server: closing client after protocol error", "conn", c.out.id, "code", code, "err", err)
	c.sendError("", code, err.Error())
}

// protocolErrorCode maps a Reader error caused by the peer's bytes to the
// Error code the daemon reports; ok is false for transport errors.
func protocolErrorCode(err error) (code string, ok bool) {
	switch {
	case errors.Is(err, protocol.ErrLineTooLong):
		return protocol.ErrCodeLineTooLong, true
	case errors.Is(err, protocol.ErrUnsupportedVersion):
		return protocol.ErrCodeUnsupportedVersion, true
	case errors.Is(err, protocol.ErrMalformed), errors.Is(err, io.ErrUnexpectedEOF):
		return protocol.ErrCodeBadIntent, true
	default:
		return "", false
	}
}

// boardEvent delivers one board event that arrived before the client was
// live: held while Hello is taking its Snapshot, dropped before Hello, sent
// when the client went live meanwhile.
func (c *client) boardEvent(send func()) {
	if c.subscribed.Load() {
		send()
		return
	}
	c.gateMu.Lock()
	if c.holding {
		c.held = append(c.held, send)
		c.gateMu.Unlock()
		return
	}
	live := c.subscribed.Load() // went live while we waited for the lock
	c.gateMu.Unlock()
	if live {
		send()
	}
}

// holdBoard starts holding board events; Hello calls it before reading the
// Snapshot.
func (c *client) holdBoard() {
	c.gateMu.Lock()
	defer c.gateMu.Unlock()
	if !c.subscribed.Load() {
		c.holding = true
	}
}

// goLive sends the held events (after the Snapshot, which the caller has
// queued) and lets later ones through. A change can land in both the
// Snapshot and a held event; clients apply them idempotently.
func (c *client) goLive() {
	c.gateMu.Lock()
	defer c.gateMu.Unlock()
	for _, send := range c.held {
		send()
	}
	c.held, c.holding = nil, false
	c.subscribed.Store(true)
}

// subscribeBoard forwards store events, SummarizerHealth, Overview and Usage to this client,
// both gated on subscribed so nothing arrives before the Hello snapshot.
func (c *client) subscribeBoard() {
	c.cleanups = append(c.cleanups,
		c.srv.cfg.Store.Subscribe(func(e state.Event) {
			if c.subscribed.Load() { // fast path: no closure once live
				c.emitStoreEvent(e)
				return
			}
			c.boardEvent(func() { c.emitStoreEvent(e) })
		}),
		c.srv.SubscribeSummarizerHealth(func(h protocol.SummarizerHealth) {
			if c.subscribed.Load() { // fast path: no closure once live
				c.sendEvent(protocol.EventSummarizerHealth, "", h)
				return
			}
			c.boardEvent(func() { c.sendEvent(protocol.EventSummarizerHealth, "", h) })
		}),
		c.srv.SubscribeOverview(func(o protocol.Overview) {
			if c.subscribed.Load() { // fast path: no closure once live
				c.sendEvent(protocol.EventOverview, "", o)
				return
			}
			c.boardEvent(func() { c.sendEvent(protocol.EventOverview, "", o) })
		}),
		c.srv.SubscribeTools(func(m protocol.ToolsChanged) {
			if c.subscribed.Load() { // fast path: no closure once live
				c.sendEvent(protocol.EventToolsChanged, "", m)
				return
			}
			c.boardEvent(func() { c.sendEvent(protocol.EventToolsChanged, "", m) })
		}),
		c.srv.SubscribeUsage(func(u protocol.Usage) {
			if c.subscribed.Load() { // fast path: no closure once live
				c.sendEvent(protocol.EventUsage, "", u)
				return
			}
			c.boardEvent(func() { c.sendEvent(protocol.EventUsage, "", u) })
		}),
		c.srv.SubscribeLoops(func(l protocol.LoopSummary) {
			if c.subscribed.Load() { // fast path: no closure once live
				c.sendEvent(protocol.EventLoop, "", l)
				return
			}
			c.boardEvent(func() { c.sendEvent(protocol.EventLoop, "", l) })
		}),
	)
}

func (c *client) runCleanups() {
	for _, fn := range c.cleanups {
		fn()
	}
	c.cleanups = nil
}

// dispatch routes one envelope to its intent handler, sends the intent's
// terminal reply (see reply.go) and reports whether the connection should
// stay open.
//
// A panicking handler must not take the daemon, and every session it
// supervises, down with it. dispatch recovers, logs the stack, answers the
// intent with internal_error (the panic value stays in the log) and returns
// false so the connection closes: the handler may have left this
// connection's state half-updated. Shared state is not repaired; handlers
// release locks with defer so a panic cannot leave one held.
func (c *client) dispatch(env protocol.Envelope) (keepOpen bool) {
	defer func() {
		if p := recover(); p != nil {
			slog.Error("server: intent handler panicked; closing connection",
				"conn", c.out.id, "type", env.Type, "req_id", env.ID,
				"panic", p, "stack", string(debug.Stack()))
			c.sendError(env.ID, protocol.ErrCodeInternal, "internal error handling the intent; closing connection")
			keepOpen = false
		}
	}()
	if env.Kind != protocol.KindIntent {
		c.sendError(env.ID, protocol.ErrCodeBadIntent, "expected an Intent")
		return true
	}
	h := c.srv.handlers[env.Type]
	if h == nil {
		c.sendError(env.ID, protocol.ErrCodeBadIntent, "intent not implemented")
		return true
	}
	res, err := h(c, env)
	c.reply(env, res, err)
	return true
}

// sendEvent queues an event for this client. Safe from any goroutine and
// never blocks (see clientConn).
func (c *client) sendEvent(typ, id string, payload any) {
	c.out.sendEvent(typ, id, payload)
}

func (c *client) sendError(id, code, msg string) {
	c.sendEvent(protocol.EventError, id, protocol.ErrorEvent{ID: id, Code: code, Message: msg})
}

// decodeIntent unmarshals env.Data into a T; a failure is bad_intent.
func decodeIntent[T any](env protocol.Envelope) (T, error) {
	var p T
	if err := json.Unmarshal(env.Data, &p); err != nil {
		return p, replyErr(protocol.ErrCodeBadIntent, err)
	}
	return p, nil
}

// checkSessionID is requireSessionID as a bad_intent reply error.
func checkSessionID(id string) error {
	if err := requireSessionID(id); err != nil {
		return replyErr(protocol.ErrCodeBadIntent, err)
	}
	return nil
}

// decodeSessionIntent decodes a session intent's payload and validates the
// session ID that sessionID extracts from it.
func decodeSessionIntent[T any](env protocol.Envelope, sessionID func(T) string) (T, error) {
	p, err := decodeIntent[T](env)
	if err != nil {
		return p, err
	}
	return p, checkSessionID(sessionID(p))
}
