package export

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const resumeID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"

// syntheticLog is a hand-written Claude-style JSONL conversation.
var syntheticLog = strings.Join([]string{
	`{"type":"summary","summary":"not a message"}`,
	`{"type":"user","timestamp":"2026-01-02T03:04:06Z","message":{"role":"user","content":"fix the widget"}}`,
	`{"type":"user","isMeta":true,"message":{"role":"user","content":"caveat: generated"}}`,
	`{"type":"assistant","timestamp":"2026-01-02T03:04:09Z","message":{"role":"assistant","content":[` +
		`{"type":"thinking","thinking":"private reasoning"},` +
		`{"type":"text","text":"On it."},` +
		`{"type":"tool_use","id":"t1","name":"Edit","input":{"file_path":"/work/example/widget.go","old_string":"a","new_string":"b"}},` +
		`{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"go test ./...\nsecond line"}},` +
		`{"type":"tool_use","id":"t3","name":"Mystery","input":{"x":1}}]}}`,
	`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"edited"}]}}`,
	`{"type":"assistant","isSidechain":true,"message":{"role":"assistant","content":[{"type":"text","text":"subagent chatter"}]}}`,
	`{"type":"user","timestamp":"2026-01-02T03:05:00Z","message":{"role":"user","content":[{"type":"text","text":"thanks \u001b[31mred\u001b[0m"}]}}`,
	`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Done."}]}}`,
	"",
}, "\n")

func fakeClaudeHome(t *testing.T, project, id, content string) string {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".claude", "projects", project)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(content), 0o600))
	return home
}

func TestReadAgentLog_rolesTextAndToolSummaries(t *testing.T) {
	home := fakeClaudeHome(t, "-work-example", resumeID, syntheticLog)
	turns, err := ReadAgentLog(home, resumeID)
	require.NoError(t, err)
	require.Equal(t, []Turn{
		{Role: "user", Text: "fix the widget", At: "2026-01-02T03:04:06Z"},
		{Role: "assistant", Text: "On it.\n[tool] Edit /work/example/widget.go\n[tool] Bash go test ./... second line\n[tool] Mystery", At: "2026-01-02T03:04:09Z"},
		{Role: "user", Text: "thanks red", At: "2026-01-02T03:05:00Z"},
		{Role: "assistant", Text: "Done."},
	}, turns)
}

func TestReadAgentLog_findsTheFileInAnyProjectFolder(t *testing.T) {
	home := fakeClaudeHome(t, "-another-project", resumeID, syntheticLog)
	// A different conversation with a different id is not picked up.
	other := filepath.Join(home, ".claude", "projects", "-other")
	require.NoError(t, os.MkdirAll(other, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(other, "11111111-2222-4333-8444-555555555555.jsonl"), []byte(`{"type":"user","message":{"content":"wrong"}}`+"\n"), 0o600))
	turns, err := ReadAgentLog(home, resumeID)
	require.NoError(t, err)
	require.NotEmpty(t, turns)
	require.NotContains(t, turns[0].Text, "wrong")
}

func TestReadAgentLog_missing(t *testing.T) {
	_, err := ReadAgentLog(t.TempDir(), resumeID)
	require.ErrorIs(t, err, ErrNoAgentLog)

	home := fakeClaudeHome(t, "p", "11111111-2222-4333-8444-555555555555", "{}\n")
	_, err = ReadAgentLog(home, resumeID)
	require.ErrorIs(t, err, ErrNoAgentLog)
}

func TestReadAgentLog_rejectsBadResumeID(t *testing.T) {
	home := t.TempDir()
	for _, id := range []string{"", "../../etc/passwd", "not-a-uuid", resumeID + "/../x"} {
		_, err := ReadAgentLog(home, id)
		require.Error(t, err, id)
		require.False(t, errors.Is(err, ErrNoAgentLog), id)
	}
}

func TestReadAgentLog_malformedLineIsAnError(t *testing.T) {
	home := fakeClaudeHome(t, "p", resumeID, `{"type":"user","message":{"content":"ok"}}`+"\nnot json\n"+`{"type":"user","message":{"content":"later"}}`+"\n")
	_, err := ReadAgentLog(home, resumeID)
	require.ErrorContains(t, err, "line 2")
}

