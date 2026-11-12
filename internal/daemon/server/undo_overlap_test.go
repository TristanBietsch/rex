package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestUndo_OverlappingFoldersCount(t *testing.T) {
	repo := undoRepo(t)
	web := filepath.Join(repo, "web")
	require.NoError(t, os.MkdirAll(web, 0o750))
	link := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(repo, link); err != nil {
		t.Skipf("no symlinks: %v", err)
	}
	e := startUndoServer(t, true,
		undoSession("a", repo, protocol.StateNeedsInput),
		undoSession("child", web, protocol.StateWorking),
		undoSession("aliased", link, protocol.StateWorking),
		undoSession("apart", t.TempDir(), protocol.StateWorking))
	agentTurn(t, e, repo, "a", "a.txt", "two\n")

	_, code, msg := undoCall(t, e, protocol.IntentUndo, protocol.Undo{SessionID: "a"})
	require.Equal(t, protocol.ErrCodeBadState, code)
	require.Contains(t, msg, "aliased")
	require.Contains(t, msg, "child")
	require.NotContains(t, msg, "apart")

	// From the child's side the parent folder overlaps too.
	require.Equal(t, []string{"a", "aliased"}, liveSlugs(e, "child"))
}

func liveSlugs(e *undoEnv, id string) []string {
	sess, _ := e.store.Get(id)
	return e.srv.liveInFolder(sess.Summary())
}

func TestFoldersOverlap(t *testing.T) {
	sep := string(filepath.Separator)
	require.True(t, foldersOverlap(sep+"r", sep+"r"))
	require.True(t, foldersOverlap(sep+"r", sep+"r"+sep+"web"))
	require.True(t, foldersOverlap(sep+"r"+sep+"web", sep+"r"))
	require.False(t, foldersOverlap(sep+"r", sep+"rx"), "a shared name prefix is not a parent")
	require.False(t, foldersOverlap(sep+"r"+sep+"a", sep+"r"+sep+"b"))
}

// The refusal is evaluated again at the last moment: a session that started
// working after the first check trips it.
func TestUndoRefusalReReadsSessionState(t *testing.T) {
	repo := undoRepo(t)
	e := startUndoServer(t, true, undoSession("a", repo, protocol.StateNeedsInput))
	sess, _ := e.store.Get("a")
	require.NoError(t, e.srv.undoRefusal(sess, false))
	require.NoError(t, e.store.Transition("a", protocol.StateWorking))
	require.Error(t, e.srv.undoRefusal(sess, false))
	require.Error(t, e.srv.undoRefusal(sess, true), "force does not override a working session")
}
