package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

const testResumeID = "7d4f3c8a-1234-4abc-89ab-cdefabcdef01"

// resumeTools overlays echo with a resume block. "argv" prints the arguments
// rex gave it and exits quickly; "slow" does the same and stays up.
const resumeTools = `tools:
  - id: echo
    resume:
      args: ["--resume", "{id}"]
      new_args: ["--session-id", "{id}"]
    models:
      - id: argv
        name: "Prints its argv"
        args: ['echo "ARGV: $0 $*"; sleep 0.3']
      - id: slow
        name: "Prints its argv and stays"
        args: ['echo "ARGV: $0 $*"; sleep 30']
`

// startResumeServer is startRetireServer with a tools overlay and a session
// cap (0 = uncapped).
func startResumeServer(t *testing.T, toolsYAML string, maxSessions int, seed ...*state.Session) *retireEnv {
	t.Helper()
	dir, err := os.MkdirTemp("", "rex-res")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	store := state.NewStore()
	for _, s := range seed {
		require.NoError(t, state.WriteMeta(dir, s))
		require.NoError(t, store.Add(s))
	}
	toolsPath := ""
	if toolsYAML != "" {
		toolsPath = filepath.Join(dir, "tools.yaml")
		require.NoError(t, os.WriteFile(toolsPath, []byte(toolsYAML), 0o600))
	}
	reg, err := registry.Load(toolsPath)
	require.NoError(t, err)
	clock := &fakeClock{now: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)}
	sock := filepath.Join(dir, "rex.sock")
	srv, err := New(Config{Socket: sock, StateDir: dir, Registry: reg, Store: store,
		Now: clock.Now, RetireSweepInterval: time.Hour, MaxConcurrentSessions: maxSessions})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan struct{})
	go func() { _ = srv.Serve(ctx); close(served) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-served:
		case <-time.After(10 * time.Second):
			t.Error("Serve did not return")
		}
	})
	require.Eventually(t, func() bool {
		c, err := net.Dial("unix", sock)
		if err != nil {
			return false
		}
		c.Close()
		return true
	}, 2*time.Second, 20*time.Millisecond)
	return &retireEnv{srv: srv, store: store, sock: sock, dir: dir, clock: clock}
}

// exited is an exited echo session whose cwd is the state dir (always a dir).
func exited(e func() string, id string, st protocol.State, model, resumeID string) *state.Session {
	return &state.Session{ID: id, ShortID: id, Slug: id, ToolID: "echo", ModelID: model, CWD: e(),
		State: st, StartedAt: time.Now().UTC().Add(-time.Hour), EndedAt: time.Now().UTC().Add(-time.Minute),
		ResumeID: resumeID}
}

// call sends one intent and returns the terminal reply: "" for an Ack, else
// the Error code and message.
func call(t *testing.T, conn net.Conn, w *protocol.Writer, r *protocol.Reader, intent string, payload any) (code, msg string) {
	t.Helper()
	reqID := fmt.Sprintf("req-%d", reqSeq.Add(1))
	require.NoError(t, w.WriteIntent(intent, reqID, payload))
	env := waitEvent(t, conn, r, func(env protocol.Envelope) bool {
		return env.ID == reqID && (env.Type == protocol.EventAck || env.Type == protocol.EventError)
	})
	if env.Type == protocol.EventAck {
		return "", ""
	}
	var ev protocol.ErrorEvent
	require.NoError(t, json.Unmarshal(env.Data, &ev))
	return ev.Code, ev.Message
}

var reqSeq atomic.Int64

func (e *retireEnv) transcript(id string) string {
	raw, _ := os.ReadFile(filepath.Join(e.dir, "sessions", id, "transcript.log"))
	return string(raw)
}

func (e *retireEnv) state(id string) protocol.State {
	st, _ := e.store.CurrentState(id)
	return st
}

func (e *retireEnv) waitIdle(t *testing.T, id string) {
	t.Helper()
	require.Eventually(t, func() bool { return !e.srv.hasStop(id) }, 10*time.Second, 20*time.Millisecond)
}

