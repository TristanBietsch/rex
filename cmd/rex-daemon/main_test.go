package main

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/daemon/server"
	"github.com/tristanbietsch/rex/internal/wire/client"
)

// A socket path over the sun_path limit fails up front with the path and
// the limit, not with a bare "invalid argument" from bind.
func TestRun_SocketPathTooLong(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("REX_LOG_DIR", filepath.Join(home, "logs"))
	sock := filepath.Join(home, strings.Repeat("s", 120), "rex.sock")

	err := run([]string{"-socket", sock, "-state-dir", filepath.Join(home, "state")})
	require.Error(t, err)
	require.Contains(t, err.Error(), sock)
	require.Contains(t, err.Error(), "allows at most")
}

// Clients start the daemon without flags, so the TUI's
// max_concurrent_sessions setting must apply unless the flag is explicit.
func TestMaxConcurrentSessions(t *testing.T) {
	st := settings.NewStore()
	require.NoError(t, st.Set("max_concurrent_sessions", 4))
	require.Equal(t, 4, maxConcurrentSessions(false, 16, st))
	require.Equal(t, 9, maxConcurrentSessions(true, 9, st))
	require.Equal(t, 16, maxConcurrentSessions(false, 16, settings.NewStore()))
}

// New dirs are owner-only; an existing one is tightened only when it is
// rex's own, never a user-supplied path that may be shared.
func TestOwnerOnlyDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}
	perm := func(dir string) os.FileMode {
		info, err := os.Stat(dir)
		require.NoError(t, err)
		return info.Mode().Perm()
	}

	fresh := filepath.Join(t.TempDir(), "a", "b")
	require.NoError(t, ownerOnlyDir(fresh, false))
	require.Equal(t, os.FileMode(0o700), perm(fresh))

	shared := filepath.Join(t.TempDir(), "shared")
	require.NoError(t, os.Mkdir(shared, 0o755))
	require.NoError(t, ownerOnlyDir(shared, false))
	require.Equal(t, os.FileMode(0o755), perm(shared))

	owned := filepath.Join(t.TempDir(), "owned")
	require.NoError(t, os.Mkdir(owned, 0o755))
	require.NoError(t, ownerOnlyDir(owned, true))
	require.Equal(t, os.FileMode(0o700), perm(owned))
}

// daemonEnv points HOME, the logs and the state dir at a fresh temp dir short
// enough for a socket path, with the summarizer off so no Ollama probe runs.
func daemonEnv(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "rex-m")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("REX_LOG_DIR", filepath.Join(dir, "logs"))
	cfg := filepath.Join(dir, ".config", "rex", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o700))
	require.NoError(t, os.WriteFile(cfg, []byte("summary_enabled: false\n"), 0o600))
	return dir
}

// runDaemon runs the daemon in dir and returns its result channel.
func runDaemon(dir, sock string) <-chan error {
	done := make(chan error, 1)
	go func() {
		done <- run([]string{"-socket", sock, "-state-dir", filepath.Join(dir, "state"), "-tools", filepath.Join(dir, "none.yaml")})
	}()
	return done
}

// stderrLines redirects os.Stderr to a pipe and returns its lines. restore
// puts stderr back (also done on cleanup); lines closes once the pipe drains.
func stderrLines(t *testing.T) (lines <-chan string, restore func()) {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	old := os.Stderr
	os.Stderr = w
	ch := make(chan string, 64)
	go func() {
		defer close(ch)
		defer r.Close()
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			ch <- sc.Text()
		}
	}()
	var once sync.Once
	restore = func() {
		once.Do(func() {
			os.Stderr = old
			_ = w.Close()
		})
	}
	t.Cleanup(restore)
	return ch, restore
}

// The banner says the socket is up, so it comes only once the socket answers.
func TestRun_ListeningBannerOnceSocketAnswers(t *testing.T) {
	dir := daemonEnv(t)
	sock := filepath.Join(dir, "rex.sock")
	lines, _ := stderrLines(t)
	done := runDaemon(dir, sock)

	for banner := false; !banner; {
		select {
		case line := <-lines:
			banner = strings.Contains(line, "listening on "+sock)
		case err := <-done:
			t.Fatalf("run returned before listening: %v", err)
		case <-time.After(10 * time.Second):
			t.Fatal("no listening banner")
		}
	}
	c, err := client.Dial(sock)
	require.NoError(t, err, "banner printed but the socket does not answer")
	require.NoError(t, c.Shutdown())
	_ = c.Close()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("daemon did not shut down")
	}
}

// A second daemon must be refused before it touches the state dir: loading
// sessions deletes meta temp files the running daemon may be renaming.
func TestRun_SecondDaemonLeavesStateAlone(t *testing.T) {
	dir := daemonEnv(t)
	sock := filepath.Join(dir, "rex.sock")
	first := runDaemon(dir, sock)
	require.Eventually(t, func() bool {
		c, err := client.Dial(sock)
		if err == nil {
			_ = c.Close()
		}
		return err == nil
	}, 10*time.Second, 20*time.Millisecond, "first daemon never listened")

	tmp := filepath.Join(dir, "state", "sessions", "s1", "meta-1.tmp")
	require.NoError(t, os.MkdirAll(filepath.Dir(tmp), 0o700))
	require.NoError(t, os.WriteFile(tmp, []byte("{}"), 0o600))

	select {
	case err := <-runDaemon(dir, sock):
		require.ErrorIs(t, err, server.ErrAlreadyRunning)
	case <-time.After(10 * time.Second):
		t.Fatal("second daemon did not return")
	}
	require.FileExists(t, tmp, "refused daemon deleted the running daemon's temp file")

	c, err := client.Dial(sock)
	require.NoError(t, err)
	require.NoError(t, c.Shutdown())
	_ = c.Close()
	select {
	case err := <-first:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("first daemon did not shut down")
	}
}

// A daemon that cannot bind its socket must not first claim it is listening.
func TestRun_NoListeningBannerWhenBindFails(t *testing.T) {
	dir := daemonEnv(t)
	// A non-empty directory at the socket path is no stale socket, so it
	// stays put and bind fails.
	sock := filepath.Join(dir, "rex.sock")
	require.NoError(t, os.MkdirAll(filepath.Join(sock, "keep"), 0o700))
	lines, restore := stderrLines(t)

	var err error
	select {
	case err = <-runDaemon(dir, sock):
	case <-time.After(10 * time.Second):
		t.Fatal("run did not return")
	}
	require.ErrorContains(t, err, "listen")
	restore()
	for line := range lines {
		require.NotContains(t, line, "listening")
	}
}
