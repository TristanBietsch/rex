package workspace

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/testutil/testenv"
)

func TestMain(m *testing.M) { testenv.Run(m) }

// mkWS makes root/.rex/workspace.json with content (0700 dir, 0600 file) and
// returns root.
func mkWS(t *testing.T, root, content string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(root, DirName), 0o700))
	require.NoError(t, os.Chmod(filepath.Join(root, DirName), 0o700)) //nolint:gosec // a directory
	require.NoError(t, os.WriteFile(filepath.Join(root, DirName, FileName), []byte(content), 0o600))
	return root
}

const goodJSON = `{"id":"7f3b89a2","name":"demo","color":"#0072B2","created_at":"2026-11-01T10:00:00Z"}`

func skipWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("mode and symlink checks do not apply on Windows")
	}
}

func TestFind_NearestWins(t *testing.T) {
	home := t.TempDir()
	outer := mkWS(t, filepath.Join(home, "dev"), `{"id":"aaaaaaaa","name":"outer","color":"#E69F00"}`)
	inner := mkWS(t, filepath.Join(outer, "rex"), goodJSON)
	deep := filepath.Join(inner, "internal", "x")
	require.NoError(t, os.MkdirAll(deep, 0o750))

	w, err := Find(deep, home)
	require.NoError(t, err)
	require.NotNil(t, w)
	require.Equal(t, "7f3b89a2", w.ID)
	require.Equal(t, "demo", w.Name)
	require.Equal(t, inner, w.Root)

	w, err = Find(outer, home)
	require.NoError(t, err)
	require.Equal(t, "aaaaaaaa", w.ID)
}

func TestFind_CwdItselfCounts(t *testing.T) {
	home := t.TempDir()
	root := mkWS(t, filepath.Join(home, "p"), goodJSON)
	w, err := Find(root, home)
	require.NoError(t, err)
	require.Equal(t, root, w.Root)
}

func TestFind_NoneReturnsNil(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "a", "b")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	w, err := Find(dir, home)
	require.NoError(t, err)
	require.Nil(t, w)
}

func TestFind_StopsAtHome(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home")
	mkWS(t, home, goodJSON) // a .rex in $HOME is never a workspace
	dir := filepath.Join(home, "proj")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	w, err := Find(dir, home)
	require.NoError(t, err)
	require.Nil(t, w)

	// Nor does the walk go above home.
	mkWS(t, base, goodJSON)
	w, err = Find(dir, home)
	require.NoError(t, err)
	require.Nil(t, w)
}

// fsRoot is the filesystem root: "/", or the temp dir's drive root (C:\) on
// Windows, where a bare separator is not absolute.
func fsRoot(t *testing.T) string {
	t.Helper()
	return filepath.VolumeName(t.TempDir()) + string(filepath.Separator)
}

func TestFind_StopsAtFilesystemRoot(t *testing.T) {
	// The root itself is never examined: with home elsewhere, a path that
	// reaches "/" ends there without error.
	w, err := Find(fsRoot(t), t.TempDir())
	require.NoError(t, err)
	require.Nil(t, w)
}

func TestFind_RelativeCwdRefused(t *testing.T) {
	_, err := Find("rel/dir", t.TempDir())
	require.Error(t, err)
}

func TestFind_SymlinkedRexDirRefused(t *testing.T) {
	skipWindows(t)
	home := t.TempDir()
	root := filepath.Join(home, "p")
	real := mkWS(t, filepath.Join(home, "elsewhere"), goodJSON)
	require.NoError(t, os.MkdirAll(root, 0o750))
	require.NoError(t, os.Symlink(filepath.Join(real, DirName), filepath.Join(root, DirName)))

	w, err := Find(root, home)
	require.ErrorIs(t, err, ErrUnsafe)
	require.Nil(t, w)
}

func TestFind_SymlinkedWorkspaceJSONRefused(t *testing.T) {
	skipWindows(t)
	home := t.TempDir()
	root := mkWS(t, filepath.Join(home, "p"), goodJSON)
	target := filepath.Join(home, "real.json")
	require.NoError(t, os.WriteFile(target, []byte(goodJSON), 0o600))
	jf := filepath.Join(root, DirName, FileName)
	require.NoError(t, os.Remove(jf))
	require.NoError(t, os.Symlink(target, jf))

	_, err := Find(root, home)
	require.ErrorIs(t, err, ErrUnsafe)
}

func TestFind_GroupWritableRexDirRefused(t *testing.T) {
	skipWindows(t)
	home := t.TempDir()
	root := mkWS(t, filepath.Join(home, "p"), goodJSON)
	require.NoError(t, os.Chmod(filepath.Join(root, DirName), 0o770)) //nolint:gosec // the point: group-writable

	_, err := Find(root, home)
	require.ErrorIs(t, err, ErrUnsafe)

	require.NoError(t, os.Chmod(filepath.Join(root, DirName), 0o702)) //nolint:gosec // test: the mode is what is under test
	_, err = Find(root, home)
	require.ErrorIs(t, err, ErrUnsafe)
}

func TestFind_WritableWorkspaceJSONRefused(t *testing.T) {
	skipWindows(t)
	home := t.TempDir()
	root := mkWS(t, filepath.Join(home, "p"), goodJSON)
	require.NoError(t, os.Chmod(filepath.Join(root, DirName, FileName), 0o666)) //nolint:gosec // test: the mode is what is under test
	_, err := Find(root, home)
	require.ErrorIs(t, err, ErrUnsafe)
}

