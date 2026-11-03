package server

import (
	"context"
	"encoding/json"
	"net"
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
// goroutine and replies through c.
type intentHandler func(c *client, env protocol.Envelope)

func handleClient(ctx context.Context, conn net.Conn, srv *Server) {
	out := newClientConn(conn, srv.nextConnID.Add(1), srv.clientQueueLen, srv.clientWriteTimeout)
	c := &client{srv: srv, ctx: ctx, out: out}
	// Deferred calls run in reverse: subscriptions are canceled first, so no
	// broadcast can enqueue any more, then queued lines are flushed and the
	// socket closes.
	defer out.finish()
	defer c.runCleanups()
	c.subscribeBoard()

	r := protocol.NewReader(conn)
	for {
		if ctx.Err() != nil {
			return
		}
		env, err := r.Read()
		if err != nil {
			return
		}
		c.dispatch(env)
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

// dispatch routes one envelope to its intent handler.
func (c *client) dispatch(env protocol.Envelope) {
	if env.Kind != protocol.KindIntent {
		c.sendError(env.ID, protocol.ErrCodeBadIntent, "expected an Intent")
		return
	}
	h := c.srv.handlers[env.Type]
	if h == nil {
		c.sendError(env.ID, protocol.ErrCodeBadIntent, "intent not implemented")
		return
	}
	h(c, env)
}

// sendEvent queues an event for this client. Safe from any goroutine and
// never blocks (see clientConn).
func (c *client) sendEvent(typ, id string, payload any) {
	c.out.sendEvent(typ, id, payload)
}

func (c *client) sendError(id, code, msg string) {
	c.sendEvent(protocol.EventError, id, protocol.ErrorEvent{ID: id, Code: code, Message: msg})
}

// decodeIntent unmarshals env.Data into a T, replying bad_intent on failure.
func decodeIntent[T any](c *client, env protocol.Envelope) (T, bool) {
	var p T
	if err := json.Unmarshal(env.Data, &p); err != nil {
		c.sendError(env.ID, protocol.ErrCodeBadIntent, err.Error())
		return p, false
	}
	return p, true
}

// validSessionID replies bad_intent and returns false when id is unusable.
func (c *client) validSessionID(env protocol.Envelope, id string) bool {
	if err := requireSessionID(id); err != nil {
		c.sendError(env.ID, protocol.ErrCodeBadIntent, err.Error())
		return false
	}
	return true
}
