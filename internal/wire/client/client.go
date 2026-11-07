// Package client is a Go SDK for rex-daemon's UDS protocol.
//
// A Client owns one connection. One reader goroutine, started by Dial,
// reads every line the daemon sends and demultiplexes it:
//
//   - an event whose envelope ID belongs to a pending Do call is that
//     call's reply and goes only to it;
//   - every other event (broadcasts, replies to intents sent with the
//     fire-and-forget methods or a caller-chosen ID) goes, in arrival
//     order, to the event stream read with NextEvent.
//
// Request/response: Do sends an intent with a fresh ID and waits for its
// terminal reply (see the protocol package doc for the intent-to-reply
// table). The fire-and-forget methods (Rename, SendInput, ...) only write
// the intent, as before acks existed.
//
// Concurrency contract:
//   - Do, HelloContext, Hello and the fire-and-forget methods may be called
//     from any number of goroutines at once. Writes are serialized by a
//     mutex, so intents never interleave on the wire.
//   - NextEvent, NextEventContext and Drain consume one stream and must be
//     called from one goroutine at a time.
//   - Close may be called from any goroutine, more than once. It closes the
//     connection, waits for the reader goroutine to exit and fails every
//     pending Do with an error wrapping ErrClosed.
//
// The event stream is buffered without blocking the reader, so a caller
// that only uses Do never stalls its replies behind unread broadcasts. The
// buffer is capped at MaxQueuedEventBytes; past it the client closes the
// connection (ErrEventQueueFull), as the daemon does to a client that
// stops reading.
package client

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tristanbietsch/rex/internal/version"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Timeouts the client applies when the caller gives none.
const (
	// DefaultDialTimeout bounds Dial.
	DefaultDialTimeout = 2 * time.Second
	// HelloTimeout bounds Hello: a daemon that accepts the connection but
	// never answers cannot hang the caller.
	HelloTimeout = 5 * time.Second
	// WriteTimeout bounds writing one intent to the socket. The daemon
	// reads continuously, so only a wedged daemon makes a write wait.
	WriteTimeout = 10 * time.Second
)

// MaxQueuedEventBytes caps the event payload bytes buffered for NextEvent.
const MaxQueuedEventBytes = 64 << 20

// Errors returned by the client. Match with errors.Is.
var (
	// ErrClosed reports that the connection is closed: by Close, by the
	// daemon, or after a fatal read error. Pending and later Do calls fail
	// with an error wrapping it.
	ErrClosed = errors.New("client: connection closed")
	// ErrEventQueueFull reports that more than MaxQueuedEventBytes of
	// events were waiting for NextEvent; the client closed the connection.
	ErrEventQueueFull = errors.New("client: event queue full; connection closed")
	// ErrNoAcks reports that the daemon predates acks: its Hello Snapshot
	// had no daemon_version, so Do would wait for an Ack that never comes.
	ErrNoAcks = errors.New("rex-daemon is older than rex and does not acknowledge intents; restart it with 'rex daemon restart'")
)

// Error is an Error event the daemon sent in reply to a Do. Match it with
// errors.As; Code is one of the protocol.ErrCode* values.
type Error struct {
	Intent  string // intent type the daemon rejected
	Code    string // protocol error code
	Message string // the daemon's message
}

// Error renders the daemon's message, with guidance for the codes that end
// the connection or mean the daemon is out of date.
func (e *Error) Error() string {
	switch e.Code {
	case protocol.ErrCodeLineTooLong:
		return "rex-daemon closed the connection: a message exceeded the protocol line limit (" + e.Message + ")"
	case protocol.ErrCodeUnsupportedVersion:
		return "rex-daemon speaks a different protocol version and closed the connection; restart it with 'rex daemon restart' (" + e.Message + ")"
	case protocol.ErrCodeInternal:
		return "rex-daemon hit an internal error handling " + e.subject() + " and closed the connection; see 'rex daemon logs'"
	case protocol.ErrCodeBadIntent:
		if e.Message == "intent not implemented" {
			return "rex-daemon is older than rex and does not support " + e.subject() + "; restart it with 'rex daemon restart'"
		}
	}
	return e.Message
}

