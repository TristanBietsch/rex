package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/testutil/testenv"
)

// TestMain runs against a throwaway HOME so run() never touches the
// developer's real rex state, socket or logs.
func TestMain(m *testing.M) {
	testenv.Run(m)
}

func TestRun_Version(t *testing.T) {
	require.NoError(t, run([]string{"-version"}))
}

// TestRun_RefusesLockedStateDir is the P0-03 regression at the binary level:
// a second daemon on the same state dir exits with an error naming the
// holder's pid and the socket, before it reads or writes any state.
func TestRun_RefusesLockedStateDir(t *testing.T) {
	dir, err := os.MkdirTemp("", "rexd")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	held, err := state.AcquireLock(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = held.Release() })
	sock := filepath.Join(dir, "rex.sock")

	errc := make(chan error, 1)
	go func() { errc <- run([]string{"-socket", sock, "-state-dir", dir, "-tools", ""}) }()
	select {
	case err := <-errc:
		require.Error(t, err)
		require.Contains(t, err.Error(), "pid "+strconv.Itoa(os.Getpid()))
		require.Contains(t, err.Error(), sock)
	case <-time.After(10 * time.Second):
		t.Fatal("run started a second daemon on a locked state dir")
	}
	_, err = os.Lstat(sock)
	require.ErrorIs(t, err, os.ErrNotExist, "the refused daemon must not create the socket")
}

// Without a home directory there is no default socket, state dir or tools
// file: run names the flag to pass instead of using a path relative to the
// working directory. Flags given explicitly are taken as given.
func TestRun_NoHomeNeedsExplicitPaths(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("XDG_RUNTIME_DIR", "")

	err := run(nil)
	require.ErrorIs(t, err, paths.ErrNoHome)
	require.ErrorContains(t, err, "-socket")

	err = run([]string{"-socket", "/x/rex.sock"})
	require.ErrorIs(t, err, paths.ErrNoHome)
	require.ErrorContains(t, err, "-state-dir")

	err = run([]string{"-socket", "/x/rex.sock", "-state-dir", "/x"})
	require.ErrorIs(t, err, paths.ErrNoHome)
	require.ErrorContains(t, err, "-tools")

	require.NoError(t, run([]string{"-version"}), "-version needs no paths")
}