// A running session may have written half a line at the end.
func TestReadAgentLog_partialLastLineIsIgnored(t *testing.T) {
	home := fakeClaudeHome(t, "p", resumeID, `{"type":"user","message":{"content":"ok"}}`+"\n"+`{"type":"assistant","message":{"con`)
	turns, err := ReadAgentLog(home, resumeID)
	require.NoError(t, err)
	require.Equal(t, []Turn{{Role: "user", Text: "ok"}}, turns)
}

func TestReadAgentLog_longToolArgumentIsCut(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"` + strings.Repeat("x", 500) + `"}}]}}` + "\n"
	home := fakeClaudeHome(t, "p", resumeID, line)
	turns, err := ReadAgentLog(home, resumeID)
	require.NoError(t, err)
	require.Len(t, turns, 1)
	require.Less(t, len([]rune(turns[0].Text)), 130)
	require.True(t, strings.HasSuffix(turns[0].Text, "…"))
}

func TestReadAgentLog_invalidUTF8InTextIsReplaced(t *testing.T) {
	home := fakeClaudeHome(t, "p", resumeID, "{\"type\":\"user\",\"message\":{\"content\":\"a\xffb\"}}\n")
	turns, err := ReadAgentLog(home, resumeID)
	require.NoError(t, err)
	require.Equal(t, "a�b", turns[0].Text)
}

func buildWithLog(t *testing.T, o Options) Document {
	t.Helper()
	o.FromAgentLog = true
	d, err := Build(o)
	require.NoError(t, err)
	return d
}

func TestBuild_fromAgentLog(t *testing.T) {
	home := fakeClaudeHome(t, "p", resumeID, syntheticLog)
	d := buildWithLog(t, Options{Session: session("claude"), Home: home, TranscriptPath: filepath.Join(t.TempDir(), "never-read.log")})
	require.Equal(t, SourceAgentLog, d.Source)
	require.Empty(t, d.Frames)
	require.Len(t, d.Turns, 4)
	require.Empty(t, d.Notice)
}

func TestBuild_fromAgentLogMissingFallsBackToFrames(t *testing.T) {
	d := buildWithLog(t, Options{Session: session("claude"), Home: t.TempDir(), TranscriptPath: writeTranscript(t, "screen text\r\n")})
	require.Equal(t, SourceFrames, d.Source)
	require.Equal(t, []string{"screen text"}, texts(d.Frames))
	require.Contains(t, d.Notice, "agent log")
	require.Contains(t, d.Notice, "showing frames")
}

func TestBuild_fromAgentLogWithoutResumeIDFallsBack(t *testing.T) {
	s := session("claude")
	s.ResumeID = ""
	d := buildWithLog(t, Options{Session: s, Home: t.TempDir(), TranscriptPath: writeTranscript(t, "x")})
	require.Equal(t, SourceFrames, d.Source)
	require.Contains(t, d.Notice, "no resume id")
}

func TestBuild_fromAgentLogOtherToolFallsBack(t *testing.T) {
	d := buildWithLog(t, Options{Session: session("codex"), Home: t.TempDir(), TranscriptPath: writeTranscript(t, "x")})
	require.Equal(t, SourceFrames, d.Source)
	require.Contains(t, d.Notice, "codex")
}

func TestBuild_fromAgentLogFallbackNeedsTranscript(t *testing.T) {
	_, err := Build(Options{Session: session("claude"), Home: t.TempDir(), FromAgentLog: true,
		TranscriptPath: filepath.Join(t.TempDir(), "none.log")})
	require.ErrorIs(t, err, ErrNoTranscript)
}

func TestBuild_agentLogDefaultsHomeToHOME(t *testing.T) {
	home := fakeClaudeHome(t, "p", resumeID, syntheticLog)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	d := buildWithLog(t, Options{Session: session("claude"), TranscriptPath: filepath.Join(t.TempDir(), "n.log")})
	require.Equal(t, SourceAgentLog, d.Source)
}
