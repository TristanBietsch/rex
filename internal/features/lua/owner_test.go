package lua

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/testutil/testperm"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// loggedRuntime is a Runtime whose log goes to a buffer.
func loggedRuntime(t *testing.T) (*Runtime, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	r, err := New(Options{
		Logger: slog.New(slog.NewTextHandler(&buf, nil)),
		Sender: func(string, string) error { return nil },
		Lister: func() []protocol.SessionSummary { return nil },
	})
	require.NoError(t, err)
	t.Cleanup(r.Close)
	return r, &buf
}

const ownerScript = `rex.on("session_added", function(s) end) loaded = true`

// init.lua gets the same owner and mode rule as config.lua and theme files
// (SECURITY.md): a file or folder that group or others can write is refused
// with a log line, and nothing in it runs.
func TestLoadFile_RefusesLooseInitLua(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows modes carry no group/other bits")
	}
	for _, mode := range []os.FileMode{0o666, 0o620} {
		r, log := loggedRuntime(t)
		path := writeTempLua(t, ownerScript)
		testperm.Loosen(t, path, mode)
		err := r.LoadFile(path)
		require.ErrorContains(t, err, "writable by group or others")
		require.Equal(t, "nil", r.L.GetGlobal("loaded").String(), "nothing ran")
		require.Contains(t, log.String(), "lua: refusing init.lua")
	}
}

func TestLoadFile_RefusesLooseFolder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows modes carry no group/other bits")
	}
	r, log := loggedRuntime(t)
	path := writeTempLua(t, ownerScript)
	testperm.Loosen(t, filepath.Dir(path), 0o777)
	t.Cleanup(func() { testperm.Loosen(t, filepath.Dir(path), 0o700) })
	require.ErrorContains(t, r.LoadFile(path), "folder")
	require.Contains(t, log.String(), "lua: refusing init.lua")
}

func TestLoadFile_PrivateFileAndSafeLinkLoad(t *testing.T) {
	r, _ := loggedRuntime(t)
	path := writeTempLua(t, ownerScript)
	require.NoError(t, r.LoadFile(path))
	require.Equal(t, "true", r.L.GetGlobal("loaded").String())

	if runtime.GOOS == "windows" {
		return
	}
	dir := t.TempDir()
	link := filepath.Join(dir, "init.lua")
	require.NoError(t, os.Symlink(path, link))
	r2, _ := loggedRuntime(t)
	require.NoError(t, r2.LoadFile(link), "a link to a private file is fine")

	testperm.Loosen(t, path, 0o666)
	r3, _ := loggedRuntime(t)
	require.ErrorContains(t, r3.LoadFile(link), "writable by group or others", "the target is checked")
}

func TestLoadFile_NotARegularFileIsRefused(t *testing.T) {
	r, _ := loggedRuntime(t)
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "init.lua"), 0o700))
	require.Error(t, r.LoadFile(filepath.Join(dir, "init.lua")))
}
