package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/version"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// ackEnv is a server with every hook wired and sessions in each state the
// intents need: s1 running (input and resize registered), s2 and s3 done.
type ackEnv struct {
	srv       *Server
	sock, dir string
	shutdowns atomic.Int32
	failNext  atomic.Bool // the next Reload fails
}

func startAckServer(t *testing.T) *ackEnv {
	t.Helper()
	dir, err := os.MkdirTemp("", "rex-ack")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	store := state.NewStore()
	for _, s := range []*state.Session{liveSession("s1"), doneSession("s2"), doneSession("s3")} {
		require.NoError(t, state.WriteMeta(dir, s))
		require.NoError(t, store.Add(s))
	}
	reg, err := registry.Load("")
	require.NoError(t, err)
	e := &ackEnv{sock: filepath.Join(dir, "rex.sock"), dir: dir}
	e.srv, err = New(Config{
		Socket: e.sock, StateDir: dir, Registry: reg, Store: store,
		// Tests claim retires themselves; the sweep must not race them.
		RetireSweepInterval: time.Hour,
		// Counted, not acted on: the connection must outlive Shutdown here.
		OnShutdown: func() { e.shutdowns.Add(1) },
		OnReload: func() error {
			if e.failNext.Swap(false) {
				return errors.New("bad tools.yaml")
			}
			return nil
		},
	})
	require.NoError(t, err)
	e.srv.RegisterInputChannel("s1", make(chan []byte, 64))
	e.srv.RegisterResize("s1", func(uint16, uint16) error { return nil })
	serveTest(t, e.srv)
	return e
}

// replies sends one intent with id, then a FocusFilter sentinel, and
// returns every event carrying id that arrived before the sentinel's Ack.
// Replies on a connection are ordered, so anything sent for the intent has
// arrived by then.
func replies(t *testing.T, conn net.Conn, w *protocol.Writer, r *protocol.Reader, write func(id string) error, id string) []protocol.Envelope {
	t.Helper()
	require.NoError(t, write(id))
	sentinel := id + "-sentinel"
	require.NoError(t, w.WriteIntent(protocol.IntentFocusFilter, sentinel, protocol.FocusFilter{ToolID: "all"}))
	var got []protocol.Envelope
	for {
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
		env, err := r.Read()
		require.NoError(t, err, "waiting for the reply to %s", id)
		switch env.ID {
		case sentinel:
			require.Equal(t, protocol.EventAck, env.Type)
			return got
		case id:
			got = append(got, env)
		}
	}
}

