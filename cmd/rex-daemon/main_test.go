package main

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/testutil/testenv"
)

// TestMain runs against a throwaway HOME so run() never touches the
// developer's real rex state, socket or logs.
//
// run() starts the daemon's model refresh (model_autodetect is on by
// default), which runs any installed agent's model-list command found on PATH.
// A test must never run a real agent, so PATH holds only system directories
// (git for the repo fixtures lives there).
func TestMain(m *testing.M) {
	if runtime.GOOS == "windows" {
		testenv.Run(m) // no system-dir PATH to fall back on
	}
	// The hot-restart e2e tests build this daemon: keep the developer's Go
	// caches (resolved before HOME moves) so the build never downloads.
	goBuildEnv = realGoCaches()
	if err := os.Setenv("PATH", "/usr/bin"+string(os.PathListSeparator)+"/bin"); err != nil {
		panic(err)
	}
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

// goBuildEnv is GOMODCACHE and GOCACHE as they were before TestMain
// isolated HOME (buildDaemon).
var goBuildEnv []string

// realGoCaches asks go env for the module and build caches.
func realGoCaches() []string {
	gobin := filepath.Join(runtime.GOROOT(), "bin", "go") //nolint:staticcheck // SA1019: the test builds on the machine it runs on
	out, err := exec.Command(gobin, "env", "GOMODCACHE", "GOCACHE").Output()
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 {
		return nil
	}
	return []string{"GOMODCACHE=" + lines[0], "GOCACHE=" + lines[1], "GOPROXY=off"}
}

// The TUI saves max_concurrent_sessions to config.yaml, but clients start
// the daemon without flags: the saved cap must apply unless the flag was
// given explicitly.
func TestMaxConcurrentFrom(t *testing.T) {
	st := settings.NewStore()
	require.NoError(t, st.Set("max_concurrent_sessions", 3))

	parse := func(args ...string) (*flag.FlagSet, *int) {
		fs := flag.NewFlagSet("rex-daemon", flag.ContinueOnError)
		n := fs.Int("max-concurrent-sessions", 16, "")
		require.NoError(t, fs.Parse(args))
		return fs, n
	}
	fs, n := parse()
	require.Equal(t, 3, maxConcurrentFrom(fs, *n, st), "the saved setting")
	fs, n = parse("-max-concurrent-sessions", "5")
	require.Equal(t, 5, maxConcurrentFrom(fs, *n, st), "an explicit flag wins")
	fs, n = parse()
	require.Equal(t, 16, maxConcurrentFrom(fs, *n, settings.NewStore()), "the default when nothing is saved")
}
