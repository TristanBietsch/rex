package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/testutil/testperm"
)

func newTrust(t *testing.T) *TrustStore {
	t.Helper()
	s, err := OpenTrust(filepath.Join(t.TempDir(), "data", "workspace-trust.json"))
	require.NoError(t, err)
	return s
}

func loadWS(t *testing.T, root string) *Workspace {
	t.Helper()
	w, err := Load(root)
	require.NoError(t, err)
	require.NotNil(t, w)
	return w
}

func TestTrust_TrustRevokeAndPrivateFile(t *testing.T) {
	s := newTrust(t)
	w := loadWS(t, mkWS(t, filepath.Join(t.TempDir(), "p"), goodJSON))
	require.False(t, s.Trusted(w), "nothing is trusted by default")

	require.NoError(t, s.Trust(w))
	require.True(t, s.Trusted(w))
	fi, err := os.Stat(s.Path())
	require.NoError(t, err)
	if !skipModeChecks {
		require.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
	}

	changed, err := s.Revoke(w.ID, w.Root)
	require.NoError(t, err)
	require.True(t, changed)
	require.False(t, s.Trusted(w))
	changed, err = s.Revoke(w.ID, w.Root)
	require.NoError(t, err)
	require.False(t, changed)
}

// Trust is bound to the folder and the id together: a workspace that ships
// a trusted id from somewhere else, or a trusted folder whose .rex/ now
// holds another id, is not trusted.
func TestTrust_BoundToFolderAndID(t *testing.T) {
	s := newTrust(t)
	trusted := loadWS(t, mkWS(t, filepath.Join(t.TempDir(), "a"), goodJSON))
	require.NoError(t, s.Trust(trusted))

	clone := loadWS(t, mkWS(t, filepath.Join(t.TempDir(), "b"), goodJSON))
	require.Equal(t, trusted.ID, clone.ID)
	require.False(t, s.Trusted(clone), "same id, other folder")

	swapped := *trusted
	swapped.ID = "aaaaaaaa"
	require.False(t, s.Trusted(&swapped), "same folder, other id")

	moved := *trusted
	moved.Root = filepath.Join(t.TempDir(), "moved")
	require.False(t, s.Trusted(&moved), "a moved folder needs trusting again")
}

func TestTrust_RevokeByIDOrFolderAlone(t *testing.T) {
	s := newTrust(t)
	w := loadWS(t, mkWS(t, filepath.Join(t.TempDir(), "a"), goodJSON))
	require.NoError(t, s.Trust(w))
	changed, err := s.Revoke(w.ID, "")
	require.NoError(t, err)
	require.True(t, changed, "by id, when the folder is gone")
	require.NoError(t, s.Trust(w))
	changed, err = s.Revoke("", w.Root)
	require.NoError(t, err)
	require.True(t, changed, "by folder")
	require.False(t, s.Trusted(w))
}

func TestTrust_SymlinkedPathMatchesRealFolder(t *testing.T) {
	skipWindows(t)
	s := newTrust(t)
	root := mkWS(t, filepath.Join(t.TempDir(), "a"), goodJSON)
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(root, link))
	require.NoError(t, s.Trust(loadWS(t, link)))
	require.True(t, s.Trusted(loadWS(t, root)), "trust records the real folder")
}

// The trust file is a security decision: one that is a symlink, writable
// by others, owned by someone else or not JSON trusts nothing.
func TestTrust_UnsafeOrBadFileTrustsNothing(t *testing.T) {
	w := loadWS(t, mkWS(t, filepath.Join(t.TempDir(), "a"), goodJSON))

	s := newTrust(t)
	require.NoError(t, s.Trust(w))
	require.NoError(t, os.WriteFile(s.Path(), []byte("{not json"), 0o600))
	require.False(t, s.Trusted(w))
	require.Error(t, s.Trust(w), "a broken file is not overwritten")

	if skipModeChecks {
		return
	}
	s = newTrust(t)
	require.NoError(t, s.Trust(w))
	testperm.Loosen(t, s.Path(), 0o622)
	require.False(t, s.Trusted(w), "group/world-writable")

	s = newTrust(t)
	require.NoError(t, s.Trust(w))
	real := s.Path() + ".real"
	require.NoError(t, os.Rename(s.Path(), real))
	require.NoError(t, os.Symlink(real, s.Path()))
	require.False(t, s.Trusted(w), "a symlink")

	s = newTrust(t)
	require.NoError(t, s.Trust(w))
	prev := ownedByMe
	t.Cleanup(func() { ownedByMe = prev })
	ownedByMe = func(fi os.FileInfo) bool { return fi.IsDir() }
	require.False(t, s.Trusted(w), "another user's file")
}

func TestTrust_BadArgs(t *testing.T) {
	_, err := OpenTrust("rel.json")
	require.Error(t, err)
	s := newTrust(t)
	require.Error(t, s.Trust(&Workspace{ID: "bad", Root: "/x"}))
	require.Error(t, s.Trust(&Workspace{ID: "7f3b89a2", Root: "rel"}))
	require.False(t, s.Trusted(nil))
	_, err = s.Revoke("", "")
	require.Error(t, err)
}