func (e *Error) subject() string {
	if e.Intent == "" {
		return "the request"
	}
	return e.Intent
}

// fatalCodes are the Error codes the daemon sends right before it closes
// the connection.
func isFatalCode(code string) bool {
	return code == protocol.ErrCodeLineTooLong || code == protocol.ErrCodeUnsupportedVersion
}

// result is what the reader hands a Do waiter.
type result struct {
	env protocol.Envelope
	err error
}

// item is one entry of the event stream: an event, or a non-fatal read
// error (one malformed line) reported in its place.
type item struct {
	env protocol.Envelope
	err error
}

// Client wraps a UDS connection to rex-daemon. See the package doc for the
// concurrency contract.
type Client struct {
	conn net.Conn

	wmu sync.Mutex // serializes writes; guards w
	w   *protocol.Writer

	idPrefix  string // unique per Client, so Do IDs never collide with caller IDs
	nextID    atomic.Uint64
	maxQueued int // event-stream cap in payload bytes (MaxQueuedEventBytes)

	done   chan struct{} // closed when the reader goroutine exits
	notify chan struct{} // cap 1; signals NextEvent that the stream changed

	mu            sync.Mutex
	pending       map[string]chan result // Do waiters by envelope ID
	queue         []item                 // event stream, oldest first
	queuedBytes   int
	readErr       error  // why the reader stopped; nil while it runs
	fatal         *Error // a connection-ending Error the daemon sent
	closed        bool   // Close was called
	readDeadline  time.Time
	daemonVersion string
	helloDone     bool
}

// Dial connects to the daemon's socket, bounded by DefaultDialTimeout.
func Dial(socket string) (*Client, error) {
	return DialTimeout(socket, DefaultDialTimeout)
}

// DialTimeout connects to the daemon's socket, bounded by timeout.
func DialTimeout(socket string, timeout time.Duration) (*Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return DialContext(ctx, socket)
}

// DialContext connects to the daemon's socket, honoring ctx's deadline and
// cancellation, and starts the reader goroutine. The error is the dialer's
// own (for example "dial unix <path>: connect: no such file or
// directory"), unwrapped, so the path appears once.
func DialContext(ctx context.Context, socket string) (*Client, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", socket)
	if err != nil {
		// Not wrapped: the *net.OpError already names the socket (F-04).
		return nil, err
	}
	return newClient(conn), nil
}

// newClient wraps conn and starts its reader goroutine.
func newClient(conn net.Conn) *Client {
	c := &Client{
		conn:      conn,
		w:         protocol.NewWriter(conn),
		idPrefix:  "c" + randomHex(4) + "-",
		maxQueued: MaxQueuedEventBytes,
		done:      make(chan struct{}),
		notify:    make(chan struct{}, 1),
		pending:   make(map[string]chan result),
	}
	go c.readLoop(protocol.NewReader(conn))
	return c
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b) // crypto/rand.Read never fails on supported platforms
	return hex.EncodeToString(b)
}

// Close closes the connection and waits for the reader goroutine to exit.
// Pending Do calls fail with an error wrapping ErrClosed; NextEvent then
// returns an error wrapping both ErrClosed and net.ErrClosed. It returns
// the connection's Close error.
func (c *Client) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	err := c.conn.Close()
	<-c.done
	c.signal()
	return err
}

// DaemonVersion returns the daemon's release label from the last Hello
// Snapshot: "" before Hello and from daemons that predate acks.
func (c *Client) DaemonVersion() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.daemonVersion
}

// SetReadDeadline sets the deadline for NextEvent and for Hello. The zero
// time clears it. It no longer touches the socket (one goroutine reads it
// for everyone); prefer NextEventContext and HelloContext. It always
// returns nil.
func (c *Client) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	c.readDeadline = t
	c.mu.Unlock()
	c.signal() // a NextEvent already waiting re-arms with the new deadline
	return nil
}

