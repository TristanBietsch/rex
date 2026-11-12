package server

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/pty"
	"github.com/tristanbietsch/rex/internal/daemon/pty/ptytest"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/features/loop"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// registerInput registers a session whose input a test reads from ch.
func registerInput(srv *Server, id string, ch chan []byte) {
	srv.RegisterInputChannel(id, ch)
	srv.RegisterUnitWriter(id, ptytest.ChanWriter(ch))
}

type fakeLoops struct {
	mu     sync.Mutex
	loops  []protocol.LoopSummary
	err    error
	inputs []string
	calls  []string
}

func (f *fakeLoops) rec(s string) (protocol.LoopSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, s)
	return protocol.LoopSummary{ID: "loop-00000001", State: protocol.LoopRunning}, f.err
}
func (f *fakeLoops) Start(p protocol.StartLoop) (protocol.LoopSummary, error) {
	return f.rec("start " + p.Name)
}
func (f *fakeLoops) Pause(id string) (protocol.LoopSummary, error)  { return f.rec("pause " + id) }
func (f *fakeLoops) Resume(id string) (protocol.LoopSummary, error) { return f.rec("resume " + id) }
func (f *fakeLoops) Stop(id string) (protocol.LoopSummary, error)   { return f.rec("stop " + id) }
func (f *fakeLoops) Snapshot() []protocol.LoopSummary {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.loops
}
func (f *fakeLoops) UserInput(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inputs = append(f.inputs, id)
}

func TestLoopIntents_RoundTripAndErrorCodes(t *testing.T) {
	srv, sock, _ := newTestServer(t, state.NewStore())
	fl := &fakeLoops{}
	srv.cfg.Loops = fl
	serveTest(t, srv)
	conn := dialTest(t, sock)
	w, r, _ := helloAs(t, conn, "rex-cli")

	send := func(id, typ string, payload any) protocol.Envelope {
		require.NoError(t, w.WriteIntent(typ, id, payload))
		return waitEvent(t, conn, r, func(e protocol.Envelope) bool { return e.ID == id })
	}
	env := send("a", protocol.IntentStartLoop, protocol.StartLoop{Name: "fix", CWD: "/x"})
	require.Equal(t, protocol.EventLoop, env.Type)
	var sum protocol.LoopSummary
	require.NoError(t, json.Unmarshal(env.Data, &sum))
	require.Equal(t, "loop-00000001", sum.ID)
	for i, typ := range []string{protocol.IntentPauseLoop, protocol.IntentResumeLoop, protocol.IntentStopLoop} {
		var p any
		switch typ {
		case protocol.IntentPauseLoop:
			p = protocol.PauseLoop{LoopID: "loop-00000001"}
		case protocol.IntentResumeLoop:
			p = protocol.ResumeLoop{LoopID: "loop-00000001"}
		default:
			p = protocol.StopLoop{LoopID: "loop-00000001"}
		}
		env = send("c"+string(rune('0'+i)), typ, p)
		require.Equal(t, protocol.EventLoop, env.Type)
	}
	require.Equal(t, []string{"start fix", "pause loop-00000001", "resume loop-00000001", "stop loop-00000001"}, fl.calls)

	// A bad id never reaches the service.
	env = send("bad", protocol.IntentStopLoop, protocol.StopLoop{LoopID: "../../x"})
	require.Equal(t, protocol.EventError, env.Type)
	require.Len(t, fl.calls, 4)

	for _, tc := range []struct {
		err  error
		code string
	}{
		{loop.ErrOff, protocol.ErrCodeBadIntent},
		{&loop.Error{Kind: loop.KindBadInput, Msg: loop.MsgTooMany}, protocol.ErrCodeBadIntent},
		{&loop.Error{Kind: loop.KindNotFound, Msg: "unknown loop"}, protocol.ErrCodeBadIntent},
		{&loop.Error{Kind: loop.KindBadState, Msg: "loop is not paused"}, protocol.ErrCodeBadState},
		{&loop.Error{Kind: loop.KindSpawn, Msg: "too many concurrent sessions (cap=1)", Err: errors.New("x")}, protocol.ErrCodeTooManySessions},
		{&loop.Error{Kind: loop.KindSpawn, Msg: "boom", Err: errors.New("boom")}, protocol.ErrCodeSpawn},
		{&loop.Error{Kind: loop.KindSpawn, Msg: "nope", Err: badIntent(errors.New("nope"))}, protocol.ErrCodeBadIntent},
	} {
		fl.err = tc.err
		env = send("e", protocol.IntentStartLoop, protocol.StartLoop{Name: "x"})
		require.Equal(t, protocol.EventError, env.Type)
		var ee protocol.ErrorEvent
		require.NoError(t, json.Unmarshal(env.Data, &ee))
		require.Equal(t, tc.code, ee.Code, tc.err.Error())
		require.Equal(t, tc.err.Error(), ee.Message)
	}
}

