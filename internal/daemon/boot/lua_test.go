package boot

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/daemon/server"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/features/lua"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func newLuaRuntime(t *testing.T, script string, send func(id, text string) error) *lua.Runtime {
	t.Helper()
	rt, err := lua.New(lua.Options{
		Sender:      send,
		Lister:      func() []protocol.SessionSummary { return nil },
		CallTimeout: 200 * time.Millisecond,
	})
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "init.lua")
	require.NoError(t, os.WriteFile(path, []byte(script), 0o600))
	require.NoError(t, rt.LoadFile(path))
	return rt
}

// A stuck handler must not block the store broadcast: session supervisors
// and client handlers would hang on every state change.
func TestDispatchLua_StuckHandlerDoesNotBlockStore(t *testing.T) {
	rt := newLuaRuntime(t, `rex.on("session_updated", function(s) while true do end end)`,
		func(string, string) error { return nil })
	store := state.NewStore()
	require.NoError(t, store.Add(&state.Session{ID: "s1", ShortID: "s1", Slug: "s", State: protocol.StateWorking}))
	stop := dispatchLua(rt, store, 4)

	start := time.Now()
	for i := 0; i < 50; i++ {
		require.NoError(t, store.Transition("s1", protocol.StateNeedsInput))
	}
	require.Less(t, time.Since(start), time.Second, "store blocked on a Lua handler")

	start = time.Now()
	stop()
	require.Less(t, time.Since(start), 2*time.Second, "stop waits at most one handler timeout")
}

func TestDispatchLua_DeliversEvents(t *testing.T) {
	var mu sync.Mutex
	var got []string
	rt := newLuaRuntime(t, `rex.on("session_added", function(s) rex.send(s.id, "hi") end)`,
		func(id, _ string) error {
			mu.Lock()
			got = append(got, id)
			mu.Unlock()
			return nil
		})
	store := state.NewStore()
	stop := dispatchLua(rt, store, 4)
	defer stop()

	require.NoError(t, store.Add(&state.Session{ID: "s1", ShortID: "s1", Slug: "s", State: protocol.StateQueued}))
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) == 1 && got[0] == "s1"
	}, 2*time.Second, 10*time.Millisecond)
}

// rex.send must not land between a client Reply's text and its delayed
// Enter: the agent would submit the reply with the script's text glued on.
func TestLuaSender_WaitsForReplyEnter(t *testing.T) {
	dir, err := os.MkdirTemp("", "rex-b")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "rex.sock")
	reg, err := registry.Load("")
	require.NoError(t, err)
	srv, err := server.New(server.Config{Socket: sock, StateDir: dir, Registry: reg, Store: state.NewStore()})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan struct{})
	go func() { _ = srv.Serve(ctx); close(served) }()
	t.Cleanup(func() { cancel(); <-served })

	ch := make(chan []byte, 8)
	srv.RegisterInputChannel("sess-1", ch)
	var c *client.Client
	require.Eventually(t, func() bool { c, err = client.Dial(sock); return err == nil }, 2*time.Second, 10*time.Millisecond)
	defer c.Close()
	_, err = c.Hello("test")
	require.NoError(t, err)
	require.NoError(t, c.Reply("sess-1", "blue"))

	recv := func() string {
		select {
		case b := <-ch:
			return string(b)
		case <-time.After(2 * time.Second):
			t.Fatal("no input delivered")
			return ""
		}
	}
	require.Equal(t, "blue", recv())
	require.NoError(t, luaSender(srv)("sess-1", "x"))
	require.Equal(t, "\r", recv(), "rex.send overtook the Reply's Enter")
	require.Equal(t, "x", recv())
}
