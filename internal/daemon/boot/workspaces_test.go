package boot

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
	"github.com/tristanbietsch/rex/internal/workspace"
)

type wsRig struct {
	home   string
	reg    *workspace.Registry
	store  *state.Store
	w      *Workspaces
	finds  atomic.Int32
	on     bool
	regErr error
}

func newWSRig(t *testing.T, on bool) *wsRig {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	reg, err := workspace.OpenRegistry(filepath.Join(home, "workspaces.json"))
	require.NoError(t, err)
	r := &wsRig{home: home, reg: reg, store: state.NewStore(), on: on}
	r.w = NewWorkspaces(WorkspacesConfig{Home: home, Registry: reg}, r.store)
	t.Cleanup(r.w.Close)
	real := r.w.find
	r.w.find = func(cwd, h string) (*workspace.Workspace, error) {
		r.finds.Add(1)
		return real(cwd, h)
	}
	r.w.SetEnabled(on)
	return r
}

// mkWS makes a workspace at home/<rel> and returns it.
func (r *wsRig) mkWS(t *testing.T, rel string) *workspace.Workspace {
	t.Helper()
	root := filepath.Join(r.home, rel)
	require.NoError(t, os.MkdirAll(root, 0o700))
	w, err := workspace.Init(root, filepath.Base(rel), r.reg)
	require.NoError(t, err)
	return w
}

func wsSession(id, cwd string) *state.Session {
	return &state.Session{ID: id, ShortID: id[:4], ToolID: "claude", ModelID: "opus", Slug: "s-" + id[:4],
		CWD: cwd, State: protocol.StateQueued, StartedAt: time.Now().UTC()}
}

func readLines(t *testing.T, root string) []workspace.IndexLine {
	t.Helper()
	f, err := os.Open(filepath.Join(root, workspace.DirName, workspace.IndexFileName))
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)
	defer f.Close()
	var out []workspace.IndexLine
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var l workspace.IndexLine
		require.NoError(t, json.Unmarshal(sc.Bytes(), &l))
		out = append(out, l)
	}
	return out
}

func TestWorkspaces_TagsNearestWorkspaceFromNestedFolder(t *testing.T) {
	r := newWSRig(t, true)
	outer := r.mkWS(t, "dev")
	inner := r.mkWS(t, "dev/app")
	nested := filepath.Join(inner.Root, "pkg", "x")
	require.NoError(t, os.MkdirAll(nested, 0o700))

	s := wsSession("aaaa1111", nested)
	r.w.Tag(s)
	require.Equal(t, inner.ID, s.WorkspaceID)
	require.NotEqual(t, outer.ID, s.WorkspaceID)

	other := wsSession("bbbb2222", filepath.Join(outer.Root, "other"))
	r.w.Tag(other)
	require.Equal(t, outer.ID, other.WorkspaceID)

	none := wsSession("cccc3333", t.TempDir())
	r.w.Tag(none)
	require.Empty(t, none.WorkspaceID)
}

func TestWorkspaces_StartedAndFirstEndWriteIndex(t *testing.T) {
	r := newWSRig(t, true)
	ws := r.mkWS(t, "proj")
	s := wsSession("aaaa1111", ws.Root)
	r.w.Tag(s)
	require.NoError(t, r.store.Add(s))
	r.w.Started(s)

	lines := readLines(t, ws.Root)
	require.Len(t, lines, 1)
	require.Equal(t, "aaaa1111", lines[0].SessionID)
	require.True(t, lines[0].EndedAt.IsZero())

	require.NoError(t, r.store.Transition("aaaa1111", protocol.StateDone))
	require.NoError(t, r.store.Transition("aaaa1111", protocol.StateDone))
	r.w.Close() // drains the queue
	lines = readLines(t, ws.Root)
	require.Len(t, lines, 2, "one line at spawn, one at the first terminal state")
	require.Equal(t, "done", lines[1].State)
	require.False(t, lines[1].EndedAt.IsZero())
}

func TestWorkspaces_OffIsInert(t *testing.T) {
	r := newWSRig(t, false)
	ws := r.mkWS(t, "proj")
	s := wsSession("aaaa1111", ws.Root)
	r.finds.Store(0)
	r.w.Tag(s)
	require.NoError(t, r.store.Add(s))
	r.w.Started(s)
	require.NoError(t, r.store.Transition("aaaa1111", protocol.StateDone))
	r.w.Close()
	require.Zero(t, r.finds.Load(), "no discovery while workspaces is off")
	require.Empty(t, s.WorkspaceID)
	_, err := os.Stat(filepath.Join(ws.Root, workspace.DirName, workspace.IndexFileName))
	require.True(t, os.IsNotExist(err), "no index file")
}

func TestWorkspaces_ReadOnlyRexWarnsOncePerRunSpawnStillWorks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory modes are not enforced on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory modes")
	}
	r := newWSRig(t, true)
	ws := r.mkWS(t, "proj")
	rexDir := filepath.Join(ws.Root, workspace.DirName)
	require.NoError(t, os.Chmod(rexDir, 0o500))       //nolint:gosec // read-only on purpose
	t.Cleanup(func() { _ = os.Chmod(rexDir, 0o700) }) //nolint:gosec // tidy up

	for i, id := range []string{"aaaa1111", "bbbb2222", "cccc3333"} {
		s := wsSession(id, ws.Root)
		r.w.Tag(s)
		require.Equal(t, ws.ID, s.WorkspaceID, "the spawn still gets its tag (%d)", i)
		require.NoError(t, r.store.Add(s))
		r.w.Started(s)
	}
	require.Equal(t, int32(1), r.w.warnings.Load(), "one notice per daemon run, not per event")
}

func TestWorkspaces_UnsafeRexIsNotTagged(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks and modes differ on Windows")
	}
	r := newWSRig(t, true)
	root := filepath.Join(r.home, "evil")
	require.NoError(t, os.MkdirAll(root, 0o700))
	require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(root, workspace.DirName)))
	s := wsSession("aaaa1111", root)
	r.w.Tag(s)
	require.Empty(t, s.WorkspaceID)
	require.Equal(t, int32(1), r.w.warnings.Load())
}
