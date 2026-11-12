package workspace

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

// isolatedHome points HOME (and USERPROFILE) at a fresh dir and returns it.
func isolatedHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func newReg(t *testing.T, home string) *Registry {
	t.Helper()
	reg, err := OpenRegistry(filepath.Join(home, "data", "workspaces.json"))
	require.NoError(t, err)
	return reg
}

func mkDir(t *testing.T, parts ...string) string {
	t.Helper()
	d := filepath.Join(parts...)
	require.NoError(t, os.MkdirAll(d, 0o755))
	return d
}

func TestInit_CreatesIdentityWithPrivateModes(t *testing.T) {
	home := isolatedHome(t)
	reg := newReg(t, home)
	root := mkDir(t, home, "dev", "demo")

	w, err := Init(root, "", reg)
	require.NoError(t, err)
	require.True(t, ValidID(w.ID))
	require.Equal(t, "demo", w.Name, "name defaults to the folder's")
	require.Equal(t, Swatches()[0], w.Color)
	require.Equal(t, root, w.Root)

	got, err := Find(root, home)
	require.NoError(t, err)
	require.Equal(t, w.ID, got.ID)

	if runtime.GOOS != "windows" {
		for path, want := range map[string]os.FileMode{
			filepath.Join(root, DirName):           0o700,
			filepath.Join(root, DirName, FileName): 0o600,
			reg.Path():                             0o600,
			filepath.Dir(reg.Path()):               0o700,
		} {
			fi, err := os.Stat(path)
			require.NoError(t, err)
			require.Equalf(t, want, fi.Mode().Perm(), "%s", path)
		}
	}
	es, err := reg.List()
	require.NoError(t, err)
	require.Len(t, es, 1)
	require.Equal(t, root, es[0].Root)
}

func TestInit_ExplicitNameAndIdempotent(t *testing.T) {
	home := isolatedHome(t)
	reg := newReg(t, home)
	root := mkDir(t, home, "p")

	a, err := Init(root, "First", reg)
	require.NoError(t, err)
	b, err := Init(root, "", reg)
	require.NoError(t, err)
	require.Equal(t, a.ID, b.ID, "init on a workspace keeps its id")
	require.Equal(t, "First", b.Name)

	c, err := Init(root, "Renamed", reg)
	require.NoError(t, err)
	require.Equal(t, a.ID, c.ID)
	require.Equal(t, "Renamed", c.Name)
}

func TestInit_NilRegistry(t *testing.T) {
	home := isolatedHome(t)
	w, err := Init(mkDir(t, home, "p"), "x", nil)
	require.NoError(t, err)
	require.True(t, ValidID(w.ID))
}

func TestInit_RefusesHomeAndRootAndNonFolders(t *testing.T) {
	home := isolatedHome(t)
	_, err := Init(home, "", nil)
	require.ErrorIs(t, err, ErrRefusedRoot)
	require.NoDirExists(t, filepath.Join(home, DirName))

	_, err = Init(string(filepath.Separator), "", nil)
	require.ErrorIs(t, err, ErrRefusedRoot)

	_, err = Init("rel", "", nil)
	require.Error(t, err)
	_, err = Init(filepath.Join(home, "missing"), "", nil)
	require.Error(t, err)
	f := filepath.Join(home, "file")
	require.NoError(t, os.WriteFile(f, nil, 0o600))
	_, err = Init(f, "", nil)
	require.Error(t, err)
}

func TestInit_RefusesUnsafeExistingRex(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks")
	}
	home := isolatedHome(t)
	root := mkDir(t, home, "p")
	elsewhere := mkDir(t, home, "elsewhere")
	require.NoError(t, os.Symlink(elsewhere, filepath.Join(root, DirName)))
	_, err := Init(root, "", nil)
	require.ErrorIs(t, err, ErrUnsafe)
	entries, _ := os.ReadDir(elsewhere)
	require.Empty(t, entries, "nothing written through the symlink")
}

func TestGenID_RegeneratesOnCollision(t *testing.T) {
	rnd := bytes.NewReader([]byte{
		0xaa, 0xaa, 0xaa, 0xaa, // taken
		0xbb, 0xbb, 0xbb, 0xbb, // taken
		0x01, 0x02, 0x03, 0x04, // free
	})
	taken := map[string]bool{"aaaaaaaa": true, "bbbbbbbb": true}
	var asked []string
	id, err := genID(rnd, func(s string) bool { asked = append(asked, s); return taken[s] })
	require.NoError(t, err)
	require.Equal(t, "01020304", id)
	require.Equal(t, []string{"aaaaaaaa", "bbbbbbbb", "01020304"}, asked)
}

func TestGenID_GivesUpAfterFiveTries(t *testing.T) {
	calls := 0
	_, err := genID(bytes.NewReader(bytes.Repeat([]byte{1}, 64)), func(string) bool { calls++; return true })
	require.Error(t, err)
	require.Equal(t, 5, calls)
}