// Every intent that carries an envelope ID gets exactly one terminal reply
// with that ID: its specific event (Hello: Snapshot), an Error on failure,
// or an Ack. This is the table in the protocol package doc.
func TestEveryIntentWithAnIDGetsExactlyOneTerminalReply(t *testing.T) {
	e := startAckServer(t)
	conn := dialTest(t, e.sock)
	w, r := helloOnConn(t, conn)

	const (
		ack  = protocol.EventAck
		fail = protocol.EventError
		snap = protocol.EventSnapshot
	)
	bad, unknown, badState := protocol.ErrCodeBadIntent, protocol.ErrCodeUnknownSession, protocol.ErrCodeBadState
	cases := []struct {
		intent  string
		payload any
		want    string // reply event type
		code    string // Error code when want is Error
		session string // Ack session_id ("*" = any non-empty)
		before  func()
	}{
		{intent: protocol.IntentHello, payload: protocol.Hello{ClientVersion: "v1", Client: "test"}, want: snap},

		{intent: protocol.IntentNewSession, payload: protocol.NewSession{ToolID: "echo", ModelID: "short", Slug: "fresh", CWD: e.dir}, want: ack, session: "*"},
		{intent: protocol.IntentNewSession, payload: protocol.NewSession{ModelID: "short", Slug: "x", CWD: e.dir}, want: fail, code: bad},
		{intent: protocol.IntentNewSession, payload: protocol.NewSession{ToolID: "nope", ModelID: "x", Slug: "y", CWD: e.dir}, want: fail, code: bad},

		{intent: protocol.IntentSubscribe, payload: protocol.Subscribe{}, want: ack},
		{intent: protocol.IntentSubscribe, payload: protocol.Subscribe{SessionID: "s1", ModeSync: true}, want: ack, session: "s1"},
		{intent: protocol.IntentSubscribe, payload: protocol.Subscribe{SessionID: "zz"}, want: fail, code: unknown},
		{intent: protocol.IntentSubscribe, payload: "not an object", want: fail, code: bad},

		{intent: protocol.IntentSendInput, payload: protocol.SendInput{SessionID: "s1", Bytes: []byte("x")}, want: ack, session: "s1"},
		{intent: protocol.IntentSendInput, payload: protocol.SendInput{SessionID: "s3", Bytes: []byte("x")}, want: fail, code: unknown},
		{intent: protocol.IntentSendInput, payload: protocol.SendInput{SessionID: "../x"}, want: fail, code: bad},

		{intent: protocol.IntentReply, payload: protocol.Reply{SessionID: "s1", Text: "hi"}, want: ack, session: "s1"},
		{intent: protocol.IntentReply, payload: protocol.Reply{SessionID: "s3", Text: "hi"}, want: fail, code: unknown},

		{intent: protocol.IntentRename, payload: protocol.Rename{SessionID: "s1", Title: "renamed"}, want: ack, session: "s1"},
		{intent: protocol.IntentRename, payload: protocol.Rename{SessionID: "s1", Slug: "s3"}, want: fail, code: bad},
		{intent: protocol.IntentRename, payload: protocol.Rename{SessionID: "zz", Title: "t"}, want: fail, code: unknown},

		{intent: protocol.IntentSetSessionName, payload: protocol.SetSessionName{SessionID: "s1", Name: "One"}, want: ack, session: "s1"},
		{intent: protocol.IntentSetSessionName, payload: protocol.SetSessionName{SessionID: "zz", Name: "x"}, want: fail, code: unknown},

		{intent: protocol.IntentSetSessionFleet, payload: protocol.SetSessionFleet{SessionID: "s1", Fleet: "f"}, want: ack, session: "s1"},
		{intent: protocol.IntentSetSessionFleet, payload: protocol.SetSessionFleet{SessionID: "zz", Fleet: "f"}, want: fail, code: unknown},

		{intent: protocol.IntentRetire, payload: protocol.Retire{SessionID: "s3"}, want: ack, session: "s3"},
		{intent: protocol.IntentRetire, payload: protocol.Retire{SessionID: "s3"}, want: ack, session: "s3"}, // already marked
		{intent: protocol.IntentRetire, payload: protocol.Retire{SessionID: "s3", TTLSeconds: 1}, want: fail, code: bad},
		{intent: protocol.IntentRetire, payload: protocol.Retire{SessionID: "zz"}, want: fail, code: unknown},
		{intent: protocol.IntentUnretire, payload: protocol.Unretire{SessionID: "s3"}, want: ack, session: "s3"},
		{intent: protocol.IntentUnretire, payload: protocol.Unretire{SessionID: "s3"}, want: ack, session: "s3"}, // not marked
		{intent: protocol.IntentUnretire, payload: protocol.Unretire{SessionID: "zz"}, want: fail, code: unknown},
		{
			intent: protocol.IntentUnretire, payload: protocol.Unretire{SessionID: "s3"}, want: fail, code: badState,
			before: func() {
				_, err := e.srv.cfg.Store.SetRetire("s3", time.Now().Add(-time.Second), time.Time{})
				require.NoError(t, err)
				claimed, _ := e.srv.cfg.Store.ClaimExpired(time.Now())
				require.Len(t, claimed, 1)
			},
		},
		{intent: protocol.IntentResume, payload: protocol.Resume{SessionID: "zz"}, want: fail, code: unknown},
		{intent: protocol.IntentResume, payload: protocol.Resume{SessionID: "s3"}, want: fail, code: bad}, // seed has no model
		{intent: protocol.IntentResume, payload: protocol.Resume{SessionID: "../x"}, want: fail, code: bad},

		{intent: protocol.IntentResize, payload: protocol.Resize{SessionID: "s1", Cols: 80, Rows: 24}, want: ack, session: "s1"},
		{intent: protocol.IntentResize, payload: protocol.Resize{SessionID: "s1"}, want: ack, session: "s1"}, // 0x0 is ignored
		{intent: protocol.IntentResize, payload: protocol.Resize{}, want: fail, code: bad},

		{intent: protocol.IntentComplete, payload: protocol.Complete{SessionID: "s1"}, want: ack, session: "s1"},
		{intent: protocol.IntentComplete, payload: protocol.Complete{SessionID: "zz"}, want: fail, code: unknown},

		{intent: protocol.IntentDelete, payload: protocol.Delete{SessionID: "s2"}, want: ack, session: "s2"},
		{intent: protocol.IntentDelete, payload: protocol.Delete{SessionID: "s2"}, want: fail, code: unknown},

		{intent: protocol.IntentFocusFilter, payload: protocol.FocusFilter{ToolID: "echo"}, want: ack},
		{intent: protocol.IntentSetMaxConcurrent, payload: protocol.SetMaxConcurrent{N: 8}, want: ack},
		{intent: protocol.IntentSetMaxConcurrent, payload: "eight", want: fail, code: bad},

		{intent: protocol.IntentReload, payload: struct{}{}, want: ack},
		{intent: protocol.IntentReload, payload: struct{}{}, want: fail, code: bad, before: func() { e.failNext.Store(true) }},
		{intent: protocol.IntentShutdown, payload: struct{}{}, want: ack},

		// Turn snapshots are off in this server; the success reply is
		// UndoResult, covered in undo_test.go.
		{intent: protocol.IntentUndo, payload: protocol.Undo{SessionID: "s1"}, want: fail, code: bad},
		{intent: protocol.IntentRedo, payload: protocol.Redo{SessionID: "s1"}, want: fail, code: bad},

		{intent: protocol.IntentOpenSession, payload: protocol.OpenSession{SessionID: "s1"}, want: fail, code: bad},
		{intent: "Bogus", payload: struct{}{}, want: fail, code: bad},
	}

	covered := map[string]bool{}
	for i, tc := range cases {
		id := fmt.Sprintf("req-%d", i)
		t.Run(fmt.Sprintf("%02d-%s-%s", i, tc.intent, tc.want), func(t *testing.T) {
			if tc.before != nil {
				tc.before()
			}
			got := replies(t, conn, w, r, func(id string) error { return w.WriteIntent(tc.intent, id, tc.payload) }, id)
			require.Len(t, got, 1, "exactly one terminal reply")
			env := got[0]
			require.Equal(t, protocol.KindEvent, env.Kind)
			require.Equal(t, tc.want, env.Type)
			switch tc.want {
			case ack:
				var a protocol.Ack
				require.NoError(t, json.Unmarshal(env.Data, &a))
				require.Equal(t, tc.intent, a.Intent)
				if tc.session == "*" {
					require.NotEmpty(t, a.SessionID)
				} else {
					require.Equal(t, tc.session, a.SessionID)
				}
			case fail:
				var ee protocol.ErrorEvent
				require.NoError(t, json.Unmarshal(env.Data, &ee))
				require.Equal(t, tc.code, ee.Code, ee.Message)
				require.Equal(t, id, ee.ID)
			case snap:
				var s protocol.Snapshot
				require.NoError(t, json.Unmarshal(env.Data, &s))
				require.Equal(t, version.Version, s.DaemonVersion)
			}
		})
		covered[tc.intent] = true
	}
	for typ := range defaultHandlers() {
		require.True(t, covered[typ], "intent %s has no reply case", typ)
	}
	require.EqualValues(t, 1, e.shutdowns.Load())

	// An Event sent as if it were an intent is rejected with its ID too.
	got := replies(t, conn, w, r, func(id string) error {
		return w.WriteEvent(protocol.EventSnapshot, id, protocol.Snapshot{})
	}, "evt")
	require.Len(t, got, 1)
	require.Equal(t, protocol.EventError, got[0].Type)
}

