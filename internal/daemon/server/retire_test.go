package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// fakeClock is a settable clock for Config.Now.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

type retireEnv struct {
	srv   *Server
	store *state.Store
	sock  string
	dir   string
	clock *fakeClock
}

// startRetireServer runs a server with a fake clock and a sweep interval long
// enough that tests drive sweeps explicitly via sweepRetired.
func startRetireServer(t *testing.T, seed ...*state.Session) *retireEnv {
	t.Helper()
	dir, err := os.MkdirTemp("", "rex-ret")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	store := state.NewStore()
	for _, s := range seed {
		require.NoError(t, state.WriteMeta(dir, s))
		require.NoError(t, store.Add(s))
	}
	reg, err := registry.Load("")
	require.NoError(t, err)
	clock := &fakeClock{now: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)}
	sock := filepath.Join(dir, "rex.sock")
	srv, err := New(Config{Socket: sock, StateDir: dir, Registry: reg, Store: store,
		Now: clock.Now, RetireSweepInterval: time.Hour})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan struct{})
	go func() { _ = srv.Serve(ctx); close(served) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-served:
		case <-time.After(5 * time.Second):
			t.Error("Serve did not return (reaper or client goroutine leaked)")
		}
	})
	for i := 0; i < 100; i++ {
		if c, err := net.Dial("unix", sock); err == nil {
			c.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	return &retireEnv{srv: srv, store: store, sock: sock, dir: dir, clock: clock}
}

func (e *retireEnv) dial(t *testing.T) (net.Conn, *protocol.Writer, *protocol.Reader) {
	t.Helper()
	conn, err := net.Dial("unix", e.sock)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	w, r := helloOnConn(t, conn)
	return conn, w, r
}

// waitEvent reads until match returns true or the deadline passes.
func waitEvent(t *testing.T, conn net.Conn, r *protocol.Reader, match func(protocol.Envelope) bool) protocol.Envelope {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn.SetReadDeadline(time.Now().Add(time.Second)) //nolint:errcheck
		env, err := r.Read()
		if err != nil {
			continue
		}
		if match(env) {
			return env
		}
	}
	t.Fatal("expected event not seen")
	return protocol.Envelope{}
}

func doneSession(id string) *state.Session {
	return &state.Session{ID: id, ShortID: id, Slug: id, ToolID: "echo",
		State: protocol.StateDone, StartedAt: time.Now().UTC()}
}

func TestSweep_DeletesExpiredTerminalSession(t *testing.T) {
	e := startRetireServer(t, doneSession("old"))
	conn, w, r := e.dial(t)

	require.NoError(t, w.WriteIntent(protocol.IntentRetire, "r1", protocol.Retire{SessionID: "old", TTLSeconds: 60}))
	waitEvent(t, conn, r, func(env protocol.Envelope) bool { return env.Type == protocol.EventSessionUpdated })

	e.srv.sweepRetired()
	_, ok := e.store.Get("old")
	require.True(t, ok, "not yet expired")

	e.clock.Advance(61 * time.Second)
	e.srv.sweepRetired()
	env := waitEvent(t, conn, r, func(env protocol.Envelope) bool { return env.Type == protocol.EventSessionRemoved })
	var rm protocol.SessionRemoved
	require.NoError(t, json.Unmarshal(env.Data, &rm))
	require.Equal(t, "old", rm.SessionID)
	require.Equal(t, protocol.RemovedRetired, rm.Reason)
	_, err := os.Stat(filepath.Join(e.dir, "sessions", "old"))
	require.True(t, os.IsNotExist(err))
}

func TestSweep_RestoredOverdueClearedFutureKept(t *testing.T) {
	past := doneSession("past")
	past.RetireAt = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	future := doneSession("future")
	future.RetireAt = time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	e := startRetireServer(t, past, future)
	// Serve's reaper sweeps immediately on start.
	require.Eventually(t, func() bool { _, ok := e.store.Get("past"); return !ok }, 3*time.Second, 20*time.Millisecond)
	_, ok := e.store.Get("future")
	require.True(t, ok)
}

func TestSweep_WorkingSessionWaitsUntilSettled(t *testing.T) {
	s := doneSession("busy")
	s.State = protocol.StateNeedsInput
	e := startRetireServer(t, s)
	require.NoError(t, e.store.Transition("busy", protocol.StateWorking))
	_, err := e.store.SetRetire("busy", e.clock.Now().Add(-time.Minute), time.Time{})
	require.NoError(t, err)

	e.srv.sweepRetired()
	_, ok := e.store.Get("busy")
	require.True(t, ok, "working session must not be cleared")

	require.NoError(t, e.store.Transition("busy", protocol.StateNeedsInput))
	e.srv.sweepRetired()
	_, ok = e.store.Get("busy")
	require.False(t, ok)
}