func TestFind_UnsafeNearestDoesNotFallThroughToOuter(t *testing.T) {
	skipWindows(t)
	home := t.TempDir()
	outer := mkWS(t, filepath.Join(home, "dev"), goodJSON)
	inner := mkWS(t, filepath.Join(outer, "evil"), `{"id":"bbbbbbbb"}`)
	require.NoError(t, os.Chmod(filepath.Join(inner, DirName), 0o777)) //nolint:gosec // test: the mode is what is under test
	w, err := Find(inner, home)
	require.ErrorIs(t, err, ErrUnsafe)
	require.Nil(t, w)
}

func TestFind_OversizedWorkspaceJSONRejected(t *testing.T) {
	home := t.TempDir()
	big := `{"id":"7f3b89a2","name":"` + strings.Repeat("x", 70<<10) + `"}`
	root := mkWS(t, filepath.Join(home, "p"), big)
	w, err := Find(root, home)
	require.ErrorIs(t, err, ErrInvalid)
	require.Nil(t, w)
}

func TestFind_HostileFields(t *testing.T) {
	home := t.TempDir()
	long := strings.Repeat("é", 100)
	root := mkWS(t, filepath.Join(home, "p"),
		`{"id":"7f3b89a2","name":"`+long+`\u001b[31m\n","color":"red; drop","extra":{"a":[1,2,3]},"cmd":"rm -rf /"}`)
	w, err := Find(root, home)
	require.NoError(t, err)
	require.Equal(t, strings.Repeat("é", MaxNameRunes), w.Name, "name truncated to 40 runes")
	require.Empty(t, w.Color, "a color that is not #RRGGBB is dropped")
}

func TestFind_NameControlRunesStripped(t *testing.T) {
	home := t.TempDir()
	root := mkWS(t, filepath.Join(home, "p"), `{"id":"7f3b89a2","name":"a\u001b[2Jb\nc\u202ed"}`)
	w, err := Find(root, home)
	require.NoError(t, err)
	require.Equal(t, "a[2Jbcd", w.Name)
}

func TestFind_BadIDsRejected(t *testing.T) {
	for _, id := range []string{"", "7F3B89A2", "7f3b89a", "7f3b89a2a", "zzzzzzzz", "../../etc"} {
		home := t.TempDir()
		root := mkWS(t, filepath.Join(home, "p"), `{"id":"`+id+`"}`)
		w, err := Find(root, home)
		require.ErrorIsf(t, err, ErrInvalid, "id %q", id)
		require.Nil(t, w)
	}
}

func TestFind_NotJSONRejected(t *testing.T) {
	home := t.TempDir()
	root := mkWS(t, filepath.Join(home, "p"), `not json`)
	_, err := Find(root, home)
	require.ErrorIs(t, err, ErrInvalid)
}

func TestFind_RexDirWithoutIdentityIsNotAWorkspace(t *testing.T) {
	home := t.TempDir()
	outer := mkWS(t, filepath.Join(home, "dev"), goodJSON)
	inner := filepath.Join(outer, "p")
	require.NoError(t, os.MkdirAll(filepath.Join(inner, DirName), 0o700))
	w, err := Find(inner, home)
	require.NoError(t, err)
	require.Equal(t, outer, w.Root, "an empty .rex/ is skipped")
}

func TestFind_RexFileIsIgnored(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "p")
	require.NoError(t, os.MkdirAll(root, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, DirName), []byte("x"), 0o600))
	w, err := Find(root, home)
	require.NoError(t, err)
	require.Nil(t, w)
}

func TestSave_RoundTripAndModes(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "p")
	require.NoError(t, os.MkdirAll(root, 0o750))
	w := &Workspace{ID: "7f3b89a2", Name: "demo", Color: "#0072B2", Root: root}
	require.NoError(t, Save(w))

	got, err := Find(root, home)
	require.NoError(t, err)
	require.Equal(t, "demo", got.Name)
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(filepath.Join(root, DirName))
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o700), fi.Mode().Perm())
		fi, err = os.Stat(filepath.Join(root, DirName, FileName))
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
	}
	require.Error(t, Save(&Workspace{ID: "bad", Root: root}))
	require.Error(t, Save(&Workspace{ID: "7f3b89a2", Root: "rel"}))
}

func TestSave_RefusesSymlinkedFile(t *testing.T) {
	skipWindows(t)
	home := t.TempDir()
	root := mkWS(t, filepath.Join(home, "p"), goodJSON)
	target := filepath.Join(home, "victim")
	require.NoError(t, os.WriteFile(target, []byte("keep"), 0o600))
	jf := filepath.Join(root, DirName, FileName)
	require.NoError(t, os.Remove(jf))
	require.NoError(t, os.Symlink(target, jf))
	err := Save(&Workspace{ID: "7f3b89a2", Name: "x", Root: root})
	require.ErrorIs(t, err, ErrUnsafe)
	b, _ := os.ReadFile(target)
	require.Equal(t, "keep", string(b))
}

func TestLoad(t *testing.T) {
	home := t.TempDir()
	root := mkWS(t, filepath.Join(home, "p"), goodJSON)
	w, err := Load(root)
	require.NoError(t, err)
	require.Equal(t, "7f3b89a2", w.ID)
	w, err = Load(filepath.Join(root, "sub"))
	require.NoError(t, err)
	require.Nil(t, w)
	_, err = Load("rel")
	require.Error(t, err)
}