// Hello performs the handshake: it sends Hello with this build's version
// and clientName, and returns the daemon's Snapshot. It is bounded by
// HelloTimeout and by any SetReadDeadline deadline.
func (c *Client) Hello(clientName string) (*protocol.Snapshot, error) {
	ctx, cancel := context.WithTimeout(context.Background(), HelloTimeout)
	defer cancel()
	c.mu.Lock()
	dl := c.readDeadline
	c.mu.Unlock()
	if !dl.IsZero() {
		var cancelDL context.CancelFunc
		ctx, cancelDL = context.WithDeadline(ctx, dl)
		defer cancelDL()
	}
	return c.HelloContext(ctx, clientName)
}

// HelloContext is Hello bounded by ctx instead of HelloTimeout. After it
// returns, the daemon broadcasts board events to the event stream and
// DaemonVersion reports the daemon's version.
func (c *Client) HelloContext(ctx context.Context, clientName string) (*protocol.Snapshot, error) {
	env, err := c.do(ctx, protocol.IntentHello, protocol.Hello{ClientVersion: version.Version, Client: clientName})
	if err != nil {
		return nil, err
	}
	if env.Type != protocol.EventSnapshot {
		return nil, fmt.Errorf("expected Snapshot, got %s/%s", env.Kind, env.Type)
	}
	var snap protocol.Snapshot
	if err := json.Unmarshal(env.Data, &snap); err != nil {
		return nil, fmt.Errorf("decode snapshot: %w", err)
	}
	c.mu.Lock()
	c.daemonVersion = snap.DaemonVersion
	c.helloDone = true
	c.mu.Unlock()
	return &snap, nil
}

// Do sends intentType with payload under a fresh envelope ID and waits for
// the daemon's terminal reply with that ID, honoring ctx's deadline and
// cancellation. It returns the reply envelope (an Ack, or the intent's
// specific event such as Hello's Snapshot). An Error reply is returned as
// a *Error. A connection that closes first fails with an error wrapping
// ErrClosed (or the *Error the daemon closed it with). After a Hello that
// showed the daemon predates acks, Do fails at once with ErrNoAcks.
//
// A reply that arrives after ctx ended is dropped, never delivered to the
// event stream. Do is safe for concurrent use.
func (c *Client) Do(ctx context.Context, intentType string, payload any) (protocol.Envelope, error) {
	c.mu.Lock()
	legacy := c.helloDone && c.daemonVersion == ""
	c.mu.Unlock()
	if legacy {
		return protocol.Envelope{}, fmt.Errorf("%s: %w", intentType, ErrNoAcks)
	}
	return c.do(ctx, intentType, payload)
}

func (c *Client) do(ctx context.Context, intentType string, payload any) (protocol.Envelope, error) {
	id := c.idPrefix + strconv.FormatUint(c.nextID.Add(1), 10)
	ch := make(chan result, 1)
	c.mu.Lock()
	if err := c.stoppedErrLocked(); err != nil {
		c.mu.Unlock()
		return protocol.Envelope{}, err
	}
	c.pending[id] = ch
	c.mu.Unlock()

	if err := c.write(ctx, intentType, id, payload); err != nil {
		c.forget(id)
		return protocol.Envelope{}, err
	}
	select {
	case r := <-ch:
		if r.err != nil {
			return protocol.Envelope{}, r.err
		}
		if r.env.Type == protocol.EventError {
			var ee protocol.ErrorEvent
			if err := json.Unmarshal(r.env.Data, &ee); err != nil {
				return protocol.Envelope{}, fmt.Errorf("%s: decode error reply: %w", intentType, err)
			}
			return protocol.Envelope{}, &Error{Intent: intentType, Code: ee.Code, Message: ee.Message}
		}
		return r.env, nil
	case <-ctx.Done():
		c.forget(id)
		return protocol.Envelope{}, fmt.Errorf("%s: waiting for rex-daemon: %w", intentType, ctx.Err())
	}
}

// forget drops a waiter that will not read its channel.
func (c *Client) forget(id string) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

