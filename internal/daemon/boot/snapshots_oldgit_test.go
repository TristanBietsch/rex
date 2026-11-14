package boot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/features/snapshot"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// With a git older than 2.32 no capture is taken, the notice is logged once
// and the session's state is untouched.
func TestSnapshots_OldGitDisablesCaptures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a shell script as git")
	}
	// The fake git fails its first call, as an exec can on a loaded machine
	// (text file busy, fork limits). A failed probe is not remembered, and a
	// start capture that fails for any reason but an old git is recorded as
	// missed, which writes the store. So the probe is settled here, retrying
	// until it answers, and the recorder's captures only read the cached
	// answer: no git exec happens on the path under test.
	bin := t.TempDir()
	once := filepath.Join(bin, "probed")
	script := "#!/bin/sh\nif [ ! -e '" + once + "' ]; then : > '" + once + "'; exit 1; fi\necho 'git version 2.30.2'\n"
	//nolint:gosec // a fake git must be executable
	require.NoError(t, os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o700))
	t.Setenv("PATH", bin)
	require.Eventually(t, func() bool {
		return errors.Is(snapshot.CheckGit(context.Background()), snapshot.ErrGitTooOld)
	}, 30*time.Second, 50*time.Millisecond)

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