func TestRetire_Validation(t *testing.T) {
	e := startRetireServer(t, doneSession("a"))
	conn, w, r := e.dial(t)

	require.NoError(t, w.WriteIntent(protocol.IntentRetire, "bad", protocol.Retire{SessionID: "a", TTLSeconds: 1}))
	env := waitEvent(t, conn, r, func(env protocol.Envelope) bool { return env.Type == protocol.EventError })
	require.Equal(t, "bad", env.ID)

	require.NoError(t, w.WriteIntent(protocol.IntentRetire, "unk", protocol.Retire{SessionID: "nope"}))
	env = waitEvent(t, conn, r, func(env protocol.Envelope) bool { return env.Type == protocol.EventError })
	var ev protocol.ErrorEvent
	require.NoError(t, json.Unmarshal(env.Data, &ev))
	require.Equal(t, protocol.ErrCodeUnknownSession, ev.Code)
}

func TestRetire_DefaultTTLAndPersisted(t *testing.T) {
	e := startRetireServer(t, doneSession("a"))
	conn, w, r := e.dial(t)
	require.NoError(t, w.WriteIntent(protocol.IntentRetire, "", protocol.Retire{SessionID: "a"}))
	waitEvent(t, conn, r, func(env protocol.Envelope) bool { return env.Type == protocol.EventSessionUpdated })

	meta, err := state.LoadMeta(e.dir, "a")
	require.NoError(t, err)
	require.True(t, e.clock.Now().Add(DefaultRetireTTL).Equal(meta.RetireAt))
	require.True(t, e.clock.Now().Equal(meta.RetiredAt), "mark time persisted")
}

func TestUnretire_AfterClaim_BadState(t *testing.T) {
	e := startRetireServer(t, doneSession("a"))
	conn, w, r := e.dial(t)
	_, err := e.store.SetRetire("a", e.clock.Now().Add(-time.Second), time.Time{})
	require.NoError(t, err)
	claimed, _ := e.store.ClaimExpired(e.clock.Now())
	require.Len(t, claimed, 1)

	require.NoError(t, w.WriteIntent(protocol.IntentUnretire, "u", protocol.Unretire{SessionID: "a"}))
	env := waitEvent(t, conn, r, func(env protocol.Envelope) bool { return env.Type == protocol.EventError })
	var ev protocol.ErrorEvent
	require.NoError(t, json.Unmarshal(env.Data, &ev))
	require.Equal(t, protocol.ErrCodeBadState, ev.Code)
	require.Equal(t, "u", ev.ID)
}

// spawnPrompt starts an echo/prompt session (blocks on `read line`) and
// returns its id once the supervisor is running.
func spawnPrompt(t *testing.T, e *retireEnv, conn net.Conn, w *protocol.Writer, r *protocol.Reader) string {
	t.Helper()
	require.NoError(t, w.WriteIntent(protocol.IntentNewSession, "n", protocol.NewSession{
		ToolID: "echo", ModelID: "prompt", Slug: "p", CWD: e.dir,
	}))
	env := waitEvent(t, conn, r, func(env protocol.Envelope) bool { return env.Type == protocol.EventSessionAdded })
	var sum protocol.SessionSummary
	require.NoError(t, json.Unmarshal(env.Data, &sum))
	require.Eventually(t, func() bool { return e.srv.InputChannel(sum.ID) != nil && e.srv.hasStop(sum.ID) },
		3*time.Second, 20*time.Millisecond)
	return sum.ID
}

func TestSendInput_CancelsRetire_SyntheticDoesNot(t *testing.T) {
	e := startRetireServer(t)
	conn, w, r := e.dial(t)
	id := spawnPrompt(t, e, conn, w, r)

	require.NoError(t, w.WriteIntent(protocol.IntentRetire, "", protocol.Retire{SessionID: id, TTLSeconds: 3600}))
	require.Eventually(t, func() bool { s, _ := e.store.Get(id); return !s.Summary().RetireAt.IsZero() }, 2*time.Second, 10*time.Millisecond)

	require.NoError(t, w.WriteIntent(protocol.IntentSendInput, "", protocol.SendInput{SessionID: id, Bytes: []byte{0x0c}, Synthetic: true}))
	time.Sleep(150 * time.Millisecond)
	s, _ := e.store.Get(id)
	require.False(t, s.Summary().RetireAt.IsZero(), "synthetic input must not cancel")

	require.NoError(t, w.WriteIntent(protocol.IntentSendInput, "", protocol.SendInput{SessionID: id, Bytes: []byte("x")}))
	require.Eventually(t, func() bool { return s.Summary().RetireAt.IsZero() }, 2*time.Second, 10*time.Millisecond)
}

