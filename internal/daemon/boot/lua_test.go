package boot

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/features/lua"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

type sent struct{ id, text string }

func writeInitLua(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "init.lua")
	require.NoError(t, os.WriteFile(path, []byte(script), 0o600))
	return path
}

func testOptions(store *state.Store, out chan<- sent) lua.Options {
	return lua.Options{
		Sender: func(id, text string) error {
			select {
			case out <- sent{id, text}:
			default:
			}
			return nil
		},
		Lister:         store.Snapshot,
		HandlerTimeout: time.Hour, // only Close/stop may end the runaway handler
		LoadTimeout:    100 * time.Millisecond,
	}
}

func newSession(id string) *state.Session {
	return &state.Session{ID: id, ShortID: id, ToolID: "echo", Slug: id,
		State: protocol.StateQueued, StartedAt: time.Now().UTC()}
}

// within fails the test if fn does not return within d.
func within(t *testing.T, d time.Duration, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s did not return within %s", what, d)
	}
}

// P0-04: a `while true do end` handler used to run inside Store.broadcast,
// on the caller's goroutine (often the PTY reader), and hang the daemon.
func TestStartLua_RunawayHandlerDoesNotBlockStore(t *testing.T) {
	store := state.NewStore()
	out := make(chan sent, 8)
	path := writeInitLua(t, `
rex.on("session_added", function(s) rex.send(s.id, "spinning"); while true do end end)
`)
	rt, stop := startLua(path, store, testOptions(store, out))
	require.NotNil(t, rt)

	within(t, 500*time.Millisecond, "store.Add", func() {
		require.NoError(t, store.Add(newSession("s1")))
		require.NoError(t, store.Transition("s1", protocol.StateWorking))
	})
	require.Equal(t, sent{"s1", "spinning"}, <-out)

	// main.go runs Close before stop; neither may wait out the handler.
	within(t, 2*time.Second, "Runtime.Close", rt.Close)
	within(t, 2*time.Second, "stop", stop)
}

// P0-04: a runaway top-level init.lua must not hang daemon startup.
func TestStartLua_RunawayTopLevelTimesOut(t *testing.T) {
	store := state.NewStore()
	path := writeInitLua(t, `while true do end`)
	var (
		rt   *lua.Runtime
		stop func()
	)
	within(t, 2*time.Second, "startLua", func() {
		rt, stop = startLua(path, store, testOptions(store, make(chan sent, 1)))
	})
	require.NotNil(t, rt, "a failed load keeps the runtime for later reloads")
	stop()
	rt.Close()
}

// Store events reach handlers with the session's current fields.
func TestStartLua_DeliversStoreEvents(t *testing.T) {
	store := state.NewStore()
	out := make(chan sent, 8)
	path := writeInitLua(t, `
rex.on("session_added", function(s) rex.send(s.id, "added") end)
rex.on("session_updated", function(s) if s.state == "done" then rex.send(s.id, "done") end end)
rex.on("session_removed", function(s) rex.send(s.session_id, "removed") end)
`)
	rt, stop := startLua(path, store, testOptions(store, out))
	t.Cleanup(func() { stop(); rt.Close() })

	require.NoError(t, store.Add(newSession("s1")))
	require.NoError(t, store.Transition("s1", protocol.StateDone))
	require.NoError(t, store.Remove("s1"))

	for _, want := range []sent{{"s1", "added"}, {"s1", "done"}, {"s1", "removed"}} {
		select {
		case got := <-out:
			require.Equal(t, want, got)
		case <-time.After(2 * time.Second):
			t.Fatalf("never received %v", want)
		}
	}
}