func TestResume_RespawnsDoneSessionInPlace(t *testing.T) {
	e := startResumeServer(t, resumeTools, 0)
	sess := exited(func() string { return e.dir }, "a", protocol.StateDone, "slow", testResumeID)
	sess.RetireAt = time.Now().UTC().Add(time.Hour)
	sess.RetiredAt = time.Now().UTC()
	code := 7
	sess.ExitCode = &code
	require.NoError(t, e.store.Add(sess))
	require.NoError(t, state.WriteMeta(e.dir, sess))
	conn, w, r := e.dial(t)

	gotCode, msg := call(t, conn, w, r, protocol.IntentResume, protocol.Resume{SessionID: "a"})
	require.Empty(t, gotCode, msg)

	// The patch that announced the respawn cleared the end of the old run.
	// (Read from the store events below via meta: the commit wrote it first.)
	meta, err := state.LoadMeta(e.dir, "a")
	require.NoError(t, err)
	require.Equal(t, testResumeID, meta.ResumeID, "meta.json keeps the resume id")
	require.True(t, meta.EndedAt.IsZero(), "meta.json has no ended_at after resume")
	require.Nil(t, meta.ExitCode)
	require.True(t, meta.RetireAt.IsZero())
	require.Equal(t, "a", meta.Slug)

	require.Eventually(t, func() bool { return strings.Contains(e.transcript("a"), "ARGV: --resume "+testResumeID) },
		10*time.Second, 20*time.Millisecond, "transcript: %q", e.transcript("a"))
	require.NotContains(t, e.transcript("a"), "--session-id")
	require.True(t, e.srv.hasStop("a"))
	require.NoError(t, e.srv.deleteSession("a", protocol.RemovedByUser))
}

func TestResume_PatchClearsEndedAtAndMarks(t *testing.T) {
	e := startResumeServer(t, resumeTools, 0)
	sess := exited(func() string { return e.dir }, "a", protocol.StateFailed, "slow", testResumeID)
	sess.RetireAt = time.Now().UTC().Add(time.Hour)
	require.NoError(t, e.store.Add(sess))
	conn, w, r := e.dial(t)

	patches := make(chan map[string]any, 16)
	cancel := e.store.Subscribe(func(ev state.Event) {
		if ev.Kind == state.EventUpdated && ev.Patch != nil {
			select {
			case patches <- ev.Patch:
			default:
			}
		}
	})
	defer cancel()

	code, msg := call(t, conn, w, r, protocol.IntentResume, protocol.Resume{SessionID: "a"})
	require.Empty(t, code, msg)
	first := <-patches
	require.Equal(t, protocol.StateQueued, first["state"])
	for _, k := range []string{"last_event_at", "ended_at", "exit_code", "retire_at", "retired_at"} {
		require.Contains(t, first, k)
	}
	for _, k := range []string{"ended_at", "exit_code", "retire_at", "retired_at"} {
		require.Nil(t, first[k], k)
	}
	require.NoError(t, e.srv.deleteSession("a", protocol.RemovedByUser))
}

func TestResume_RunsToDoneAgain(t *testing.T) {
	e := startResumeServer(t, resumeTools, 0)
	sess := exited(func() string { return e.dir }, "a", protocol.StateDone, "argv", testResumeID)
	require.NoError(t, e.store.Add(sess))
	conn, w, r := e.dial(t)

	code, msg := call(t, conn, w, r, protocol.IntentResume, protocol.Resume{SessionID: "a"})
	require.Empty(t, code, msg)
	e.waitIdle(t, "a")
	require.Equal(t, protocol.StateDone, e.state("a"))
	meta, err := state.LoadMeta(e.dir, "a")
	require.NoError(t, err)
	require.False(t, meta.EndedAt.IsZero(), "the second run ended")
	require.Equal(t, testResumeID, meta.ResumeID)
	require.Equal(t, 1, strings.Count(e.transcript("a"), "ARGV:"))
}