func TestSweep_LiveSession_KilledWithoutFailedPatch(t *testing.T) {
	e := startRetireServer(t)
	conn, w, r := e.dial(t)
	id := spawnPrompt(t, e, conn, w, r)
	require.Eventually(t, func() bool {
		st, _ := e.store.CurrentState(id)
		return st != protocol.StateWorking && st != protocol.StateQueued
	},
		5*time.Second, 50*time.Millisecond, "echo/prompt should settle to needs_input")

	_, err := e.store.SetRetire(id, e.clock.Now().Add(-time.Second), time.Time{})
	require.NoError(t, err)
	e.srv.sweepRetired()

	sawFailed := false
	waitEvent(t, conn, r, func(env protocol.Envelope) bool {
		if env.Type == protocol.EventSessionUpdated {
			var u protocol.SessionUpdated
			_ = json.Unmarshal(env.Data, &u)
			if u.Patch["state"] == string(protocol.StateFailed) {
				sawFailed = true
			}
		}
		return env.Type == protocol.EventSessionRemoved
	})
	require.False(t, sawFailed, "deleting a live session must not broadcast failed")
	require.False(t, e.srv.hasStop(id), "supervisor must have exited")
	require.Nil(t, e.srv.InputChannel(id))
}

func TestSetSessionName_BroadcastsToAllClientsAndPersists(t *testing.T) {
	e := startRetireServer(t, doneSession("a"))
	connA, wA, rA := e.dial(t)
	connB, _, rB := e.dial(t)

	require.NoError(t, wA.WriteIntent(protocol.IntentSetSessionName, "n1", protocol.SetSessionName{SessionID: "a", Name: "  my\nlabel "}))
	for _, c := range []struct {
		conn net.Conn
		r    *protocol.Reader
	}{{connA, rA}, {connB, rB}} {
		env := waitEvent(t, c.conn, c.r, func(env protocol.Envelope) bool { return env.Type == protocol.EventSessionUpdated })
		var u protocol.SessionUpdated
		require.NoError(t, json.Unmarshal(env.Data, &u))
		require.Equal(t, "my label", u.Patch["name"])
	}
	meta, err := state.LoadMeta(e.dir, "a")
	require.NoError(t, err)
	require.Equal(t, "my label", meta.Name)

	require.NoError(t, wA.WriteIntent(protocol.IntentSetSessionName, "n2", protocol.SetSessionName{SessionID: "a", Name: ""}))
	env := waitEvent(t, connA, rA, func(env protocol.Envelope) bool { return env.Type == protocol.EventSessionUpdated })
	var u protocol.SessionUpdated
	require.NoError(t, json.Unmarshal(env.Data, &u))
	v, ok := u.Patch["name"]
	require.True(t, ok)
	require.Equal(t, "", v)
}

func TestSetSessionName_Errors(t *testing.T) {
	e := startRetireServer(t, doneSession("a"))
	conn, w, r := e.dial(t)
	long := make([]rune, protocol.MaxSessionNameRunes+1)
	for i := range long {
		long[i] = 'x'
	}
	require.NoError(t, w.WriteIntent(protocol.IntentSetSessionName, "long", protocol.SetSessionName{SessionID: "a", Name: string(long)}))
	env := waitEvent(t, conn, r, func(env protocol.Envelope) bool { return env.Type == protocol.EventError })
	require.Equal(t, "long", env.ID)

	require.NoError(t, w.WriteIntent(protocol.IntentSetSessionName, "unk", protocol.SetSessionName{SessionID: "zzz", Name: "x"}))
	env = waitEvent(t, conn, r, func(env protocol.Envelope) bool { return env.Type == protocol.EventError })
	var ev protocol.ErrorEvent
	require.NoError(t, json.Unmarshal(env.Data, &ev))
	require.Equal(t, protocol.ErrCodeUnknownSession, ev.Code)
}

func TestDeleteSession_Idempotent(t *testing.T) {
	e := startRetireServer(t, doneSession("a"))
	require.NoError(t, e.srv.deleteSession("a", protocol.RemovedByUser))
	require.ErrorIs(t, e.srv.deleteSession("a", protocol.RemovedRetired), errSessionNotFound)
}

// deleteSession (the retire sweep's path) must read session fields under
// the session lock: a rename can land at the same time. Run with -race.
func TestDeleteSession_RacesRename(t *testing.T) {
	e := startRetireServer(t)
	for i := range 50 {
		id := fmt.Sprintf("s%d", i)
		require.NoError(t, e.store.Add(doneSession(id)))
		renamed := make(chan struct{})
		go func() {
			defer close(renamed)
			_ = e.store.UpdateRename(id, id+"-renamed", "")
		}()
		_ = e.srv.deleteSession(id, protocol.RemovedRetired)
		<-renamed
	}
}