func TestGenID_ShortReaderFails(t *testing.T) {
	_, err := genID(bytes.NewReader([]byte{1, 2}), nil)
	require.Error(t, err)
}

func TestInit_ColorsUniqueUntilListRunsOut(t *testing.T) {
	home := isolatedHome(t)
	reg := newReg(t, home)
	seen := map[string]bool{}
	for i := range Swatches() {
		w, err := Init(mkDir(t, home, "p", string(rune('a'+i))), "", reg)
		require.NoError(t, err)
		require.Falsef(t, seen[w.Color], "color %s repeated at %d", w.Color, i)
		seen[w.Color] = true
	}
	require.Len(t, seen, len(Swatches()))

	w, err := Init(mkDir(t, home, "p", "extra"), "", reg)
	require.NoError(t, err)
	require.Contains(t, Swatches(), w.Color, "after the list runs out a swatch is reused")
}

func TestSwatches_ReturnsCopyOfColorblindSafeList(t *testing.T) {
	s := Swatches()
	require.Equal(t, []string{"#0072B2", "#E69F00", "#009E73", "#CC79A7", "#56B4E9", "#D55E00", "#F0E442"}, s)
	for _, c := range s {
		require.True(t, ValidColor(c))
	}
	s[0] = "#000000"
	require.Equal(t, "#0072B2", Swatches()[0])
}

func TestRegistry_RootUpdatesWhenFolderMoves(t *testing.T) {
	home := isolatedHome(t)
	reg := newReg(t, home)
	old := mkDir(t, home, "old")
	w, err := Init(old, "proj", reg)
	require.NoError(t, err)

	moved := filepath.Join(home, "new")
	require.NoError(t, os.Rename(old, moved))
	got, err := Find(moved, home)
	require.NoError(t, err)
	require.Equal(t, w.ID, got.ID)
	require.NoError(t, reg.Seen(got))

	e, ok := reg.Get(w.ID)
	require.True(t, ok)
	require.Equal(t, moved, e.Root)
	es, err := reg.List()
	require.NoError(t, err)
	require.Len(t, es, 1, "same id, one row")
	require.True(t, reg.Taken(w.ID))
	require.False(t, reg.Taken("00000000"))
}

func TestRegistry_ListSortedAndTolerant(t *testing.T) {
	home := isolatedHome(t)
	reg := newReg(t, home)
	for _, w := range []*Workspace{
		{ID: "22222222", Name: "b", Root: "/r/b"},
		{ID: "11111111", Name: "b", Root: "/r/b2"},
		{ID: "33333333", Name: "a", Root: "/r/a"},
	} {
		require.NoError(t, reg.Seen(w))
	}
	es, err := reg.List()
	require.NoError(t, err)
	require.Equal(t, []string{"33333333", "11111111", "22222222"}, []string{es[0].ID, es[1].ID, es[2].ID})

	// A hand-edited file with a bad row keeps the good ones.
	require.NoError(t, os.WriteFile(reg.Path(), []byte(`{"v":1,"workspaces":{"11111111":{"id":"11111111","root":"/r","name":"ok","color":"nope"},"../x":{"id":"../x","root":"/r"},"22222222":{"id":"99999999","root":"/r"}}}`), 0o600))
	es, err = reg.List()
	require.NoError(t, err)
	require.Len(t, es, 1)
	require.Empty(t, es[0].Color)
}

func TestRegistry_Refusals(t *testing.T) {
	home := isolatedHome(t)
	_, err := OpenRegistry("rel.json")
	require.Error(t, err)
	_, err = OpenRegistry("")
	require.Error(t, err)

	reg := newReg(t, home)
	require.Empty(t, mustList(t, reg), "a missing file is an empty registry")
	require.Error(t, reg.Seen(&Workspace{ID: "bad", Root: "/r"}))

	require.NoError(t, os.MkdirAll(filepath.Dir(reg.Path()), 0o700))
	require.NoError(t, os.WriteFile(reg.Path(), []byte("{nope"), 0o600))
	_, err = reg.List()
	require.ErrorIs(t, err, ErrInvalid)
	require.False(t, reg.Taken("11111111"))

	require.NoError(t, os.WriteFile(reg.Path(), bytes.Repeat([]byte(" "), maxRegistryBytes+1), 0o600))
	_, err = reg.List()
	require.ErrorIs(t, err, ErrInvalid)

	if runtime.GOOS != "windows" {
		require.NoError(t, os.Remove(reg.Path()))
		target := filepath.Join(home, "victim")
		require.NoError(t, os.WriteFile(target, []byte("keep"), 0o600))
		require.NoError(t, os.Symlink(target, reg.Path()))
		require.ErrorIs(t, reg.Seen(&Workspace{ID: "11111111", Root: "/r"}), ErrUnsafe)
		b, _ := os.ReadFile(target)
		require.Equal(t, "keep", string(b))
	}
}

func mustList(t *testing.T, r *Registry) []Entry {
	t.Helper()
	es, err := r.List()
	require.NoError(t, err)
	return es
}
