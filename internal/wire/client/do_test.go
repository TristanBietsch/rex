package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/version"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// fakeDaemon is the daemon end of a net.Pipe: tests read the intents a
// Client writes and script the events it gets back.
type fakeDaemon struct {
	conn net.Conn
	r    *protocol.Reader
	w    *protocol.Writer
	wmu  sync.Mutex
}

// pipeClient returns a Client wired to a fakeDaemon. Both ends close when
// the test ends.
func pipeClient(t *testing.T) (*Client, *fakeDaemon) {
	t.Helper()
	a, b := net.Pipe()
	c := newClient(a)
	fd := &fakeDaemon{conn: b, r: protocol.NewReader(b), w: protocol.NewWriter(b)}
	t.Cleanup(func() {
		_ = b.Close()
		_ = c.Close()
	})
	return c, fd
}

func (fd *fakeDaemon) read(t *testing.T) protocol.Envelope {
	t.Helper()
	env, err := fd.r.Read()
	require.NoError(t, err)
	require.Equal(t, protocol.KindIntent, env.Kind)
	return env
}

func (fd *fakeDaemon) event(t *testing.T, typ, id string, payload any) {
	t.Helper()
	fd.wmu.Lock()
	defer fd.wmu.Unlock()
	require.NoError(t, fd.w.WriteEvent(typ, id, payload))
}

func (fd *fakeDaemon) ack(t *testing.T, env protocol.Envelope, sessionID string) {
	t.Helper()
	fd.event(t, protocol.EventAck, env.ID, protocol.Ack{Intent: env.Type, SessionID: sessionID})
}

type doResult struct {
	env protocol.Envelope
	err error
}

func goDo(c *Client, ctx context.Context, typ string, payload any) <-chan doResult {
	ch := make(chan doResult, 1)
	go func() {
		env, err := c.Do(ctx, typ, payload)
		ch <- doResult{env, err}
	}()
	return ch
}

func TestDo_Ack(t *testing.T) {
	c, fd := pipeClient(t)
	res := goDo(c, context.Background(), protocol.IntentRename, protocol.Rename{SessionID: "s1", Title: "t"})

	env := fd.read(t)
	require.Equal(t, protocol.IntentRename, env.Type)
	require.NotEmpty(t, env.ID, "Do assigns an ID")
	fd.ack(t, env, "s1")

	r := <-res
	require.NoError(t, r.err)
	require.Equal(t, protocol.EventAck, r.env.Type)
	require.Equal(t, env.ID, r.env.ID)
	var a protocol.Ack
	require.NoError(t, json.Unmarshal(r.env.Data, &a))
	require.Equal(t, protocol.Ack{Intent: protocol.IntentRename, SessionID: "s1"}, a)
}

func TestDo_ErrorReplyIsTyped(t *testing.T) {
	c, fd := pipeClient(t)
	cases := []struct {
		code, msg string
		contains  string
	}{
		{protocol.ErrCodeBadIntent, `slug "x" already in use by abcd`, `slug "x" already in use by abcd`},
		{protocol.ErrCodeUnknownSession, "session not found", "session not found"},
		{protocol.ErrCodeBadIntent, "intent not implemented", "older than rex"},
		{protocol.ErrCodeInternal, "internal error", "internal error handling Rename"},
		{protocol.ErrCodeUnsupportedVersion, "v 2", "different protocol version"},
		{protocol.ErrCodeLineTooLong, "limit 16 bytes", "line limit"},
	}
	for _, tc := range cases {
		res := goDo(c, context.Background(), protocol.IntentRename, protocol.Rename{SessionID: "s1"})
		env := fd.read(t)
		fd.event(t, protocol.EventError, env.ID, protocol.ErrorEvent{ID: env.ID, Code: tc.code, Message: tc.msg})
		r := <-res
		var de *Error
		require.True(t, errors.As(r.err, &de), "%v", r.err)
		require.Equal(t, &Error{Intent: protocol.IntentRename, Code: tc.code, Message: tc.msg}, de)
		require.Contains(t, de.Error(), tc.contains)
	}
	require.Contains(t, (&Error{Code: protocol.ErrCodeInternal}).Error(), "the request")
}

