package server

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	wclient "github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// newHotTestServer builds a server over store in a short temp dir (the socket
// path must fit sun_path), lets cfg adjust it, and serves it.
func newHotTestServer(t *testing.T, store *state.Store, cfg func(*Config)) (*Server, string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "rex-hi")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	reg, err := registry.Load("")
	require.NoError(t, err)
	c := Config{Socket: filepath.Join(dir, "rex.sock"), StateDir: dir, Registry: reg, Store: store}
	if cfg != nil {
		cfg(&c)
	}
	srv, err := New(c)
	require.NoError(t, err)
	serveTest(t, srv)
	require.Eventually(t, func() bool { return srv.getListener() != nil }, 5*time.Second, 10*time.Millisecond)
	return srv, dir
}

// helloClient dials sock and says Hello as a CLI client.
func helloClient(t *testing.T, sock string) *wclient.Client {
	t.Helper()
	c, err := wclient.Dial(sock)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	_, err = c.Hello("rex-cli")
	require.NoError(t, err)
	return c
}

// requireErrCode fails unless err is a daemon Error reply with code.
func requireErrCode(t *testing.T, err error, code string) *wclient.Error {
	t.Helper()
	var we *wclient.Error
	require.ErrorAs(t, err, &we)
	require.Equal(t, code, we.Code, we.Message)
	return we
}

func doCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// Restart takes only the hot mode: a cold restart is the client's Shutdown
// plus a start, so the daemon refuses it as a bad intent. A hot restart on
// a daemon with no hot wiring is refused with restart_refused and points
// at the cold path; the connection stays usable.
func TestRestartIntentModeValidation(t *testing.T) {
	srv, _ := newHotTestServer(t, state.NewStore(), nil)
	c := helloClient(t, srv.cfg.Socket)
	ctx := doCtx(t)

	for _, mode := range []string{"cold", "", "HOT"} {
		_, err := c.Do(ctx, protocol.IntentRestart, protocol.Restart{Mode: mode})
		we := requireErrCode(t, err, protocol.ErrCodeBadIntent)
		require.Contains(t, we.Message, `must be "hot"`, "mode %q", mode)
	}

	_, err := c.Do(ctx, protocol.IntentRestart, protocol.Restart{Mode: protocol.RestartHot})
	we := requireErrCode(t, err, protocol.ErrCodeRestartRefused)
	require.Contains(t, we.Message, "-cold", "the refusal names the fallback")

	env, err := c.Do(ctx, protocol.IntentBoards, struct{}{})
	require.NoError(t, err, "the connection survives the refusals")
	require.Equal(t, protocol.EventBoards, env.Type)
}

// A Reload with no reload hook wired is refused; one whose layers partly
// failed answers with the whole report: what was read, what changed, what
// needs a restart and which layer failed, under the request's id.
func TestReloadedReportsPartialFailure(t *testing.T) {
	srv, _ := newHotTestServer(t, state.NewStore(), nil)
	c := helloClient(t, srv.cfg.Socket)
	_, err := c.Do(doCtx(t), protocol.IntentReload, struct{}{})
	we := requireErrCode(t, err, protocol.ErrCodeBadIntent)
	require.Contains(t, we.Message, "not implemented")

	want := protocol.ReloadReport{
		Reloaded: []string{"tools.yaml", "config.yaml"},
		Changed:  []string{"theme", "socket"},
		Restart:  []string{"socket"},
		Errors:   []string{"config.lua:4: attempt to call a nil value"},
	}
	srv2, _ := newHotTestServer(t, state.NewStore(), func(cfg *Config) {
		cfg.OnReload = func() protocol.ReloadReport { return want }
	})
	c2 := helloClient(t, srv2.cfg.Socket)
	env, err := c2.Do(doCtx(t), protocol.IntentReload, struct{}{})
	require.NoError(t, err, "a partial failure is a report, not an Error")
	require.Equal(t, protocol.EventReloaded, env.Type)
	var got protocol.ReloadReport
	require.NoError(t, json.Unmarshal(env.Data, &got))
	require.Equal(t, want, got)
}

