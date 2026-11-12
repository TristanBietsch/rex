package boot

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
)

func recv(t *testing.T, out <-chan sent) sent {
	t.Helper()
	select {
	case got := <-out:
		return got
	case <-time.After(2 * time.Second):
		t.Fatal("no lua send")
		return sent{}
	}
}

// R20: rex reload rebuilds init.lua's runtime; a file that fails keeps the
// previous handlers and the error names file:line.
func TestLuaHostReload(t *testing.T) {
	store := state.NewStore()
	out := make(chan sent, 8)
	path := writeInitLua(t, `rex.on("session_added", function(s) rex.send(s.id, "v1") end)`)
	rt, stop := startLua(path, store, testOptions(store, out))
	h := &LuaHost{path: path, store: store, opts: testOptions(store, out), rt: rt, stop: stop}
	t.Cleanup(h.Close)

	require.NoError(t, store.Add(newSession("a")))
	require.Equal(t, sent{"a", "v1"}, recv(t, out))

	require.NoError(t, os.WriteFile(path, []byte(`rex.on("session_added", function(s) rex.send(s.id, "v2") end)`), 0o600))
	require.NoError(t, h.Reload())
	require.NoError(t, store.Add(newSession("b")))
	require.Equal(t, sent{"b", "v2"}, recv(t, out), "new handlers, and only them")
	select {
	case extra := <-out:
		t.Fatalf("old handler still ran: %v", extra)
	case <-time.After(100 * time.Millisecond):
	}

	require.NoError(t, os.WriteFile(path, []byte("rex.on(\n  oops oops\n"), 0o600))
	err := h.Reload()
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "init.lua:"), err.Error())
	require.NoError(t, store.Add(newSession("c")))
	require.Equal(t, sent{"c", "v2"}, recv(t, out), "a failed reload keeps the previous handlers")
}

func TestLuaFileError_FileLine(t *testing.T) {
	err := luaFileError(errors.New(`lua: LoadFile "/x/init.lua": /x/init.lua line:2(column:11) near 'oops':   syntax error `))
	require.Equal(t, "/x/init.lua:2: near 'oops':   syntax error", err.Error())
	err = luaFileError(errors.New("lua: LoadFile \"/x/init.lua\": /x/init.lua:4: boom\nstack traceback:\n\t[G]: ?"))
	require.Equal(t, "/x/init.lua:4: boom", err.Error())
}
