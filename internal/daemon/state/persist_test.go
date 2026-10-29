package state

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestWriteAndLoadMeta(t *testing.T) {
	dir := t.TempDir()
	sess := &Session{
		ID: "id1", ShortID: "id1", ToolID: "echo", ModelID: "short",
		Slug: "ok", State: protocol.StateDone, StartedAt: time.Now().UTC(),
	}
	require.NoError(t, WriteMeta(dir, sess))
	got, err := LoadMeta(dir, sess.ID)
	require.NoError(t, err)
	require.Equal(t, sess.ID, got.ID)
	require.Equal(t, protocol.StateDone, got.State)
}

// Transcripts and meta are private to the user, and SecureSessionsDir
// tightens a sessions dir an older daemon left world-readable.
func TestPersist_OwnerOnlyPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "sessions"), 0o755))
	require.NoError(t, SecureSessionsDir(root))

	sess := &Session{ID: "p1", ShortID: "p1", State: protocol.StateDone, StartedAt: time.Now().UTC()}
	require.NoError(t, WriteMeta(root, sess))
	tf, err := OpenTranscript(root, "p1")
	require.NoError(t, err)
	require.NoError(t, tf.Close())

	for path, want := range map[string]os.FileMode{
		filepath.Join(root, "sessions"):                         0o700,
		filepath.Join(root, "sessions", "p1"):                   0o700,
		filepath.Join(root, "sessions", "p1", "meta.json"):      0o600,
		filepath.Join(root, "sessions", "p1", "transcript.log"): 0o600,
	} {
		info, err := os.Stat(path)
		require.NoError(t, err)
		require.Equal(t, want, info.Mode().Perm(), path)
	}
}

func TestAppendTranscript(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, AppendTranscript(dir, "id1", []byte("line1\n")))
	require.NoError(t, AppendTranscript(dir, "id1", []byte("line2\n")))
	b, err := os.ReadFile(filepath.Join(dir, "sessions", "id1", "transcript.log"))
	require.NoError(t, err)
	require.Equal(t, "line1\nline2\n", string(b))
}

func TestTranscriptFile(t *testing.T) {
	dir := t.TempDir()
	tf, err := OpenTranscript(dir, "id1")
	require.NoError(t, err)
	require.NoError(t, tf.Write([]byte("a")))
	require.NoError(t, tf.Write([]byte("b")))
	require.NoError(t, tf.Close())
	b, err := os.ReadFile(filepath.Join(dir, "sessions", "id1", "transcript.log"))
	require.NoError(t, err)
	require.Equal(t, "ab", string(b))
}

func TestTranscriptTail(t *testing.T) {
	dir := t.TempDir()

	// Missing transcript → (nil, nil).
	b, err := TranscriptTail(dir, "missing", 1024)
	require.NoError(t, err)
	require.Nil(t, b)

	// Smaller than max → return whole file.
	require.NoError(t, AppendTranscript(dir, "id1", []byte("hello")))
	b, err = TranscriptTail(dir, "id1", 1024)
	require.NoError(t, err)
	require.Equal(t, "hello", string(b))

	// Larger than max → return last N bytes.
	require.NoError(t, AppendTranscript(dir, "id2", []byte("abcdefghijklmnop")))
	b, err = TranscriptTail(dir, "id2", 4)
	require.NoError(t, err)
	require.Equal(t, "mnop", string(b))
}

func TestLoadAllRecoversCrashed(t *testing.T) {
	dir := t.TempDir()
	sess := &Session{
		ID: "id1", ShortID: "id1", State: protocol.StateWorking,
		StartedAt: time.Now().UTC(),
	}
	require.NoError(t, WriteMeta(dir, sess))

	loaded, err := LoadAll(dir)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	require.Equal(t, protocol.StateCrashed, loaded[0].State,
		"previously-working sessions must reload as crashed")
}

// A WriteMeta interrupted between create and rename leaves a temp file; the
// next startup removes it and still loads the session.
func TestLoadAll_RemovesStaleMetaTemps(t *testing.T) {
	dir := t.TempDir()
	sess := &Session{ID: "id1", ShortID: "id1", State: protocol.StateDone, StartedAt: time.Now().UTC()}
	require.NoError(t, WriteMeta(dir, sess))
	stale := filepath.Join(dir, "sessions", "id1", "meta-123.tmp")
	require.NoError(t, os.WriteFile(stale, []byte(`{"id":`), 0o600))

	loaded, err := LoadAll(dir)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	require.NoFileExists(t, stale)
}

// The summarizer reads the tail of a live session; bytes written through an
// open TranscriptFile must be visible before Close.
func TestTranscriptFile_TailVisibleBeforeClose(t *testing.T) {
	dir := t.TempDir()
	tf, err := OpenTranscript(dir, "live")
	require.NoError(t, err)
	defer tf.Close()
	require.NoError(t, tf.Write([]byte("still running")))
	b, err := TranscriptTail(dir, "live", 1024)
	require.NoError(t, err)
	require.Equal(t, "still running", string(b))
}