// helloBoard connects a board (TUI) with pid and build and reads its
// Snapshot.
func helloBoard(t *testing.T, sock string, pid int, build string) (net.Conn, *protocol.Reader) {
	t.Helper()
	conn := dialTest(t, sock)
	w, r := protocol.NewWriter(conn), protocol.NewReader(conn)
	require.NoError(t, w.WriteIntent(protocol.IntentHello, "h", protocol.Hello{ClientVersion: "v1", Client: protocol.ClientTUI, PID: pid, Build: build}))
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	env, err := r.Read()
	require.NoError(t, err)
	require.Equal(t, protocol.EventSnapshot, env.Type)
	return conn, r
}

// readUntilType reads r until an event of type typ arrives.
func readUntilType(t *testing.T, r *protocol.Reader, typ string) protocol.Envelope {
	t.Helper()
	for {
		env, err := r.Read()
		require.NoError(t, err, "waiting for %s", typ)
		if env.Type == typ {
			return env
		}
	}
}

// Boards lists every board sorted by pid (CLI clients are not boards); a
// SwapBoards with no build asks every board to swap, whatever its build.
func TestBoardsSortedAndSwapWithoutBuild(t *testing.T) {
	srv, _ := newHotTestServer(t, state.NewStore(), nil)
	_, r9 := helloBoard(t, srv.cfg.Socket, 9, "b2")
	_, r3 := helloBoard(t, srv.cfg.Socket, 3, "b1")
	c := helloClient(t, srv.cfg.Socket)
	ctx := doCtx(t)

	var boards protocol.Boards
	require.Eventually(t, func() bool {
		env, err := c.Do(ctx, protocol.IntentBoards, struct{}{})
		require.NoError(t, err)
		boards = protocol.Boards{}
		require.NoError(t, json.Unmarshal(env.Data, &boards))
		return len(boards.Boards) == 2
	}, 5*time.Second, 20*time.Millisecond)
	require.Equal(t, []protocol.BoardInfo{{PID: 3, Build: "b1"}, {PID: 9, Build: "b2"}}, boards.Boards)

	env, err := c.Do(ctx, protocol.IntentSwapBoards, protocol.SwapBoards{})
	require.NoError(t, err)
	boards = protocol.Boards{}
	require.NoError(t, json.Unmarshal(env.Data, &boards))
	require.Len(t, boards.Boards, 2, "an empty build swaps every board")
	for _, r := range []*protocol.Reader{r3, r9} {
		var sw protocol.SwapBoards
		require.NoError(t, json.Unmarshal(readUntilType(t, r, protocol.EventSwap).Data, &sw))
		require.Empty(t, sw.Build)
	}
}

// A summarizer health flip reaches every client that said Hello.
func TestBroadcastSummarizerHealthReachesBoards(t *testing.T) {
	srv, _ := newHotTestServer(t, state.NewStore(), nil)
	_, r := helloBoard(t, srv.cfg.Socket, 5, "b")
	require.Eventually(t, func() bool {
		srv.summarizerHealthMu.RLock()
		defer srv.summarizerHealthMu.RUnlock()
		return srv.summarizerHealthSubs.Len() == 1
	}, 5*time.Second, 10*time.Millisecond)
	srv.BroadcastSummarizerHealth(false, "model unreachable")
	var h protocol.SummarizerHealth
	require.NoError(t, json.Unmarshal(readUntilType(t, r, protocol.EventSummarizerHealth).Data, &h))
	require.Equal(t, protocol.SummarizerHealth{Available: false, Reason: "model unreachable"}, h)
}

