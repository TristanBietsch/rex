package session

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/server"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/features/snapshot"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/testutil"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

type undoRig struct {
	sock  string
	repo  string
	snaps *snapshot.Store
}

func undoSess(id, slug, cwd string, st protocol.State) *state.Session {
	return &state.Session{ID: id, ShortID: id[:4], Slug: slug, ToolID: "echo", CWD: cwd, State: st, StartedAt: time.Now().UTC()}
}

// startUndoRig runs a daemon with turn snapshots on, a repo with one
// finished agent turn for session "alpha" (a.txt: one -> two, new.txt added)
// and any extra sessions.
func startUndoRig(t *testing.T, extra ...func(repo string) *state.Session) *undoRig {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	home := useHome(t)
	writeConfig(t, home, "turn_snapshots: true\n")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	repo := t.TempDir()
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "a.txt"), []byte("one\n"), 0o600))

	snaps := snapshot.NewStore(filepath.Join(t.TempDir(), "snapshots"))
	ctx := context.Background()
	_, err = snaps.Capture(ctx, repo, "alpha0001", snapshot.PhaseStart)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "a.txt"), []byte("two\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "new.txt"), []byte("new\n"), 0o600))
	_, err = snaps.Capture(ctx, repo, "alpha0001", snapshot.PhaseEnd)
	require.NoError(t, err)

	d := testutil.StartDaemonWithConfig(t, "", func(c *server.Config) {
		c.Snapshots = snaps
		c.SnapshotsEnabled = func() bool { return true }
		require.NoError(t, c.Store.Add(undoSess("alpha0001", "alpha", repo, protocol.StateNeedsInput)))
		for _, f := range extra {
			require.NoError(t, c.Store.Add(f(repo)))
		}
	})
	t.Cleanup(d.Close)
	return &undoRig{sock: d.Socket, repo: repo, snaps: snaps}
}

func (r *undoRig) file(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(r.repo, name))
	if err != nil {
		return "<missing>"
	}
	return string(b)
}

func TestRunUndo_NonTTYNeedsYes(t *testing.T) {
	r := startUndoRig(t)
	captureOut(t, "txt")
	_, errOut := withPruneIO(t, "y\n", false)
	err := RunUndo([]string{"-socket", r.sock, "alpha"})
	require.Equal(t, core.ExitOperationRefused, exitCode(t, err))
	require.ErrorContains(t, err, "-yes")
	require.Empty(t, errOut.String(), "no prompt without a terminal")
	require.Equal(t, "two\n", r.file(t, "a.txt"), "nothing changed")
}

func TestRunUndo_YesRestoresAndRedoBringsBack(t *testing.T) {
	r := startUndoRig(t)
	out := captureOut(t, "json")
	withPruneIO(t, "", false)
	require.NoError(t, RunUndo([]string{"-socket", r.sock, "-yes", "alpha"}))
	require.Equal(t, "one\n", r.file(t, "a.txt"))
	require.Equal(t, "<missing>", r.file(t, "new.txt"))
	require.Contains(t, out.String(), `"restored":["a.txt"]`)
	require.Contains(t, out.String(), `"deleted":["new.txt"]`)
	require.Contains(t, out.String(), `"op":"undo"`)

	out = captureOut(t, "txt")
	require.NoError(t, RunRedo([]string{"-socket", r.sock, "-y", "alpha"}))
	require.Equal(t, "two\n", r.file(t, "a.txt"))
	require.Equal(t, "new\n", r.file(t, "new.txt"))
	require.Contains(t, out.String(), "redo\t")

	err := RunRedo([]string{"-socket", r.sock, "-y", "alpha"})
	require.Equal(t, core.ExitOperationRefused, exitCode(t, err))
	require.ErrorContains(t, err, "nothing to redo")
}

