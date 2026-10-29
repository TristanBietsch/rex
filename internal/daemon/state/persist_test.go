package state

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

// A tail cut from the middle of the transcript is replayed into a terminal
// emulator on attach: it must not start inside an escape or a rune.
func TestTranscriptTail_SafeBoundary(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name, content string
		max           int
		want          string
	}{
		{"mid_escape", "AAAA\x1b[31mred\x1b[0m\r\nnext", 17, "\x1b[0m\r\nnext"},
		{"after_newline", "abc\ndef", 5, "def"},
		{"mid_rune", "ééé", 5, "éé"},
		{"whole_file_untouched", "\x1b[31mred", 64, "\x1b[31mred"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, AppendTranscript(dir, tc.name, []byte(tc.content)))
			b, err := TranscriptTail(dir, tc.name, tc.max)
			require.NoError(t, err)
			require.Equal(t, tc.want, string(b))
		})
	}
}

func TestTranscriptTail_RejectsUnsafeID(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"", "..", "../escape", "a/b"} {
		_, err := TranscriptTail(dir, id, 1024)
		require.ErrorIs(t, err, ErrUnsafeSessionID, "id %q", id)
	}
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

// meta.json written by an older daemon (or by hand) can hold escapes and
// newlines in display fields; they must not reach the board.
func TestLoadMeta_SanitizesDisplayFields(t *testing.T) {
	dir := t.TempDir()
	sess := &Session{
		ID: "id1", ShortID: "id1", Slug: "ok", State: protocol.StateDone, StartedAt: time.Now().UTC(),
		Title:       "fix\x1b[2J the\nbuild‮",
		Fleet:       strings.Repeat("f", 100),
		Name:        "my\tname\x07",
		Description: "line one\r\nline two",
		LastLine:    "\x1b]0;pwned\x07done",
		CWD:         "/tmp/x",
	}
	require.NoError(t, WriteMeta(dir, sess))

	got, err := LoadMeta(dir, "id1")
	require.NoError(t, err)
	require.Equal(t, "fix[2J the build", got.Title)
	require.Equal(t, strings.Repeat("f", 64), got.Fleet)
	require.Equal(t, "my name", got.Name)
	require.Equal(t, "line one line two", got.Description)
	require.Equal(t, "]0;pwneddone", got.LastLine)
	require.Equal(t, "ok", got.Slug)
	require.Equal(t, "/tmp/x", got.CWD)
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
