package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/protocol"
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

func TestAppendTranscript(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, AppendTranscript(dir, "id1", []byte("line1\n")))
	require.NoError(t, AppendTranscript(dir, "id1", []byte("line2\n")))
	b, err := os.ReadFile(filepath.Join(dir, "sessions", "id1", "transcript.log"))
	require.NoError(t, err)
	require.Equal(t, "line1\nline2\n", string(b))
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