// Do honors ctx: it returns when the deadline passes, and a reply that
// arrives afterwards is dropped rather than leaking into the event stream.
func TestDo_Timeout(t *testing.T) {
	c, fd := pipeClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	res := goDo(c, ctx, protocol.IntentReload, struct{}{})
	env := fd.read(t)

	r := <-res
	require.ErrorIs(t, r.err, context.DeadlineExceeded)
	require.Contains(t, r.err.Error(), "Reload")

	fd.ack(t, env, "") // too late
	fd.event(t, protocol.EventSessionRemoved, "", protocol.SessionRemoved{SessionID: "after"})
	got, err := c.NextEvent()
	require.NoError(t, err)
	require.Equal(t, protocol.EventSessionRemoved, got.Type, "the late Ack was dropped")

	// A canceled ctx fails before anything is written.
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	_, err = c.Do(ctx, protocol.IntentReload, struct{}{})
	require.ErrorIs(t, err, context.Canceled)
}

// Concurrent Do calls each get their own reply while broadcasts, sent in
// between and in a different order, reach the event stream in order and
// without any reply mixed in.
func TestDo_ConcurrentWithInterleavedBroadcasts(t *testing.T) {
	c, fd := pipeClient(t)
	const n = 40

	results := make([]<-chan doResult, n)
	for i := range n {
		results[i] = goDo(c, context.Background(), protocol.IntentSendInput,
			protocol.SendInput{SessionID: fmt.Sprintf("s%d", i)})
	}

	// The daemon reads every intent, then answers in reverse order with a
	// broadcast before each reply.
	served := make(chan struct{})
	go func() {
		defer close(served)
		var envs []protocol.Envelope
		for range n {
			envs = append(envs, fd.read(t))
		}
		for i := n - 1; i >= 0; i-- {
			fd.event(t, protocol.EventSessionUpdated, "", protocol.SessionUpdated{
				SessionID: "board", Patch: map[string]any{"seq": float64(n - 1 - i)},
			})
			var p protocol.SendInput
			require.NoError(t, json.Unmarshal(envs[i].Data, &p))
			fd.ack(t, envs[i], p.SessionID)
		}
	}()

	var stream []protocol.Envelope
	streamDone := make(chan struct{})
	go func() {
		defer close(streamDone)
		for range n {
			env, err := c.NextEvent()
			if err != nil {
				return
			}
			stream = append(stream, env)
		}
	}()

	for i := range n {
		r := <-results[i]
		require.NoError(t, r.err)
		var a protocol.Ack
		require.NoError(t, json.Unmarshal(r.env.Data, &a))
		require.Equal(t, fmt.Sprintf("s%d", i), a.SessionID, "reply %d went to the wrong caller", i)
	}
	<-served
	<-streamDone
	require.Len(t, stream, n)
	for i, env := range stream {
		require.Equal(t, protocol.EventSessionUpdated, env.Type, "only broadcasts reach the stream")
		var u protocol.SessionUpdated
		require.NoError(t, json.Unmarshal(env.Data, &u))
		require.Equal(t, float64(i), u.Patch["seq"], "broadcasts keep their order")
	}
}

// Close fails every pending Do with ErrClosed and ends the event stream.
func TestClose_UnblocksWaiters(t *testing.T) {
	c, fd := pipeClient(t)
	var results []<-chan doResult
	for range 3 {
		results = append(results, goDo(c, context.Background(), protocol.IntentReload, struct{}{}))
		fd.read(t)
	}
	streamErr := make(chan error, 1)
	go func() {
		_, err := c.NextEvent()
		streamErr <- err
	}()

	require.NoError(t, c.Close())
	for _, res := range results {
		select {
		case r := <-res:
			require.ErrorIs(t, r.err, ErrClosed)
		case <-time.After(2 * time.Second):
			t.Fatal("Close did not unblock a pending Do")
		}
	}
	err := <-streamErr
	require.ErrorIs(t, err, net.ErrClosed, "the TUI treats this as a dead connection")
	require.ErrorIs(t, err, ErrClosed)

	_, err = c.Do(context.Background(), protocol.IntentReload, struct{}{})
	require.ErrorIs(t, err, ErrClosed)
}

