package export

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestBuild_cleansSessionStrings(t *testing.T) {
	s := session("claude")
	s.Name = "n\x1b]0;t\x07a\u202eme\u0085\u009b"
	s.Description = "line\nbreak\x7f"
	s.CWD = "/work/\xc2\x9d52;c;QQ==\xc2\x9c"
	d, err := Build(Options{Session: s, TranscriptPath: writeTranscript(t, "x")})
	require.NoError(t, err)
	for _, f := range []Format{FormatJSON, FormatYAML, FormatMD, FormatTXT} {
		out, err := Render(d, f)
		require.NoError(t, err, f)
		noUnsafe(t, strings.ReplaceAll(string(out), "\n", ""))
	}
	require.Equal(t, "name", d.Session.Name)
	require.Equal(t, "line break", d.Session.Description)
}

func TestRender_yamlSurvivesTextYAMLRejects(t *testing.T) {
	d := sampleFrames()
	d.Frames = []Frame{{At: 1, Text: "nel\u0085 csi\u009b del\x7f nc\ufffe end"}}
	out, err := Render(d, FormatYAML)
	require.NoError(t, err)
	var generic map[string]any
	require.NoError(t, yaml.Unmarshal(out, &generic))
	frame := generic["frames"].([]any)[0].(map[string]any)
	require.Equal(t, d.Frames[0].Text, frame["text"])

	var fromJSON Document
	j, err := Render(d, FormatJSON)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(j, &fromJSON))
	require.Equal(t, d.Frames, fromJSON.Frames)
}

func TestBuild_maxFramesBoundsFramesAndLineText(t *testing.T) {
	d, err := Build(Options{Session: session("claude"), TranscriptPath: writeTranscript(t, longTranscript()), MaxFrames: 5})
	require.NoError(t, err)
	require.Len(t, d.Frames, 5)
	require.Positive(t, d.FramesDropped)
	require.True(t, strings.HasSuffix(d.Frames[4].Text, "line 4999"))

	d, err = Build(Options{Session: session("echo"), TranscriptPath: writeTranscript(t, longTranscript()), MaxFrames: 5})
	require.NoError(t, err)
	require.Len(t, d.Frames, 5)
	require.Equal(t, 25-5, d.FramesDropped)
	require.True(t, strings.HasSuffix(d.Frames[4].Text, "line 4999"))

	d, err = Build(Options{Session: session("echo"), TranscriptPath: writeTranscript(t, "a\r\nb\r\n")})
	require.NoError(t, err)
	require.Len(t, d.Frames, 1)
	require.Equal(t, "a\nb", d.Frames[0].Text)
}

func TestBuild_maxFramesKeepsTheLastTurns(t *testing.T) {
	home := fakeClaudeHome(t, "p", resumeID, syntheticLog)
	d := buildWithLog(t, Options{Session: session("claude"), Home: home, MaxFrames: 1,
		TranscriptPath: filepath.Join(t.TempDir(), "n.log")})
	require.Len(t, d.Turns, 1)
	require.Equal(t, "Done.", d.Turns[0].Text)
	require.Equal(t, 3, d.TurnsDropped)
	for _, f := range []Format{FormatMD, FormatTXT, FormatJSON, FormatYAML} {
		out, err := Render(d, f)
		require.NoError(t, err)
		require.Contains(t, string(out), "3")
	}
}

func TestBuild_readsOnlyUpToTheSizeAtOpen(t *testing.T) {
	p := writeTranscript(t, "before\r\n")
	afterStat = func() {
		f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o600)
		require.NoError(t, err)
		_, err = f.WriteString("after\r\n")
		require.NoError(t, err)
		require.NoError(t, f.Close())
	}
	t.Cleanup(func() { afterStat = func() {} })
	d, err := Build(Options{Session: session("claude"), TranscriptPath: p})
	require.NoError(t, err)
	require.Equal(t, []string{"before"}, texts(d.Frames))
}

func TestWriteFile_forceDoesNotFollowASymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	require.NoError(t, os.WriteFile(target, []byte("keep"), 0o600))
	link := filepath.Join(dir, "out.json")
	require.NoError(t, os.Symlink(target, link))

	err := WriteFile(link, []byte("new"), true)
	require.ErrorIs(t, err, ErrSymlink)
	b, rerr := os.ReadFile(target)
	require.NoError(t, rerr)
	require.Equal(t, "keep", string(b))

	err = WriteFile(link, []byte("new"), false)
	require.ErrorIs(t, err, ErrExists)
}

func TestWriteFile_forceLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "out.json")
	require.NoError(t, WriteFile(p, []byte("one"), true))
	require.NoError(t, WriteFile(p, []byte("two"), true))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	b, err := os.ReadFile(p)
	require.NoError(t, err)
	require.Equal(t, "two", string(b))
	require.Error(t, WriteFile(filepath.Join(dir, "missing", "x"), []byte("x"), true))
}

func TestReadAgentLog_symlinkedProjectsRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	real := fakeClaudeHome(t, "p", resumeID, syntheticLog)
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".claude"), 0o700))
	require.NoError(t, os.Symlink(filepath.Join(real, ".claude", "projects"), filepath.Join(home, ".claude", "projects")))
	turns, err := ReadAgentLog(home, resumeID)
	require.NoError(t, err)
	require.Len(t, turns, 4)
}

func TestReadAgentLog_unreadableSubdirIsSkipped(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions and a non-root user")
	}
	home := fakeClaudeHome(t, "b-good", resumeID, syntheticLog)
	bad := filepath.Join(home, ".claude", "projects", "a-bad")
	require.NoError(t, os.MkdirAll(bad, 0o700))
	require.NoError(t, os.Chmod(bad, 0o000))       //nolint:gosec // G302: the test needs an unreadable folder.
	t.Cleanup(func() { _ = os.Chmod(bad, 0o700) }) //nolint:gosec // G302: restore so TempDir can clean up.
	turns, err := ReadAgentLog(home, resumeID)
	require.NoError(t, err)
	require.Len(t, turns, 4)
}

func TestReadLine_skipsOversizeLines(t *testing.T) {
	in := "short\n" + strings.Repeat("x", 100000) + "\nafter\n"
	br := bufio.NewReaderSize(strings.NewReader(in), 16)
	var got []string
	for {
		line, err := readLine(br, 1000)
		got = append(got, string(line))
		if err != nil {
			break
		}
	}
	require.Equal(t, []string{"short\n", "", "after\n", ""}, got)
}

func TestParseAgentLog_tooManyMessages(t *testing.T) {
	line := `{"type":"user","message":{"content":"x"}}` + "\n"
	_, err := parseAgentLog(strings.NewReader(strings.Repeat(line, maxTurns+1)))
	require.ErrorIs(t, err, ErrTooLarge)
}