func TestResume_LiveMarkedSessionOnlyUnmarks(t *testing.T) {
	e := startResumeServer(t, resumeTools, 0)
	conn, w, r := e.dial(t)
	code, msg := call(t, conn, w, r, protocol.IntentNewSession,
		protocol.NewSession{ToolID: "echo", ModelID: "slow", Slug: "live", CWD: e.dir})
	require.Empty(t, code, msg)
	var id string
	for _, s := range e.store.Snapshot() {
		id = s.ID
	}
	require.NotEmpty(t, id)
	require.Eventually(t, func() bool { return strings.Count(e.transcript(id), "ARGV:") == 1 }, 10*time.Second, 20*time.Millisecond)

	code, msg = call(t, conn, w, r, protocol.IntentRetire, protocol.Retire{SessionID: id, TTLSeconds: 3600})
	require.Empty(t, code, msg)
	sess, _ := e.store.Get(id)
	require.False(t, sess.Summary().RetireAt.IsZero())

	code, msg = call(t, conn, w, r, protocol.IntentResume, protocol.Resume{SessionID: id})
	require.Empty(t, code, msg)
	require.True(t, sess.Summary().RetireAt.IsZero(), "unmarked")
	meta, err := state.LoadMeta(e.dir, id)
	require.NoError(t, err)
	require.True(t, meta.RetireAt.IsZero(), "unmark persisted")

	// Unmarked already: still an Ack, still no spawn.
	code, msg = call(t, conn, w, r, protocol.IntentResume, protocol.Resume{SessionID: id})
	require.Empty(t, code, msg)
	time.Sleep(300 * time.Millisecond)
	require.Equal(t, 1, strings.Count(e.transcript(id), "ARGV:"), "no second spawn")
	require.NoError(t, e.srv.deleteSession(id, protocol.RemovedByUser))
}

func TestResume_Refusals(t *testing.T) {
	e := startResumeServer(t, resumeTools, 0)
	dir := func() string { return e.dir }
	badID := exited(dir, "badid", protocol.StateDone, "slow", "")
	flagID := exited(dir, "flag", protocol.StateDone, "slow", "--dangerously-skip-permissions-aaaaaaaaaaaa")
	gone := exited(dir, "gone", protocol.StateDone, "slow", testResumeID)
	gone.ToolID = "retired-tool"
	noModel := exited(dir, "nomodel", protocol.StateDone, "slow", testResumeID)
	noModel.ModelID = "vanished"
	noCWD := exited(dir, "nocwd", protocol.StateCrashed, "slow", testResumeID)
	noCWD.CWD = filepath.Join(e.dir, "deleted-dir")
	fileCWD := exited(dir, "filecwd", protocol.StateCrashed, "slow", testResumeID)
	fileCWD.CWD = filepath.Join(e.dir, "rex.sock.file")
	require.NoError(t, os.WriteFile(fileCWD.CWD, nil, 0o600))
	for _, s := range []*state.Session{badID, flagID, gone, noModel, noCWD, fileCWD} {
		require.NoError(t, e.store.Add(s))
	}
	conn, w, r := e.dial(t)

	cases := []struct {
		id, code, msg string
	}{
		{"nope", protocol.ErrCodeUnknownSession, "not found"},
		{"badid", protocol.ErrCodeBadState, "session exited and echo cannot be resumed"},
		{"flag", protocol.ErrCodeBadState, "session exited and echo cannot be resumed"},
		{"gone", protocol.ErrCodeBadIntent, "tool retired-tool/slow not in registry"},
		{"nomodel", protocol.ErrCodeBadIntent, "tool echo/vanished not in registry"},
		{"nocwd", protocol.ErrCodeSpawn, "working directory"},
		{"filecwd", protocol.ErrCodeSpawn, "working directory"},
	}
	for _, c := range cases {
		code, msg := call(t, conn, w, r, protocol.IntentResume, protocol.Resume{SessionID: c.id})
		require.Equal(t, c.code, code, c.id)
		require.Contains(t, msg, c.msg, c.id)
		require.False(t, e.srv.hasStop(c.id), c.id)
	}
	for _, id := range []string{"badid", "flag", "gone", "nomodel"} {
		require.Equal(t, protocol.StateDone, e.state(id), "a refused resume changes nothing: "+id)
	}
	require.Equal(t, protocol.StateCrashed, e.state("nocwd"))
}