func TestRunUndo_ConfirmListsCountsAndShellNote(t *testing.T) {
	r := startUndoRig(t)
	captureOut(t, "txt")
	_, errOut := withPruneIO(t, "n\n", true)
	err := RunUndo([]string{"-socket", r.sock, "alpha"})
	require.Equal(t, core.ExitOperationRefused, exitCode(t, err))
	require.Contains(t, errOut.String(), "restore 1 file(s), delete 1 file(s)")
	require.Contains(t, errOut.String(), "shell side effects (installs, databases, files outside the folder) are not undone")
	require.Contains(t, errOut.String(), "[y/N]")
	require.Equal(t, "two\n", r.file(t, "a.txt"), "declined: nothing changed")

	out := captureOut(t, "txt")
	withPruneIO(t, "yes\n", true)
	require.NoError(t, RunUndo([]string{"-socket", r.sock, "alpha"}))
	require.Equal(t, "one\n", r.file(t, "a.txt"))
	require.Contains(t, out.String(), "undo\t")
}

func TestRunUndo_TextViewTellsTheUser(t *testing.T) {
	r := startUndoRig(t)
	out := captureOut(t, "text")
	withPruneIO(t, "", false)
	require.NoError(t, RunUndo([]string{"-socket", r.sock, "-yes", "alpha"}))
	require.Contains(t, out.String(), "1 file(s) restored, 1 deleted")
	require.Contains(t, out.String(), "a.txt")
	require.Contains(t, out.String(), "shell side effects")
	require.Contains(t, out.String(), "agent was not told")
}

func TestRunUndo_OtherLiveSessionNeedsForce(t *testing.T) {
	r := startUndoRig(t, func(repo string) *state.Session {
		return undoSess("beta00002", "beta", repo, protocol.StateWorking)
	})
	captureOut(t, "txt")
	withPruneIO(t, "", false)
	err := RunUndo([]string{"-socket", r.sock, "-yes", "alpha"})
	require.Equal(t, core.ExitOperationRefused, exitCode(t, err))
	require.ErrorContains(t, err, "beta")
	require.Equal(t, "two\n", r.file(t, "a.txt"))
	require.NoError(t, RunUndo([]string{"-socket", r.sock, "-yes", "-force", "alpha"}))
	require.Equal(t, "one\n", r.file(t, "a.txt"))
}

func TestRunUndo_WorkingSessionRefused(t *testing.T) {
	r := startUndoRig(t, func(repo string) *state.Session {
		return undoSess("busy00003", "busy", repo, protocol.StateWorking)
	})
	captureOut(t, "txt")
	withPruneIO(t, "", false)
	err := RunUndo([]string{"-socket", r.sock, "-yes", "busy"})
	require.Equal(t, core.ExitOperationRefused, exitCode(t, err))
	require.ErrorContains(t, err, "working")
}

func TestRunUndo_Arguments(t *testing.T) {
	r := startUndoRig(t)
	captureOut(t, "txt")
	withPruneIO(t, "", false)
	require.Equal(t, core.ExitInvalidArgs, exitCode(t, RunUndo([]string{"-socket", r.sock})))
	require.Equal(t, core.ExitInvalidArgs, exitCode(t, RunUndo([]string{"-socket", r.sock, "a", "b"})))
	require.Equal(t, core.ExitInvalidArgs, exitCode(t, RunUndo([]string{"-socket", r.sock, "-turns", "0", "alpha"})))
	require.Equal(t, core.ExitInvalidArgs, exitCode(t, RunRedo([]string{"-socket", r.sock, "-turns", "2", "alpha"})), "redo has no -turns")
	require.Equal(t, core.ExitSelectorNotFound, exitCode(t, RunUndo([]string{"-socket", r.sock, "-yes", "nope"})))
	err := RunUndo([]string{"-socket", r.sock, "-yes", "-turns", "5", "alpha"})
	require.Equal(t, core.ExitOperationRefused, exitCode(t, err))
	require.ErrorContains(t, err, "only 1")
}

func TestRunUndo_OffExitsFive(t *testing.T) {
	home := useHome(t)
	writeConfig(t, home, "turn_snapshots: false\n")
	captureOut(t, "txt")
	for _, run := range []func([]string) error{RunUndo, RunRedo} {
		err := run([]string{"-yes", "x"})
		info := core.Describe(err)
		require.Equal(t, core.ExitInvalidArgs, info.Code)
		require.Equal(t, "off: enable in Settings → Features (rex config set turn_snapshots true)", info.Msg)
	}
}
