//go:build unix

package spawnctx

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A FIFO is refused without being opened: opening one for reading blocks
// until a writer comes, so a test that opened it would hang past its 1 s.
func TestValidatePath_FIFO(t *testing.T) {
	root := workspace(t, nil)
	fifo := filepath.Join(root, "pipe")
	require.NoError(t, syscall.Mkfifo(fifo, 0o600))
	require.NoError(t, os.Symlink(fifo, filepath.Join(root, "pipe-link")))

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := ValidatePath(root, "pipe")
		require.ErrorIs(t, err, ErrSpecial)
		_, err = ValidatePath(root, "pipe-link")
		require.ErrorIs(t, err, ErrSpecial, "a link to a FIFO too")
		_, _, err = Preview(root, "pipe", 64<<10, 200)
		require.ErrorIs(t, err, ErrSpecial)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the FIFO was opened")
	}
}

func TestValidatePath_Device(t *testing.T) {
	if _, err := os.Lstat("/dev/null"); err != nil {
		t.Skip("no /dev/null")
	}
	_, err := ValidatePath("/dev", "null")
	require.ErrorIs(t, err, ErrSpecial)
}

func TestOpenRead_NonBlocking(t *testing.T) {
	root := workspace(t, nil)
	fifo := filepath.Join(root, "pipe")
	require.NoError(t, syscall.Mkfifo(fifo, 0o600))
	// O_NONBLOCK: even a FIFO that slipped past the checks never hangs.
	f, err := openRead(fifo)
	require.NoError(t, err)
	require.NoError(t, f.Close())
}

func TestWalk_SkipsUnreadableFolders(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads everything")
	}
	root := workspace(t, map[string]string{"open/a.go": "x", "shut/b.go": "x"})
	require.NoError(t, os.Chmod(filepath.Join(root, "shut"), 0))
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(root, "shut"), 0o700) }) //nolint:gosec // G302: a folder needs x to be removed
	idx, err := Walk(context.Background(), root, 100, time.Minute)
	require.NoError(t, err)
	require.Equal(t, []string{"open/", "open/a.go", "shut/"}, idx.Paths)
}

// fakeGit puts a git on PATH that says the folder is a work tree (running
// onRevParse first) and runs lsFiles for ls-files.
func fakeGit(t *testing.T, onRevParse, lsFiles string) {
	t.Helper()
	bin := t.TempDir()
	script := "#!/bin/sh\n" +
		"for a in \"$@\"; do\n" +
		"  if [ \"$a\" = rev-parse ]; then " + onRevParse + "\n echo true; exit 0; fi\n" +
		"done\n" + lsFiles + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o700)) //nolint:gosec // G306: the fake git must be executable
	t.Setenv("PATH", bin)
}

func TestWalk_GitFailsFallsBackToPlainWalk(t *testing.T) {
	root := workspace(t, map[string]string{"a.go": "x"})
	fakeGit(t, ":", "exit 1")
	idx, err := Walk(context.Background(), root, 100, time.Minute)
	require.NoError(t, err)
	require.Equal(t, []string{"a.go"}, idx.Paths)
}

func TestWalk_GitOutputWithoutFinalNUL(t *testing.T) {
	root := workspace(t, nil)
	fakeGit(t, ":", `printf 'a/b.go\000c.go'`)
	idx, err := Walk(context.Background(), root, 100, time.Minute)
	require.NoError(t, err)
	require.Equal(t, []string{"a/", "a/b.go", "c.go"}, idx.Paths)
}

func TestWalk_GitGoneFallsBackToPlainWalk(t *testing.T) {
	root := workspace(t, map[string]string{"a.go": "x"})
	fakeGit(t, `/bin/rm "$0"`, "exit 0") // git is gone by the time ls-files starts
	idx, err := Walk(context.Background(), root, 100, time.Minute)
	require.NoError(t, err)
	require.Equal(t, []string{"a.go"}, idx.Paths)
}

func TestWalk_GitKilledAtBudgetKeepsWhatItListed(t *testing.T) {
	root := workspace(t, nil)
	fakeGit(t, ":", `printf 'early.go\000'; exec /bin/sleep 30`)
	start := time.Now()
	idx, err := Walk(context.Background(), root, 100, 1500*time.Millisecond)
	require.NoError(t, err)
	require.True(t, idx.Truncated)
	require.Equal(t, []string{"early.go"}, idx.Paths)
	require.Less(t, time.Since(start), 10*time.Second)
}