// stoppedErrLocked is the error new and pending Do calls get once the
// reader has stopped, or nil while it runs. c.mu must be held.
func (c *Client) stoppedErrLocked() error {
	switch {
	case c.closed:
		return ErrClosed
	case c.readErr == nil:
		return nil
	case c.fatal != nil:
		return fmt.Errorf("%w: %w", ErrClosed, c.fatal)
	default:
		return fmt.Errorf("%w: %w", ErrClosed, c.readErr)
	}
}

// write sends one intent. It holds the write mutex for the whole line and
// bounds the write by ctx's deadline and WriteTimeout.
func (c *Client) write(ctx context.Context, typ, id string, payload any) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%s: %w", typ, err)
	}
	deadline := time.Now().Add(WriteTimeout)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if err := c.conn.SetWriteDeadline(deadline); err != nil {
		return fmt.Errorf("%s: %w", typ, err)
	}
	return c.w.WriteIntent(typ, id, payload)
}

// send writes an intent without waiting for any reply.
func (c *Client) send(typ, id string, payload any) error {
	return c.write(context.Background(), typ, id, payload)
}

// readLoop is the connection's only reader. It routes each envelope (see
// the package doc) until the connection fails, then records why and fails
// every pending Do.
func (c *Client) readLoop(r *protocol.Reader) {
	defer close(c.done)
	for {
		env, err := r.Read()
		if err != nil {
			if errors.Is(err, protocol.ErrMalformed) || errors.Is(err, protocol.ErrUnsupportedVersion) {
				// One bad line, consumed whole: report it in the stream
				// and keep reading.
				c.push(item{err: err}, 0)
				continue
			}
			c.stop(err)
			return
		}
		c.route(env)
	}
}

// route delivers one envelope to its Do waiter or the event stream.
func (c *Client) route(env protocol.Envelope) {
	c.mu.Lock()
	if env.ID != "" {
		if ch, ok := c.pending[env.ID]; ok {
			delete(c.pending, env.ID)
			c.mu.Unlock()
			ch <- result{env: env} // buffered: never blocks
			return
		}
		if strings.HasPrefix(env.ID, c.idPrefix) {
			c.mu.Unlock()
			return // late reply to a Do whose ctx ended: drop it
		}
	}
	if env.Type == protocol.EventError && env.ID == "" {
		var ee protocol.ErrorEvent
		if json.Unmarshal(env.Data, &ee) == nil && isFatalCode(ee.Code) {
			c.fatal = &Error{Code: ee.Code, Message: ee.Message}
		}
	}
	c.mu.Unlock()
	c.push(item{env: env}, len(env.Data))
}

// push appends to the event stream. Over MaxQueuedEventBytes it closes
// the connection instead; the reader then stops with ErrEventQueueFull.
func (c *Client) push(it item, size int) {
	c.mu.Lock()
	if c.queuedBytes+size > c.maxQueued {
		c.mu.Unlock()
		c.stop(fmt.Errorf("%w (%d bytes waiting): %w", ErrEventQueueFull, c.queuedBytes, net.ErrClosed))
		_ = c.conn.Close()
		return
	}
	c.queue = append(c.queue, it)
	c.queuedBytes += size
	c.mu.Unlock()
	c.signal()
}

// stop records why reading ended (the first cause wins) and fails every
// pending Do. The connection is closed so later writes fail fast.
func (c *Client) stop(cause error) {
	c.mu.Lock()
	if c.readErr == nil {
		if c.closed {
			cause = fmt.Errorf("%w: %w", ErrClosed, net.ErrClosed)
		}
		c.readErr = connClosedErr(cause)
	}
	waitErr := c.stoppedErrLocked()
	pending := c.pending
	c.pending = make(map[string]chan result)
	c.mu.Unlock()
	for _, ch := range pending {
		ch <- result{err: waitErr}
	}
	_ = c.conn.Close()
	c.signal()
}

