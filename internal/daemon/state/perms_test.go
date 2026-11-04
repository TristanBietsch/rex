//go:build !windows

package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/testutil/testperm"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func requireMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	fi, err := os.Lstat(path)
	require.NoError(t, err)
	require.Equal(t, want, fi.Mode().Perm(), "%s", path)
}

// TestWriteMeta_OwnerOnly is the S-01 regression for meta.json: session
// metadata (cwd, titles, prompts) is private to the user.
func TestWriteMeta_OwnerOnly(t *testing.T) {
	root := t.TempDir()
	sess := &Session{ID: "m1", ShortID: "m1", Slug: "m1", State: protocol.StateDone, StartedAt: time.Now()}
	require.NoError(t, WriteMeta(root, sess))
	requireMode(t, filepath.Join(root, "sessions"), 0o700)
	requireMode(t, filepath.Join(root, "sessions", "m1"), 0o700)
	requireMode(t, filepath.Join(root, "sessions", "m1", "meta.json"), 0o600)
}

func TestOpenTranscript_OwnerOnly(t *testing.T) {
	root := t.TempDir()
	tf, err := OpenTranscript(root, "t1")
	require.NoError(t, err)
	require.NoError(t, tf.Write([]byte("secret output\n")))
	require.NoError(t, tf.Close())
	requireMode(t, filepath.Join(root, "sessions", "t1"), 0o700)
	requireMode(t, filepath.Join(root, "sessions", "t1", "transcript.log"), 0o600)
}

func TestCreateHookFile_OwnerOnly(t *testing.T) {
	root := t.TempDir()
	path, err := CreateHookFile(root, "h1")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, "sessions", "h1", "hooks.log"), path)
	requireMode(t, filepath.Dir(path), 0o700)
	requireMode(t, path, 0o600)

	// Creating it again keeps what the agent already appended.
	require.NoError(t, os.WriteFile(path, []byte("working\n"), 0o600))
	_, err = CreateHookFile(root, "h1")
	require.NoError(t, err)
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "working\n", string(b))
}

// TestTightenPerms: state written by older daemons (0755 dirs, 0644 files)
// is made owner-only on startup, without following symlinks out of the
// state dir.
func TestTightenPerms(t *testing.T) {
	root := filepath.Join(t.TempDir(), "rex")
	dir := filepath.Join(root, "sessions", "old")
	testperm.MkdirAll(t, dir, 0o755)
	testperm.Loosen(t, root, 0o755)
	testperm.Loosen(t, filepath.Join(root, "sessions"), 0o755)
	for _, name := range []string{"meta.json", "transcript.log", "hooks.log"} {
		testperm.WriteFile(t, filepath.Join(dir, name), []byte("x"), 0o644)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	testperm.WriteFile(t, outside, []byte("x"), 0o644)
	require.NoError(t, os.Symlink(outside, filepath.Join(dir, "link")))
	testperm.WriteFile(t, filepath.Join(root, "sessions", "stray-file"), []byte("x"), 0o644)

	require.NoError(t, TightenPerms(root))

	requireMode(t, root, 0o700)
	requireMode(t, filepath.Join(root, "sessions"), 0o700)
	requireMode(t, dir, 0o700)
	for _, name := range []string{"meta.json", "transcript.log", "hooks.log"} {
		requireMode(t, filepath.Join(dir, name), 0o600)
	}
	requireMode(t, outside, 0o644) // the symlink target is not ours to change
	requireMode(t, filepath.Join(root, "sessions", "stray-file"), 0o600)
}

func TestTightenPerms_MissingRoot(t *testing.T) {
	require.NoError(t, TightenPerms(filepath.Join(t.TempDir(), "absent")))
}

func TestTightenPerms_RootIsAFile(t *testing.T) {
	root := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(root, nil, 0o600))
	require.ErrorContains(t, TightenPerms(root), "not a directory")
}

func TestPersist_RootIsAFile(t *testing.T) {
	root := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(root, nil, 0o600))
	_, err := OpenTranscript(root, "t1")
	require.Error(t, err)
	_, err = CreateHookFile(root, "h1")
	require.Error(t, err)
	require.Error(t, WriteMeta(root, &Session{ID: "m1"}))
}

func TestTightenPerms_NoSessionsDir(t *testing.T) {
	root := t.TempDir()
	testperm.Loosen(t, root, 0o755)
	require.NoError(t, TightenPerms(root))
	requireMode(t, root, 0o700)
}
