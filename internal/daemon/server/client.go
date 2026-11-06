package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"runtime/debug"
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
	// cleanups run when the connection ends (output sinks registered by
	// Subscribe). Handler goroutine only.
	cleanups []func()
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

// subscribeBoard forwards store events and SummarizerHealth to this client,
// both gated on subscribed so nothing arrives before the Hello snapshot.
func (c *client) subscribeBoard() {
	c.cleanups = append(c.cleanups,
		c.srv.cfg.Store.Subscribe(func(e state.Event) {
			if c.subscribed.Load() {
				c.emitStoreEvent(e)
			}
		}),
		c.srv.SubscribeSummarizerHealth(func(h protocol.SummarizerHealth) {
			if c.subscribed.Load() {
				c.sendEvent(protocol.EventSummarizerHealth, "", h)
			}
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
