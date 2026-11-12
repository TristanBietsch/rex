package boot

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// blockingQwen puts a stub qwen on PATH that records that it started, then
// waits until the release file exists before printing a model list.
func blockingQwen(t *testing.T) (started, release string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub agents are POSIX shell scripts")
	}
	home := usageHome(t, "", false)
	_ = home
	bin := t.TempDir()
	started = filepath.Join(t.TempDir(), "started")
	release = filepath.Join(t.TempDir(), "release")
	script := "#!/bin/sh\n[ \"$1\" = --warm ] && exit 0\ntouch '" + started + "'\nwhile [ ! -e '" + release + "' ]; do sleep 0.05; done\nprintf 'MODEL\\nQwen3.8-Flash (qfmodel)\\n'\n"
	stub := filepath.Join(bin, "qwen")
	require.NoError(t, os.WriteFile(stub, []byte(script), 0o755)) //nolint:gosec // a test stub must be executable
	require.NoError(t, exec.Command(stub, "--warm").Run())
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin") // only the stub plus system tools: never a real agent
	return started, release
}

// A reload while a refresh is running cancels it: the old result is dropped
// even if the command would have finished, and the cache is never written.
func TestCatalog_ReloadDuringRefreshDropsOldResult(t *testing.T) {
	started, release := blockingQwen(t)
	st := loadSettings(t)
	srv, cat := newCatalogServer(t, st)
	cache := filepath.Join(os.Getenv("HOME"), ".local", "state", "rex", "models-cache.json")

	cat.Refresh(context.Background(), st, true)
	require.Eventually(t, func() bool { _, err := os.Stat(started); return err == nil }, 10*time.Second, 20*time.Millisecond)

	writeConfig(t, "model_autodetect: false\n")
	off := loadSettings(t)
	require.NoError(t, cat.Reload(context.Background(), off, false))
	require.NoError(t, os.WriteFile(release, nil, 0o600)) //nolint:gosec // test path under t.TempDir()

	done := make(chan struct{})
	go func() { cat.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("the canceled refresh did not stop")
	}
	require.False(t, hasModel(srv.Registry(), "qwen", "qfmodel"), "stale result dropped")
	require.NoFileExists(t, cache, "a canceled refresh writes no cache")
}

// Turning model_autodetect off while a refresh runs kills its subprocess.
func TestCatalog_FlagOffMidRefreshKillsSubprocess(t *testing.T) {
	started, _ := blockingQwen(t) // never released: only a kill ends it
	st := loadSettings(t)
	srv, cat := newCatalogServer(t, st)
	apply := cat.Applier(context.Background(), st)

	cat.Refresh(context.Background(), st, true)
	require.Eventually(t, func() bool { _, err := os.Stat(started); return err == nil }, 10*time.Second, 20*time.Millisecond)

	writeConfig(t, "model_autodetect: false\n")
	apply(loadSettings(t))

	done := make(chan struct{})
	go func() { cat.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("flag off did not stop the refresh")
	}
	require.False(t, hasModel(srv.Registry(), "qwen", "qfmodel"))
}