// A daemon without the loop service (an old one answers the same way).
func TestLoopIntents_NotImplementedWithoutService(t *testing.T) {
	srv, sock, _ := newTestServer(t, state.NewStore())
	serveTest(t, srv)
	conn := dialTest(t, sock)
	w, r, _ := helloAs(t, conn, "rex-cli")
	require.NoError(t, w.WriteIntent(protocol.IntentStartLoop, "s", protocol.StartLoop{Name: "x"}))
	env := waitEvent(t, conn, r, func(e protocol.Envelope) bool { return e.ID == "s" })
	var ee protocol.ErrorEvent
	require.NoError(t, json.Unmarshal(env.Data, &ee))
	require.Equal(t, protocol.ErrCodeBadIntent, ee.Code)
	require.Equal(t, "intent not implemented", ee.Message)
}

// A client that joins late gets the loops in its Snapshot, and Loop events
// after that.
func TestHello_SnapshotCarriesLoopsAndBroadcastsChanges(t *testing.T) {
	srv, sock, _ := newTestServer(t, state.NewStore())
	fl := &fakeLoops{loops: []protocol.LoopSummary{{ID: "loop-0000000a", Name: "fix", State: protocol.LoopPaused, PauseReason: protocol.PauseRestart}}}
	srv.cfg.Loops = fl
	serveTest(t, srv)

	conn := dialTest(t, sock)
	_, r, snap := helloAs(t, conn, "rex-tui")
	require.Len(t, snap.Loops, 1)
	require.Equal(t, protocol.PauseRestart, snap.Loops[0].PauseReason)

	srv.PublishLoop(protocol.LoopSummary{ID: "loop-0000000a", State: protocol.LoopRunning})
	env := waitEvent(t, conn, r, func(e protocol.Envelope) bool { return e.Type == protocol.EventLoop })
	var got protocol.LoopSummary
	require.NoError(t, json.Unmarshal(env.Data, &got))
	require.Equal(t, protocol.LoopRunning, got.State)
}

func TestSnapshot_NoLoopsWithoutServiceOrWhileOff(t *testing.T) {
	srv, sock, _ := newTestServer(t, state.NewStore())
	serveTest(t, srv)
	_, _, snap := helloAs(t, dialTest(t, sock), "rex-cli")
	require.Empty(t, snap.Loops)
}

func TestSubscribeLoops_CancelStopsCallbacks(t *testing.T) {
	srv, _, _ := newTestServer(t, state.NewStore())
	var n int
	cancel := srv.SubscribeLoops(func(protocol.LoopSummary) { n++ })
	srv.PublishLoop(protocol.LoopSummary{})
	cancel()
	srv.PublishLoop(protocol.LoopSummary{})
	require.Equal(t, 1, n)
}

// Client keystrokes reach the loop service; the attach nudge does not.
func TestSendInput_TellsLoopsOfUserInputOnly(t *testing.T) {
	srv, sock, _ := newTestServer(t, state.NewStore())
	fl := &fakeLoops{}
	srv.cfg.Loops = fl
	serveTest(t, srv)
	srv.RegisterInputChannel("s1", make(chan []byte, 8))
	conn := dialTest(t, sock)
	w, r, _ := helloAs(t, conn, "rex-tui")

	require.NoError(t, w.WriteIntent(protocol.IntentSendInput, "i1", protocol.SendInput{SessionID: "s1", Bytes: []byte("\f"), Synthetic: true}))
	waitEvent(t, conn, r, func(e protocol.Envelope) bool { return e.ID == "i1" })
	require.NoError(t, w.WriteIntent(protocol.IntentSendInput, "i2", protocol.SendInput{SessionID: "s1", Bytes: []byte("y")}))
	waitEvent(t, conn, r, func(e protocol.Envelope) bool { return e.ID == "i2" })
	require.NoError(t, w.WriteIntent(protocol.IntentReply, "i3", protocol.Reply{SessionID: "s1", Text: "ok"}))
	waitEvent(t, conn, r, func(e protocol.Envelope) bool { return e.ID == "i3" })
	fl.mu.Lock()
	defer fl.mu.Unlock()
	require.Equal(t, []string{"s1", "s1"}, fl.inputs)
}