// When the daemon hangs up, events it sent first are still delivered, then
// io.EOF; pending Do calls fail with ErrClosed.
func TestDaemonEOF_DrainsThenEOF(t *testing.T) {
	c, fd := pipeClient(t)
	res := goDo(c, context.Background(), protocol.IntentReload, struct{}{})
	fd.read(t)
	fd.event(t, protocol.EventSessionRemoved, "", protocol.SessionRemoved{SessionID: "x"})
	require.NoError(t, fd.conn.Close())

	r := <-res
	require.ErrorIs(t, r.err, ErrClosed)
	require.ErrorIs(t, r.err, io.EOF)

	env, err := c.NextEvent()
	require.NoError(t, err)
	require.Equal(t, protocol.EventSessionRemoved, env.Type)
	_, err = c.NextEvent()
	require.ErrorIs(t, err, io.EOF)
	require.NoError(t, c.Drain(func(protocol.Envelope) bool { return true }), "Drain ends cleanly at EOF")
}

// The daemon reports a protocol violation with an ID-less Error and then
// closes: pending Do calls get that Error, not a bare EOF.
func TestFatalErrorThenClose(t *testing.T) {
	c, fd := pipeClient(t)
	res := goDo(c, context.Background(), protocol.IntentReload, struct{}{})
	fd.read(t)
	fd.event(t, protocol.EventError, "", protocol.ErrorEvent{Code: protocol.ErrCodeUnsupportedVersion, Message: "v 9"})
	require.NoError(t, fd.conn.Close())

	r := <-res
	var de *Error
	require.True(t, errors.As(r.err, &de), "%v", r.err)
	require.Equal(t, protocol.ErrCodeUnsupportedVersion, de.Code)
	require.ErrorIs(t, r.err, ErrClosed)

	env, err := c.NextEvent()
	require.NoError(t, err)
	require.Equal(t, protocol.EventError, env.Type, "the Error is in the stream too")
}

func TestHello_SendsVersionAndRecordsDaemonVersion(t *testing.T) {
	c, fd := pipeClient(t)
	require.Empty(t, c.DaemonVersion())
	done := make(chan error, 1)
	go func() {
		snap, err := c.Hello("rex-test")
		if err == nil && snap.Filter != "all" {
			err = fmt.Errorf("filter %q", snap.Filter)
		}
		done <- err
	}()
	env := fd.read(t)
	require.Equal(t, protocol.IntentHello, env.Type)
	var h protocol.Hello
	require.NoError(t, json.Unmarshal(env.Data, &h))
	require.Equal(t, protocol.Hello{ClientVersion: version.Version, Client: "rex-test"}, h)
	fd.event(t, protocol.EventSnapshot, env.ID, protocol.Snapshot{Filter: "all", DaemonVersion: "v9"})
	require.NoError(t, <-done)
	require.Equal(t, "v9", c.DaemonVersion())
}

// A daemon that predates acks answers Hello with no daemon_version. Do then
// fails at once instead of waiting for an Ack that never comes.
func TestDo_DaemonWithoutAcks(t *testing.T) {
	c, fd := pipeClient(t)
	done := make(chan error, 1)
	go func() { _, err := c.Hello("rex-test"); done <- err }()
	env := fd.read(t)
	fd.event(t, protocol.EventSnapshot, env.ID, protocol.Snapshot{Filter: "all"})
	require.NoError(t, <-done)

	_, err := c.Do(context.Background(), protocol.IntentReload, struct{}{})
	require.ErrorIs(t, err, ErrNoAcks)
}

func TestHello_Errors(t *testing.T) {
	c, fd := pipeClient(t)

	// Not a Snapshot.
	done := make(chan error, 1)
	go func() { _, err := c.Hello("x"); done <- err }()
	env := fd.read(t)
	fd.event(t, protocol.EventAck, env.ID, protocol.Ack{Intent: protocol.IntentHello})
	require.ErrorContains(t, <-done, "expected Snapshot")

	// A daemon that never answers: Hello gives up at the read deadline.
	require.NoError(t, c.SetReadDeadline(time.Now().Add(50*time.Millisecond)))
	go func() { _, err := c.Hello("x"); done <- err }()
	fd.read(t)
	require.ErrorIs(t, <-done, context.DeadlineExceeded)
	require.NoError(t, c.SetReadDeadline(time.Time{}))

	// HelloContext honors its ctx.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	go func() { _, err := c.HelloContext(ctx, "x"); done <- err }()
	fd.read(t)
	require.ErrorIs(t, <-done, context.DeadlineExceeded)
}