// A tool with no resume block cannot come back.
func TestResume_ToolWithoutResumeIsBadState(t *testing.T) {
	e := startResumeServer(t, "", 0)
	sess := exited(func() string { return e.dir }, "a", protocol.StateDone, "short", testResumeID)
	require.NoError(t, e.store.Add(sess))
	conn, w, r := e.dial(t)

	code, msg := call(t, conn, w, r, protocol.IntentResume, protocol.Resume{SessionID: "a"})
	require.Equal(t, protocol.ErrCodeBadState, code)
	require.Equal(t, "session exited and echo cannot be resumed", msg)
	require.Equal(t, protocol.StateDone, e.state("a"))
	require.False(t, e.srv.hasStop("a"))
}

func TestResume_ClaimedReapingIsBadState(t *testing.T) {
	e := startResumeServer(t, resumeTools, 0)
	sess := exited(func() string { return e.dir }, "a", protocol.StateDone, "slow", testResumeID)
	require.NoError(t, e.store.Add(sess))
	_, err := e.store.SetRetire("a", e.clock.Now().Add(-time.Second), time.Time{})
	require.NoError(t, err)
	claimed, _ := e.store.ClaimExpired(e.clock.Now())
	require.Len(t, claimed, 1)
	conn, w, r := e.dial(t)

	code, msg := call(t, conn, w, r, protocol.IntentResume, protocol.Resume{SessionID: "a"})
	require.Equal(t, protocol.ErrCodeBadState, code)
	require.Equal(t, protocol.MsgClearing, msg)
	require.Equal(t, protocol.StateDone, e.state("a"))
	require.False(t, e.srv.hasStop("a"), "slot released, nothing spawned")
	e.srv.gate.mu.Lock()
	active := e.srv.gate.active
	e.srv.gate.mu.Unlock()
	require.Zero(t, active, "no session slot leaked")
}

func TestResume_ConcurrentDoubleSpawnsOnce(t *testing.T) {
	e := startResumeServer(t, resumeTools, 0)
	sess := exited(func() string { return e.dir }, "a", protocol.StateDone, "argv", testResumeID)
	require.NoError(t, e.store.Add(sess))

	const n = 6
	var wg sync.WaitGroup
	codes := make([]string, n)
	start := make(chan struct{})
	for i := range n {
		conn, w, r := e.dial(t)
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			codes[i], _ = call(t, conn, w, r, protocol.IntentResume, protocol.Resume{SessionID: "a"})
		}()
	}
	close(start)
	wg.Wait()
	for _, c := range codes {
		require.Empty(t, c)
	}
	e.waitIdle(t, "a")
	time.Sleep(200 * time.Millisecond)
	require.Equal(t, 1, strings.Count(e.transcript("a"), "ARGV:"), "exactly one spawn; transcript %q", e.transcript("a"))
}