func TestSendText_AndSetSessionLoop(t *testing.T) {
	store := state.NewStore()
	srv, _, _ := newTestServer(t, store)
	_, err := srv.SendText("nope", "x", nil)
	require.ErrorContains(t, err, "not running")
	ch := make(chan []byte, 4)
	registerInput(srv, "s1", ch)
	sent, err := srv.SendText("s1", "hello", nil)
	require.NoError(t, err)
	require.True(t, sent)
	require.Equal(t, []byte("hello"), <-ch)
	require.Equal(t, []byte{'\r'}, <-ch)

	require.NoError(t, store.Add(&state.Session{ID: "s1", ShortID: "s1", Slug: "s", State: protocol.StateWorking, StartedAt: time.Now()}))
	srv.SetSessionLoop("s1", "loop-0000000a", 3)
	sess, _ := store.Get("s1")
	require.Equal(t, "loop-0000000a", sess.Summary().LoopID)
	require.Equal(t, 3, sess.Summary().LoopIter)
	srv.SetSessionLoop("nope", "loop-0000000a", 1) // unknown session: nothing happens
	require.ErrorContains(t, srv.EnsureLive(t.Context(), "nope"), "not found")
	require.NoError(t, srv.EnsureLive(t.Context(), "s1"), "a live session is left alone")
}

// A client keystroke never lands between a loop's text and its Enter, and a
// send whose guard says the user typed meanwhile sends nothing.
func TestSendText_IsNotInterleavedWithClientInput(t *testing.T) {
	srv, sock, _ := newTestServer(t, state.NewStore())
	fl := &fakeLoops{}
	srv.cfg.Loops = fl
	serveTest(t, srv)
	ch := make(chan []byte, 8)
	registerInput(srv, "s1", ch)
	conn := dialTest(t, sock)
	w, r, _ := helloAs(t, conn, "rex-tui")

	done := make(chan struct{})
	go func() {
		defer close(done)
		sent, err := srv.SendText("s1", "hello", func() bool { return true })
		require.NoError(t, err)
		require.True(t, sent)
	}()
	require.Equal(t, []byte("hello"), <-ch) // text is out; Enter follows after the delay
	require.NoError(t, w.WriteIntent(protocol.IntentSendInput, "k", protocol.SendInput{SessionID: "s1", Bytes: []byte("y")}))
	waitEvent(t, conn, r, func(e protocol.Envelope) bool { return e.ID == "k" })
	<-done
	require.Equal(t, []byte{'\r'}, <-ch, "Enter comes before the client's key")
	require.Equal(t, []byte("y"), <-ch)

	// The guard is evaluated under the lock: false sends nothing.
	sent, err := srv.SendText("s1", "again", func() bool { return false })
	require.NoError(t, err)
	require.False(t, sent)
	require.Empty(t, ch)
	_, err = srv.SendText("nope", "x", nil)
	require.ErrorContains(t, err, "not running")
}

// The guard is asked again after the pause before Enter: if the session
// changed in that gap (a permission prompt appeared), the text stays typed
// and no Enter is written.
func TestSendText_GuardIsCheckedAgainBeforeEnter(t *testing.T) {
	srv, _, _ := newTestServer(t, state.NewStore())
	ch := make(chan []byte, 8)
	registerInput(srv, "s1", ch)

	calls := 0
	sent, err := srv.SendText("s1", "go on", func() bool { calls++; return calls == 1 })
	require.NoError(t, err)
	require.False(t, sent, "the second look said no")
	require.Equal(t, 2, calls, "once before the text, once before Enter")
	require.Equal(t, []byte("go on"), <-ch)
	require.Empty(t, ch, "no Enter was written")

	calls = 0
	sent, err = srv.SendText("s1", "again", func() bool { calls++; return true })
	require.NoError(t, err)
	require.True(t, sent)
	require.Equal(t, []byte("again"), <-ch)
	require.Equal(t, []byte{'\r'}, <-ch)
}