// SetReadDeadline still bounds NextEvent, with the "i/o timeout" error the
// CLI's wait and ack loops look for.
func TestNextEvent_DeadlineAndContext(t *testing.T) {
	c, fd := pipeClient(t)
	require.NoError(t, c.SetReadDeadline(time.Now().Add(30*time.Millisecond)))
	_, err := c.NextEvent()
	require.ErrorIs(t, err, os.ErrDeadlineExceeded)
	require.Contains(t, err.Error(), "i/o timeout")
	_, err = c.NextEvent()
	require.ErrorIs(t, err, os.ErrDeadlineExceeded, "an expired deadline fails at once")

	// Clearing it lets events through again, and queued events come first.
	require.NoError(t, c.SetReadDeadline(time.Time{}))
	fd.event(t, protocol.EventSummarizerHealth, "", protocol.SummarizerHealth{Available: true})
	env, err := c.NextEvent()
	require.NoError(t, err)
	require.Equal(t, protocol.EventSummarizerHealth, env.Type)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = c.NextEventContext(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

// A malformed line is reported once and the stream continues.
func TestNextEvent_MalformedLineIsNotFatal(t *testing.T) {
	c, fd := pipeClient(t)
	go func() {
		_, _ = fd.conn.Write([]byte("not json\n"))
		fd.event(t, protocol.EventSummarizerHealth, "", protocol.SummarizerHealth{})
	}()
	_, err := c.NextEvent()
	require.ErrorIs(t, err, protocol.ErrMalformed)
	env, err := c.NextEvent()
	require.NoError(t, err)
	require.Equal(t, protocol.EventSummarizerHealth, env.Type)
}

// Unread events are capped: past the cap the client closes the connection
// rather than buffer without bound, and pending Do calls fail.
func TestEventQueue_Overflow(t *testing.T) {
	c, fd := pipeClient(t)
	c.mu.Lock()
	c.maxQueued = 64
	c.mu.Unlock()
	res := goDo(c, context.Background(), protocol.IntentReload, struct{}{})
	fd.read(t)
	go func() {
		for range 10 {
			if fd.w.WriteEvent(protocol.EventSessionOutput, "", protocol.SessionOutput{SessionID: "s", Bytes: []byte("0123456789")}) != nil {
				return
			}
		}
	}()
	r := <-res
	require.ErrorIs(t, r.err, ErrEventQueueFull)
	var err error
	for err == nil {
		_, err = c.NextEvent()
	}
	require.ErrorIs(t, err, ErrEventQueueFull)
	require.ErrorIs(t, err, net.ErrClosed, "the TUI reconnects instead of spinning")
}

func TestConnClosedErr(t *testing.T) {
	tooLong := fmt.Errorf("%w (limit 1 bytes)", protocol.ErrLineTooLong)
	got := connClosedErr(tooLong)
	require.ErrorIs(t, got, protocol.ErrLineTooLong)
	require.ErrorIs(t, got, net.ErrClosed)
	for _, err := range []error{io.EOF, io.ErrUnexpectedEOF, net.ErrClosed, &net.OpError{Op: "read", Err: errors.New("reset")}} {
		require.Equal(t, err, connClosedErr(err), "returned as is")
	}
}

// F-04: the dial error names the socket once ("dial unix <p>: connect:
// ..."), not twice.
func TestDial_ErrorNamesSocketOnce(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "none.sock")
	_, err := Dial(sock)
	require.Error(t, err)
	require.Equal(t, 1, strings.Count(err.Error(), sock), err.Error())
	require.True(t, strings.HasPrefix(err.Error(), "dial unix "), err.Error())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = DialContext(ctx, sock)
	require.Error(t, err)
}
