package export

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func writeTranscript(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "transcript.log")
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	return p
}

func session(tool string) protocol.SessionSummary {
	s := sampleSession()
	s.ToolID = tool
	return s
}

func TestBuild_fullScreenToolIsReplayed(t *testing.T) {
	p := writeTranscript(t, "\x1b[2J\x1b[Hone\r\n\x1b[2J\x1b[Htwo\r\n")
	d, err := Build(Options{Session: session("claude"), TranscriptPath: p})
	require.NoError(t, err)
	require.Equal(t, Version, d.Version)
	require.Equal(t, SourceFrames, d.Source)
	require.Equal(t, []string{"one", "two"}, texts(d.Frames))
	require.Empty(t, d.Turns)
}

func TestBuild_lineToolIsStripped(t *testing.T) {
	p := writeTranscript(t, "\x1b[32mhello\x1b[0m\r\nprogress 1%\rprogress 100%\r\n")
	d, err := Build(Options{Session: session("echo"), TranscriptPath: p})
	require.NoError(t, err)
	require.Len(t, d.Frames, 1)
	require.Equal(t, "hello\nprogress 100%", d.Frames[0].Text)
}

func TestBuild_emptyTranscriptHasNoFrames(t *testing.T) {
	for _, tool := range []string{"claude", "echo"} {
		d, err := Build(Options{Session: session(tool), TranscriptPath: writeTranscript(t, "")})
		require.NoError(t, err)
		require.Empty(t, d.Frames)
	}
}

func TestBuild_maxFramesKeepsTheLast(t *testing.T) {
	p := writeTranscript(t, "a\x1b[2J\x1b[Hb\x1b[2J\x1b[Hc\x1b[2J\x1b[Hd")
	d, err := Build(Options{Session: session("claude"), TranscriptPath: p, MaxFrames: 2})
	require.NoError(t, err)
	require.Equal(t, []string{"c", "d"}, texts(d.Frames))
	require.Equal(t, 2, d.FramesDropped)
}

func TestBuild_negativeMaxFrames(t *testing.T) {
	_, err := Build(Options{Session: session("claude"), TranscriptPath: writeTranscript(t, "x"), MaxFrames: -1})
	require.Error(t, err)
}

func TestBuild_missingTranscript(t *testing.T) {
	_, err := Build(Options{Session: session("claude"), TranscriptPath: filepath.Join(t.TempDir(), "none.log")})
	require.ErrorIs(t, err, ErrNoTranscript)
}

func TestBuild_largeTranscriptStreams(t *testing.T) {
	var b strings.Builder
	for range 20000 {
		b.WriteString("\x1b[2J\x1b[Hline of output that is repeated\r\n")
	}
	d, err := Build(Options{Session: session("claude"), TranscriptPath: writeTranscript(t, b.String())})
	require.NoError(t, err)
	require.Len(t, d.Frames, 1)
}

func TestWriteFile_refusesToOverwriteWithoutForce(t *testing.T) {
	p := filepath.Join(t.TempDir(), "out.json")
	require.NoError(t, WriteFile(p, []byte("one"), false))
	err := WriteFile(p, []byte("two"), false)
	require.True(t, errors.Is(err, ErrExists), "%v", err)
	got, err := os.ReadFile(p)
	require.NoError(t, err)
	require.Equal(t, "one", string(got))

	require.NoError(t, WriteFile(p, []byte("two"), true))
	got, err = os.ReadFile(p)
	require.NoError(t, err)
	require.Equal(t, "two", string(got))
}

func TestWriteFile_mode0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits")
	}
	p := filepath.Join(t.TempDir(), "out.json")
	require.NoError(t, WriteFile(p, []byte("x"), false))
	st, err := os.Stat(p)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), st.Mode().Perm())

	// --force tightens a file that was group readable.
	require.NoError(t, os.Chmod(p, 0o644)) //nolint:gosec // G302: the test needs a group-readable file to prove --force tightens it.
	require.NoError(t, WriteFile(p, []byte("y"), true))
	st, err = os.Stat(p)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), st.Mode().Perm())
}

func TestWriteFile_missingDirectory(t *testing.T) {
	err := WriteFile(filepath.Join(t.TempDir(), "no", "such", "out.json"), []byte("x"), false)
	require.Error(t, err)
	require.False(t, errors.Is(err, ErrExists))
}

func TestDefaultName(t *testing.T) {
	s := sampleSession()
	require.Equal(t, "fix-the-widget-1111.md", DefaultName(s, FormatMD))
	s.Slug = "we/ird na*me"
	require.Equal(t, "we-ird-na-me-1111.json", DefaultName(s, FormatJSON))
	s.Slug = ""
	require.Equal(t, "session-1111.txt", DefaultName(s, FormatTXT))
}