// Raw input (Lua's rex.send) goes through the same lock: it never lands
// between a loop's text and its Enter.
func TestSendRaw_WaitsForALoopSendInProgress(t *testing.T) {
	srv, _, _ := newTestServer(t, state.NewStore())
	ch := make(chan []byte, 8)
	registerInput(srv, "s1", ch)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = srv.SendText("s1", "hello", nil)
	}()
	require.Equal(t, []byte("hello"), <-ch) // in the gap before Enter
	require.NoError(t, srv.SendRaw("s1", []byte("lua"), time.Second))
	<-done
	require.Equal(t, []byte{'\r'}, <-ch, "Enter first")
	require.Equal(t, []byte("lua"), <-ch)

	require.ErrorContains(t, srv.SendRaw("nope", []byte("x"), time.Second), "no input channel")
	full := make(chan []byte)
	registerInput(srv, "s2", full)
	require.ErrorContains(t, srv.SendRaw("s2", []byte("x"), 30*time.Millisecond), "timed out")
}

// With a session's writer registered, every input is one unit on it: a
// Reply and a loop send carry their text and Enter together, client bytes are
// a single part.
func TestInput_GoesThroughTheSessionWriterAsUnits(t *testing.T) {
	srv, sock, _ := newTestServer(t, state.NewStore())
	serveTest(t, srv)
	srv.RegisterInputChannel("s1", make(chan []byte, 1)) // present, but never used
	var mu sync.Mutex
	var units []pty.WriteUnit
	srv.RegisterUnitWriter("s1", pty.UnitWriter(func(_ context.Context, u pty.WriteUnit) (bool, error) {
		mu.Lock()
		defer mu.Unlock()
		units = append(units, u)
		return u.Guard == nil || u.Guard(), nil
	}))
	conn := dialTest(t, sock)
	w, r, _ := helloAs(t, conn, "rex-tui")

	require.NoError(t, w.WriteIntent(protocol.IntentSendInput, "i1", protocol.SendInput{SessionID: "s1", Bytes: []byte("y")}))
	waitEvent(t, conn, r, func(e protocol.Envelope) bool { return e.ID == "i1" })
	require.NoError(t, w.WriteIntent(protocol.IntentReply, "i2", protocol.Reply{SessionID: "s1", Text: "ok"}))
	waitEvent(t, conn, r, func(e protocol.Envelope) bool { return e.ID == "i2" })
	sent, err := srv.SendText("s1", "go on", func() bool { return true })
	require.NoError(t, err)
	require.True(t, sent)
	require.NoError(t, srv.SendRaw("s1", []byte("raw"), time.Second))
	sent, err = srv.SendText("s1", "no", func() bool { return false })
	require.NoError(t, err)
	require.False(t, sent, "the initial guard stops it before any unit")

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, units, 4)
	require.Equal(t, [][]byte{[]byte("y")}, units[0].Parts)
	require.False(t, units[0].Wait)
	require.Equal(t, [][]byte{[]byte("ok"), {'\r'}}, units[1].Parts)
	require.True(t, units[1].Wait)
	require.Equal(t, replySubmitDelay, units[1].Gap)
	require.Equal(t, [][]byte{[]byte("go on"), {'\r'}}, units[2].Parts)
	require.NotNil(t, units[2].Guard, "the guard rides with the unit, for the check before Enter")
	require.Equal(t, [][]byte{[]byte("raw")}, units[3].Parts)

	// A writer that fails reports it to the client.
	srv.RegisterUnitWriter("s1", pty.UnitWriter(func(context.Context, pty.WriteUnit) (bool, error) {
		return false, errors.New("session input is closed")
	}))
	require.NoError(t, w.WriteIntent(protocol.IntentSendInput, "i3", protocol.SendInput{SessionID: "s1", Bytes: []byte("z")}))
	env := waitEvent(t, conn, r, func(e protocol.Envelope) bool { return e.ID == "i3" })
	require.Equal(t, protocol.EventError, env.Type)
}
