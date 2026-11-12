package boot

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// With a git older than 2.32 no capture is taken, the notice is logged once
// and the session's state is untouched.
func TestSnapshots_OldGitDisablesCaptures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a shell script as git")
	}
	bin := t.TempDir()
	//nolint:gosec // a fake git must be executable
	require.NoError(t, os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\necho 'git version 2.30.2'\n"), 0o700))
	// Warm the script: a freshly written file's first exec can fail (text
	// file busy) or crawl under load, and a failed probe is a failed capture.
	require.NoError(t, exec.Command(filepath.Join(bin, "git")).Run()) //nolint:gosec // the fake git just written
	t.Setenv("PATH", bin)

	r := newSnapRig(t, true)
	dir := t.TempDir()
	r.add(t, "s1", dir, protocol.StateQueued)
	require.NoError(t, r.sessions.Transition("s1", protocol.StateWorking))
	require.NoError(t, r.sessions.Transition("s1", protocol.StateNeedsInput))
	require.Eventually(t, r.rec.oldWarned.Load, 30*time.Second, 50*time.Millisecond)
	r.rec.Close()
	sess, _ := r.sessions.Get("s1")
	require.Equal(t, protocol.StateNeedsInput, sess.Summary().State)
	_, err := os.Stat(r.store.Root())
	require.ErrorIs(t, err, os.ErrNotExist)
}