func TestResume_AtCapIsTooManySessions(t *testing.T) {
	e := startResumeServer(t, resumeTools, 1)
	conn, w, r := e.dial(t)
	code, msg := call(t, conn, w, r, protocol.IntentNewSession,
		protocol.NewSession{ToolID: "echo", ModelID: "slow", Slug: "busy", CWD: e.dir})
	require.Empty(t, code, msg)
	sess := exited(func() string { return e.dir }, "a", protocol.StateDone, "slow", testResumeID)
	require.NoError(t, e.store.Add(sess))
	require.NoError(t, state.WriteMeta(e.dir, sess))

	code, msg = call(t, conn, w, r, protocol.IntentResume, protocol.Resume{SessionID: "a"})
	require.Equal(t, protocol.ErrCodeTooManySessions, code, msg)
	require.Equal(t, protocol.StateDone, e.state("a"), "not reopened")
	require.False(t, e.srv.hasStop("a"), "placeholder released")
	meta, err := state.LoadMeta(e.dir, "a")
	require.NoError(t, err)
	require.False(t, meta.EndedAt.IsZero(), "meta untouched")

	// Room again: the same Resume now works (the reservation did not leak).
	for _, s := range e.store.Snapshot() {
		if s.ID != "a" {
			require.NoError(t, e.srv.deleteSession(s.ID, protocol.RemovedByUser))
		}
	}
	e.srv.gate.SetMax(1)
	require.Eventually(t, func() bool {
		code, _ := call(t, conn, w, r, protocol.IntentResume, protocol.Resume{SessionID: "a"})
		return code == ""
	}, 5*time.Second, 50*time.Millisecond)
	require.NoError(t, e.srv.deleteSession("a", protocol.RemovedByUser))
}

// A Delete that lands in the reserve window cancels the reservation:
// startSupervisor starts no process and leaves no directory behind.
func TestResume_DeleteInReserveWindowStartsNothing(t *testing.T) {
	e := startResumeServer(t, resumeTools, 0)
	marker := filepath.Join(e.dir, "spawned")
	sess := exited(func() string { return e.dir }, "a", protocol.StateDone, "slow", testResumeID)
	require.NoError(t, e.store.Add(sess))
	require.NoError(t, state.WriteMeta(e.dir, sess))

	res, ok := e.srv.reserveStop("a")
	require.True(t, ok)
	_, again := e.srv.reserveStop("a")
	require.False(t, again, "slot is taken")
	require.True(t, e.srv.hasStop("a"))

	require.NoError(t, e.srv.deleteSession("a", protocol.RemovedByUser))
	require.True(t, res.cancelled.Load(), "delete cancelled the reservation")
	_, err := os.Stat(filepath.Join(e.dir, "sessions", "a"))
	require.True(t, os.IsNotExist(err))

	tool, model, ok := e.srv.Registry().FindModel("echo", "slow")
	require.True(t, ok)
	spec := spawnSpec{Tool: tool, Model: model, CWD: e.dir, SessionID: "a"}
	require.True(t, e.srv.TryAcquireSession())
	err = startSupervisor(context.Background(), e.srv, sess, spec,
		[]string{"bash", "-c", "touch " + marker}, nil, "", promptDelivery{}, res)
	e.srv.ReleaseSession()
	require.ErrorIs(t, err, errResumeCancelled)
	require.False(t, e.srv.hasStop("a"), "placeholder gone")
	time.Sleep(300 * time.Millisecond)
	_, err = os.Stat(marker)
	require.True(t, os.IsNotExist(err), "no process was spawned")
	_, err = os.Stat(filepath.Join(e.dir, "sessions", "a"))
	require.True(t, os.IsNotExist(err), "no orphan session directory")
}

func TestReserveStop_UnreserveAndPromote(t *testing.T) {
	s := &Server{}
	r, ok := s.reserveStop("a")
	require.True(t, ok)
	s.unreserveStop("a", r)
	require.False(t, s.hasStop("a"))
	s.unreserveStop("a", r) // idempotent

	r, ok = s.reserveStop("a")
	require.True(t, ok)
	stopped := false
	require.True(t, s.promoteStop("a", r, func() { stopped = true }))
	s.unreserveStop("a", r) // must not drop the promoted stop func
	require.True(t, s.hasStop("a"))
	s.StopSession("a")
	require.True(t, stopped)
	s.UnregisterStop("a")

	// A stale reservation cannot promote over someone else's slot.
	r, _ = s.reserveStop("a")
	s.unreserveStop("a", r)
	s.RegisterStop("a", func() {})
	require.False(t, s.promoteStop("a", r, func() { t.Fatal("must not register") }))
	require.True(t, s.hasStop("a"))
}