// An intent without an ID gets no Ack: clients that predate acks see the
// stream they always did. Failures still answer with an Error, ID empty.
func TestIntentWithoutID_NoAck(t *testing.T) {
	e := startAckServer(t)
	conn := dialTest(t, e.sock)
	w, r := helloOnConn(t, conn)

	require.NoError(t, w.WriteIntent(protocol.IntentRename, "", protocol.Rename{SessionID: "s1", Title: "quiet"}))
	require.NoError(t, w.WriteIntent(protocol.IntentRename, "", protocol.Rename{SessionID: "zz", Title: "t"}))
	require.NoError(t, w.WriteIntent(protocol.IntentFocusFilter, "done", protocol.FocusFilter{}))
	var types []string
	for {
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
		env, err := r.Read()
		require.NoError(t, err)
		if env.ID == "done" {
			break
		}
		types = append(types, env.Type)
		require.Empty(t, env.ID)
	}
	require.NotContains(t, types, protocol.EventAck)
	require.Contains(t, types, protocol.EventSessionUpdated, "the rename still applied")
	require.Contains(t, types, protocol.EventError, "the failed rename still answered")
}

// Broadcasts an intent causes reach a connection that sent Hello before
// that intent's Ack, so a client that waits for the Ack can read the
// resulting state from its event stream.
func TestAck_FollowsItsBroadcast(t *testing.T) {
	e := startAckServer(t)
	conn := dialTest(t, e.sock)
	w, r := helloOnConn(t, conn)

	require.NoError(t, w.WriteIntent(protocol.IntentNewSession, "n1",
		protocol.NewSession{ToolID: "echo", ModelID: "short", Slug: "ordered", CWD: e.dir}))
	var added string
	for {
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
		env, err := r.Read()
		require.NoError(t, err)
		if env.Type == protocol.EventSessionAdded {
			var sum protocol.SessionSummary
			require.NoError(t, json.Unmarshal(env.Data, &sum))
			added = sum.ID
		}
		if env.ID == "n1" {
			require.Equal(t, protocol.EventAck, env.Type)
			var a protocol.Ack
			require.NoError(t, json.Unmarshal(env.Data, &a))
			require.Equal(t, added, a.SessionID, "SessionAdded for the new session came first")
			return
		}
	}
}
