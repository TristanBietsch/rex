package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

var unsafeIDs = []string{"", ".", "..", "../..", "../x", "a/b", "/etc", "a/../b"}

// TestSessionPaths_RejectUnsafeIDs is the S-04 regression: every helper that
// builds a path from a session id refuses ids that would leave
// <root>/sessions, so a client-supplied "../.." can't read or create files
// elsewhere.
func TestSessionPaths_RejectUnsafeIDs(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "state")
	require.NoError(t, os.MkdirAll(root, 0o700))
	// What "../.." resolves to from <root>/sessions/<id>: the state root's parent.
	require.NoError(t, os.WriteFile(filepath.Join(base, "transcript.log"), []byte("secret"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(base, "meta.json"), []byte(`{"id":"../.."}`), 0o600))

	for _, id := range unsafeIDs {
		tail, err := TranscriptTail(root, id, 1024)
		require.ErrorIs(t, err, ErrUnsafeSessionID, "TranscriptTail %q", id)
		require.Empty(t, tail)

		_, err = LoadMeta(root, id)
		require.ErrorIs(t, err, ErrUnsafeSessionID, "LoadMeta %q", id)

		_, err = OpenTranscript(root, id)
		require.ErrorIs(t, err, ErrUnsafeSessionID, "OpenTranscript %q", id)

		require.ErrorIs(t, AppendTranscript(root, id, []byte("x")), ErrUnsafeSessionID, "AppendTranscript %q", id)

		_, err = HookFile(root, id)
		require.ErrorIs(t, err, ErrUnsafeSessionID, "HookFile %q", id)

		_, err = CreateHookFile(root, id)
		require.ErrorIs(t, err, ErrUnsafeSessionID, "CreateHookFile %q", id)

		require.ErrorIs(t, ValidateSessionID(id), ErrUnsafeSessionID, "ValidateSessionID %q", id)
	}
	b, err := os.ReadFile(filepath.Join(base, "transcript.log"))
	require.NoError(t, err)
	require.Equal(t, "secret", string(b), "nothing outside the state dir was appended to")
}

func TestSessionPaths_AcceptNormalIDs(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"7d4f3c8a-1234-4abc-89ab-cdefabcdef01", "fake-sess", "a", "...x"} {
		require.NoError(t, ValidateSessionID(id), "id %q", id)
		p, err := HookFile(root, id)
		require.NoError(t, err)
		require.Equal(t, filepath.Join(root, "sessions", id, "hooks.log"), p)
	}
}