// A resize the session refuses (a frozen PTY answers errFrozen during a
// hot restart) comes back as an Error; a session with no resize callback
// takes the resize as a no-op.
func TestResizeRefusalIsReported(t *testing.T) {
	store := state.NewStore()
	require.NoError(t, store.Add(liveSession("r1")))
	require.NoError(t, store.Add(liveSession("r2")))
	srv, _ := newHotTestServer(t, store, nil)
	srv.RegisterResize("r1", func(uint16, uint16) error { return errors.New("session is frozen") })
	c := helloClient(t, srv.cfg.Socket)
	ctx := doCtx(t)

	_, err := c.Do(ctx, protocol.IntentResize, protocol.Resize{SessionID: "r1", Cols: 100, Rows: 30})
	we := requireErrCode(t, err, protocol.ErrCodeUnknownSession)
	require.Contains(t, we.Message, "frozen")

	_, err = c.Do(ctx, protocol.IntentResize, protocol.Resize{SessionID: "r2", Cols: 100, Rows: 30})
	require.NoError(t, err)
}

// EnsureLive (a loop's next iteration) refuses an unknown session, leaves
// a live one alone, and will not respawn an exited one while resume is off.
func TestEnsureLive(t *testing.T) {
	store := state.NewStore()
	require.NoError(t, store.Add(liveSession("live")))
	require.NoError(t, store.Add(liveSession("starting")))
	done := liveSession("done")
	done.State = protocol.StateDone
	require.NoError(t, store.Add(done))
	resume := true
	reg, err := registry.Load("")
	require.NoError(t, err)
	dir := t.TempDir()
	srv, err := New(Config{Socket: filepath.Join(dir, "s"), StateDir: dir, Registry: reg, Store: store,
		ResumeEnabled: func() bool { return resume }})
	require.NoError(t, err)
	stopped := false
	srv.RegisterStop("live", func() { stopped = true })
	ctx := context.Background()

	require.ErrorContains(t, srv.EnsureLive(ctx, "nope"), "not found")
	require.NoError(t, srv.EnsureLive(ctx, "live"))
	require.False(t, stopped, "a live session is not restarted")
	require.NoError(t, srv.EnsureLive(ctx, "starting"), "not terminal: nothing to do")
	resume = false
	require.ErrorContains(t, srv.EnsureLive(ctx, "done"), "resume is off")
	got, _ := store.Get("done")
	require.Equal(t, protocol.StateDone, got.Summary().State, "left as it was")
}

// While a hot restart is under way (the flag is set from the freeze until
// the exec or the rollback), a new session is refused and leaves nothing
// behind: no row, no claimed slot. A resume refused the same way settles
// the respawned row as failed instead of leaving it queued with no
// supervisor. Once the flag clears, both work again.
func TestRestartingRefusesSpawnAndResume(t *testing.T) {
	e := startResumeServer(t, resumeTools, 1)
	sess := exited(func() string { return e.dir }, "a", protocol.StateDone, "argv", testResumeID)
	require.NoError(t, e.store.Add(sess))
	require.NoError(t, state.WriteMeta(e.dir, sess))

	e.srv.hotActive.Store(true) // the restart window
	code, msg := e.newSession(t, protocol.NewSession{ToolID: "echo", ModelID: "argv", Slug: "fresh"})
	require.NotEmpty(t, code)
	require.Contains(t, msg, "hot-restarting")
	require.Len(t, e.store.Snapshot(), 1, "the refused session left no row")

	conn, w, r := e.dial(t)
	code, msg = call(t, conn, w, r, protocol.IntentResume, protocol.Resume{SessionID: "a"})
	require.Equal(t, protocol.ErrCodeSpawn, code)
	require.Contains(t, msg, "hot-restarting")
	require.Equal(t, protocol.StateFailed, e.state("a"), "not left queued without a supervisor")
	meta, err := state.LoadMeta(e.dir, "a")
	require.NoError(t, err)
	require.Equal(t, protocol.StateFailed, meta.State, "the failure is persisted")
	require.False(t, e.srv.hasStop("a"))

	e.srv.hotActive.Store(false)
	code, msg = e.newSession(t, protocol.NewSession{ToolID: "echo", ModelID: "argv", Slug: "fresh"})
	require.Empty(t, code, "the refused spawns gave their slot back: %s", msg)
	for _, sum := range e.store.Snapshot() {
		if sum.Slug == "fresh" {
			e.waitIdle(t, sum.ID) // the child exits on its own; let it end before cleanup
		}
	}
}