// connClosedErr makes a terminal read error recognizable as a closed
// connection. EOF and socket errors already are; anything else (a line
// over the protocol limit) is wrapped with net.ErrClosed, because the
// client closes the connection on it and the stream will yield nothing
// more.
func connClosedErr(err error) error {
	var opErr *net.OpError
	switch {
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF),
		errors.Is(err, net.ErrClosed), errors.As(err, &opErr):
		return err
	default:
		return fmt.Errorf("%w (connection closed: %w)", err, net.ErrClosed)
	}
}

func (c *Client) signal() {
	select {
	case c.notify <- struct{}{}:
	default:
	}
}

// NextEvent returns the next event in the stream, waiting as long as
// needed or until the SetReadDeadline deadline, which fails with an error
// wrapping os.ErrDeadlineExceeded ("i/o timeout"). Events that arrived
// before the connection ended are returned first; after that it returns
// the reason reading stopped: io.EOF when the daemon closed cleanly, an
// error wrapping net.ErrClosed after Close. A malformed line yields its
// error once, and the stream continues.
func (c *Client) NextEvent() (protocol.Envelope, error) {
	return c.NextEventContext(context.Background())
}

// NextEventContext is NextEvent that also returns when ctx ends, with
// ctx's error.
func (c *Client) NextEventContext(ctx context.Context) (protocol.Envelope, error) {
	for {
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return protocol.Envelope{}, fmt.Errorf("%w: %w", ErrClosed, net.ErrClosed)
		}
		if len(c.queue) > 0 {
			it := c.queue[0]
			c.queue[0] = item{}
			c.queue = c.queue[1:]
			c.queuedBytes -= len(it.env.Data)
			c.mu.Unlock()
			return it.env, it.err
		}
		if c.readErr != nil {
			err := c.readErr
			c.mu.Unlock()
			return protocol.Envelope{}, err
		}
		deadline := c.readDeadline
		c.mu.Unlock()

		if err := c.wait(ctx, deadline); err != nil {
			return protocol.Envelope{}, err
		}
	}
}

// wait blocks until the stream may have changed, ctx ends or deadline
// (zero = none) passes.
func (c *Client) wait(ctx context.Context, deadline time.Time) error {
	var expired <-chan time.Time
	if !deadline.IsZero() {
		d := time.Until(deadline)
		if d <= 0 {
			return fmt.Errorf("client: read event: %w", os.ErrDeadlineExceeded)
		}
		t := time.NewTimer(d)
		defer t.Stop()
		expired = t.C
	}
	select {
	case <-c.notify:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-expired:
		return fmt.Errorf("client: read event: %w", os.ErrDeadlineExceeded)
	}
}

// Drain reads events until EOF or the handler returns false.
func (c *Client) Drain(handler func(protocol.Envelope) bool) error {
	for {
		env, err := c.NextEvent()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if !handler(env) {
			return nil
		}
	}
}

// The methods below write one intent and return without waiting for a
// reply (the daemon sends none when the ID is empty; an Error, if any,
// arrives in the event stream). Use Do to learn the outcome.

// Subscribe pins this connection to a session's output stream. Use sessionID="" for board-wide only.
func (c *Client) Subscribe(sessionID string) error {
	return c.send(protocol.IntentSubscribe, "", protocol.Subscribe{SessionID: sessionID})
}

// SubscribeReplay subscribes and also requests the recent transcript tail as a SessionOutput event.
func (c *Client) SubscribeReplay(sessionID string) error {
	return c.send(protocol.IntentSubscribe, "", protocol.Subscribe{SessionID: sessionID, Replay: true})
}

// SubscribeOpts subscribes with explicit replay / mode-sync options.
func (c *Client) SubscribeOpts(sessionID string, replay, modeSync bool) error {
	return c.send(protocol.IntentSubscribe, "", protocol.Subscribe{SessionID: sessionID, Replay: replay, ModeSync: modeSync})
}

// Resize tells the daemon to resize the session's PTY.
func (c *Client) Resize(sessionID string, cols, rows uint16) error {
	return c.send(protocol.IntentResize, "", protocol.Resize{SessionID: sessionID, Cols: cols, Rows: rows})
}