// Delete and Resume racing over the wire: whatever the interleaving, the
// session ends up gone with no process, no stop slot and no directory.
func TestResume_RacingDeleteLeavesNoOrphan(t *testing.T) {
	e := startResumeServer(t, resumeTools, 0)
	for i := range 15 {
		id := fmt.Sprintf("r%d", i)
		sess := exited(func() string { return e.dir }, id, protocol.StateDone, "slow", testResumeID)
		require.NoError(t, e.store.Add(sess))
		require.NoError(t, state.WriteMeta(e.dir, sess))
		c1, w1, r1 := e.dial(t)
		c2, w2, r2 := e.dial(t)
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			call(t, c1, w1, r1, protocol.IntentResume, protocol.Resume{SessionID: id})
		}()
		go func() {
			defer wg.Done()
			<-start
			call(t, c2, w2, r2, protocol.IntentDelete, protocol.Delete{SessionID: id})
		}()
		close(start)
		wg.Wait()
		_, still := e.store.Get(id)
		require.False(t, still, id)
		require.Eventually(t, func() bool { return !e.srv.hasStop(id) }, 5*time.Second, 10*time.Millisecond, id)
		require.Eventually(t, func() bool {
			_, err := os.Stat(filepath.Join(e.dir, "sessions", id))
			return os.IsNotExist(err)
		}, 5*time.Second, 10*time.Millisecond, "orphan dir for "+id)
	}
}

// A hook-detected agent's hook file is emptied before the respawn so the new
// adapter does not replay the old run's lines.
func TestResume_ResetsHookFile(t *testing.T) {
	const hookTools = `tools:
  - id: echo
    detect: { kind: hooks, format: claude }
    resume:
      args: ["--resume", "{id}"]
    models:
      - id: hook
        name: "Stays up"
        args: ['sleep 30']
`
	e := startResumeServer(t, hookTools, 0)
	sess := exited(func() string { return e.dir }, "a", protocol.StateCrashed, "hook", testResumeID)
	require.NoError(t, e.store.Add(sess))
	hf, err := state.CreateHookFile(e.dir, "a")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(hf, []byte("needs_input\nsid "+testResumeID+"\n"), 0o600))
	conn, w, r := e.dial(t)

	code, msg := call(t, conn, w, r, protocol.IntentResume, protocol.Resume{SessionID: "a"})
	require.Empty(t, code, msg)
	raw, err := os.ReadFile(hf)
	require.NoError(t, err)
	require.Empty(t, raw, "stale hook lines dropped")
	require.NoError(t, e.srv.deleteSession("a", protocol.RemovedByUser))
}

// A Resume that lands after the row turned done but before its supervisor
// released the stop slot (the done event goes out first) waits for the
// teardown and respawns: it used to count the session as live and only
// unmark it, so the resume silently did nothing.
func TestResume_WaitsForExitingSupervisor(t *testing.T) {
	e := startResumeServer(t, resumeTools, 0)
	sess := exited(func() string { return e.dir }, "a", protocol.StateDone, "slow", testResumeID)
	require.NoError(t, e.store.Add(sess))
	require.NoError(t, state.WriteMeta(e.dir, sess))

	// The old run's supervisor, still winding down: slot held, exit pending.
	exitedCh := make(chan struct{})
	e.srv.RegisterStop("a", func() { <-exitedCh })
	e.srv.registerExited("a", exitedCh)
	go func() {
		time.Sleep(200 * time.Millisecond)
		e.srv.UnregisterStop("a")
		close(exitedCh)
	}()

	conn, w, r := e.dial(t)
	code, msg := call(t, conn, w, r, protocol.IntentResume, protocol.Resume{SessionID: "a"})
	require.Empty(t, code, msg)
	require.Eventually(t, func() bool { return strings.Contains(e.transcript("a"), "ARGV: --resume "+testResumeID) },
		10*time.Second, 20*time.Millisecond, "the resume respawned the session")
	require.NoError(t, e.srv.deleteSession("a", protocol.RemovedByUser))
}