// NewSession submits a NewSession intent.
func (c *Client) NewSession(req protocol.NewSession) error {
	return c.send(protocol.IntentNewSession, "", req)
}

// SendInput forwards raw bytes to the session's PTY.
func (c *Client) SendInput(sessionID string, b []byte) error {
	return c.send(protocol.IntentSendInput, "", protocol.SendInput{SessionID: sessionID, Bytes: b})
}

// SendRedraw sends a ^L redraw nudge flagged synthetic, so it doesn't count
// as user input (it must not cancel a pending retire).
func (c *Client) SendRedraw(sessionID string) error {
	return c.send(protocol.IntentSendInput, "", protocol.SendInput{SessionID: sessionID, Bytes: []byte{0x0c}, Synthetic: true})
}

// Reply sends an inline text reply.
func (c *Client) Reply(sessionID, text string) error {
	return c.send(protocol.IntentReply, "", protocol.Reply{SessionID: sessionID, Text: text})
}

// Rename changes a session's slug or title. reqID is echoed on the reply
// (an Error such as slug already in use, or an Ack from a current daemon),
// which arrives in the event stream.
func (c *Client) Rename(sessionID, slug, title, reqID string) error {
	return c.send(protocol.IntentRename, reqID, protocol.Rename{SessionID: sessionID, Slug: slug, Title: title})
}

// SetSessionName sets (or, with name == "", clears) a session's display
// name. reqID is echoed on the reply, which arrives in the event stream.
func (c *Client) SetSessionName(sessionID, name, reqID string) error {
	return c.send(protocol.IntentSetSessionName, reqID, protocol.SetSessionName{SessionID: sessionID, Name: name})
}

// Retire marks a session done: it is cleared after ttl (0 = daemon default)
// unless unmarked or typed into first. reqID is echoed on the reply.
func (c *Client) Retire(sessionID string, ttl time.Duration, reqID string) error {
	return c.send(protocol.IntentRetire, reqID, protocol.Retire{SessionID: sessionID, TTLSeconds: int64(ttl / time.Second)})
}

// Unretire cancels a pending Retire. reqID is echoed on the reply.
func (c *Client) Unretire(sessionID, reqID string) error {
	return c.send(protocol.IntentUnretire, reqID, protocol.Unretire{SessionID: sessionID})
}

// Delete removes a session.
func (c *Client) Delete(sessionID string) error {
	return c.send(protocol.IntentDelete, "", protocol.Delete{SessionID: sessionID})
}

// Complete asks the daemon to cleanly terminate a session and mark it done.
func (c *Client) Complete(sessionID string) error {
	return c.send(protocol.IntentComplete, "", protocol.Complete{SessionID: sessionID})
}

// Shutdown asks the daemon to stop (the socket form of SIGTERM).
func (c *Client) Shutdown() error {
	return c.send(protocol.IntentShutdown, "", struct{}{})
}

// Reload asks the daemon to re-read tools.yaml (the socket form of SIGHUP).
// Use Do(ctx, protocol.IntentReload, struct{}{}) to learn whether it worked.
func (c *Client) Reload() error {
	return c.send(protocol.IntentReload, "", struct{}{})
}

// FocusFilter sets the cosmetic tool filter.
func (c *Client) FocusFilter(toolID string) error {
	return c.send(protocol.IntentFocusFilter, "", protocol.FocusFilter{ToolID: toolID})
}

// SetMaxConcurrent updates the daemon's concurrent-session cap. n <= 0 means uncapped.
func (c *Client) SetMaxConcurrent(n int) error {
	return c.send(protocol.IntentSetMaxConcurrent, "", protocol.SetMaxConcurrent{N: n})
}

// SetSessionFleet updates the fleet label of an existing session.
// An empty fleet string clears the fleet.
func (c *Client) SetSessionFleet(sessionID, fleet string) error {
	return c.send(protocol.IntentSetSessionFleet, "", protocol.SetSessionFleet{SessionID: sessionID, Fleet: fleet})
}
