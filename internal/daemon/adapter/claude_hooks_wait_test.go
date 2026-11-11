package adapter

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func hookFileAfter(t *testing.T, event, stdin string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hooks.log")
	runHook(t, hookCommands(t)[event], path, stdin)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(raw)
}

func TestClaudeHookArgs_PreToolUseWaitLine(t *testing.T) {
	cases := []struct {
		tool string
		want string
	}{
		{"Bash", "working\nwait+ shell\n"},
		{"Task", "working\nwait+ subagent\n"},
		{"Agent", "working\nwait+ subagent\n"},
		{"WebFetch", "working\nwait+ web\n"},
		{"WebSearch", "working\nwait+ web\n"},
		{"Read", "working\n"},
		{"Edit", "working\n"},
		{"mcp__x__Bash", "working\n"},
		{"Bashful", "working\n"},
		{"", "working\n"},
	}
	for _, c := range cases {
		t.Run(c.tool, func(t *testing.T) {
			got := hookFileAfter(t, "PreToolUse", `{"session_id":"s","hook_event_name":"PreToolUse","tool_name":"`+c.tool+`","tool_input":{"command":"ls"}}`)
			require.Equal(t, c.want, got)
		})
	}
}

// A subagent's tool call (the JSON carries an agent_id) never marks the
// main session as waiting, whatever the tool.
func TestClaudeHookArgs_SubagentPreToolUseWritesNoWait(t *testing.T) {
	for _, tool := range []string{"Bash", "Task", "WebFetch"} {
		got := hookFileAfter(t, "PreToolUse",
			`{"session_id":"s","agent_id":"sub-7","agent_type":"Explore","tool_name":"`+tool+`","tool_input":{}}`)
		require.Equal(t, "working\n", got, tool)
	}
	// Pretty-printed JSON too.
	got := hookFileAfter(t, "PreToolUse", "{\n  \"session_id\": \"s\",\n  \"agent_id\": \"sub-7\",\n  \"tool_name\": \"Bash\"\n}\n")
	require.Equal(t, "working\n", got)
}

// Only the top-level tool_name decides: a field of the same name deeper in
// tool_input does not turn a Read into a wait.
func TestClaudeHookArgs_PreToolUseTopLevelToolName(t *testing.T) {
	got := hookFileAfter(t, "PreToolUse",
		`{"session_id":"s","tool_name":"Read","tool_input":{"a":1,"tool_name":"Bash"}}`)
	require.Equal(t, "working\n", got)
	got = hookFileAfter(t, "PreToolUse", "{\n  \"session_id\": \"s\",\n\t\"tool_name\": \"Bash\"\n}\n")
	require.Equal(t, "working\nwait+ shell\n", got)
	// A quoted agent_id inside a string value is not a subagent call.
	got = hookFileAfter(t, "PreToolUse",
		`{"session_id":"s","tool_name":"Bash","tool_input":{"command":"echo \"agent_id\": \"x\""}}`)
	require.Equal(t, "working\nwait+ shell\n", got)
}

func TestClaudeHookArgs_PostToolUseClearsWait(t *testing.T) {
	require.Equal(t, "working\nwait-\n", hookFileAfter(t, "PostToolUse", `{"session_id":"s","tool_name":"Bash"}`))
	require.Equal(t, "working\nwait-\n", hookFileAfter(t, "PostToolUse", `{"session_id":"s","tool_name":"Read"}`))
	require.Equal(t, "working\nagent~ sub-7\n",
		hookFileAfter(t, "PostToolUse", `{"session_id":"s","agent_id":"sub-7","agent_type":"Explore","tool_name":"Bash"}`),
		"a subagent's call neither sets nor clears the main wait")
}

func TestClaudeHookArgs_OtherEventsWriteNoWaitLines(t *testing.T) {
	for _, ev := range []string{"UserPromptSubmit", "Notification", "Stop"} {
		require.NotContains(t, hookFileAfter(t, ev, `{"tool_name":"Bash"}`), "wait", ev)
	}
}

func waitingAdapter(t *testing.T) (*ClaudeHooks, string, *time.Time) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hooks.log")
	a := NewClaudeHooks(path)
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return now }
	return a, path, &now
}

func TestClaudeHooks_WaitSetAndCleared(t *testing.T) {
	a, path, now := waitingAdapter(t)
	kind, since := a.Waiting()
	require.Empty(t, kind)
	require.True(t, since.IsZero())

	appendLine(t, path, "working")
	appendLine(t, path, "wait+ shell")
	kind, since = a.Waiting()
	require.Equal(t, protocol.WaitShell, kind)
	require.True(t, since.Equal(*now))

	*now = now.Add(time.Minute)
	appendLine(t, path, "working")
	appendLine(t, path, "wait-")
	kind, since = a.Waiting()
	require.Empty(t, kind)
	require.True(t, since.IsZero())
}

func TestClaudeHooks_WaitKinds(t *testing.T) {
	for _, k := range []string{protocol.WaitShell, protocol.WaitSubagent, protocol.WaitWeb} {
		a, path, _ := waitingAdapter(t)
		appendLine(t, path, "wait+ "+k)
		got, _ := a.Waiting()
		require.Equal(t, k, got)
	}
	// An unknown or missing kind is ignored (a wait line can only come from
	// our own hook, but the file is plain text).
	for _, line := range []string{"wait+", "wait+ teleport", "wait+ shell;x"} {
		a, path, _ := waitingAdapter(t)
		appendLine(t, path, line)
		got, _ := a.Waiting()
		require.Empty(t, got, line)
	}
}

// A second wait+ replaces the first (one slot): last writer wins.
func TestClaudeHooks_WaitReplaced(t *testing.T) {
	a, path, now := waitingAdapter(t)
	appendLine(t, path, "wait+ shell")
	_, _ = a.Waiting()
	*now = now.Add(5 * time.Second)
	appendLine(t, path, "wait+ web")
	kind, since := a.Waiting()
	require.Equal(t, protocol.WaitWeb, kind)
	require.True(t, since.Equal(*now))
}

// The wait ends with the turn and with any move out of working.
func TestClaudeHooks_WaitClearedByLeavingWorking(t *testing.T) {
	for _, word := range []string{"needs_input"} { // Stop, Notification and SessionStart all write it
		a, path, _ := waitingAdapter(t)
		appendLine(t, path, "working")
		appendLine(t, path, "wait+ shell")
		got, _ := a.Waiting()
		require.Equal(t, protocol.WaitShell, got)
		appendLine(t, path, word)
		got, _ = a.Waiting()
		require.Empty(t, got, word)
		// A later working word does not bring the wait back.
		appendLine(t, path, "working")
		got, _ = a.Waiting()
		require.Empty(t, got, word)
	}
}

func TestClaudeHooks_WaitClearedByEvents(t *testing.T) {
	cmds := hookCommands(t)
	for _, ev := range []string{"Stop", "Notification", "SessionStart"} {
		a, path, _ := waitingAdapter(t)
		runHook(t, cmds["PreToolUse"], path, `{"tool_name":"Bash"}`)
		got, _ := a.Waiting()
		require.Equal(t, protocol.WaitShell, got, ev)
		runHook(t, cmds[ev], path, `{"session_id":"s","source":"compact"}`)
		got, _ = a.Waiting()
		require.Empty(t, got, ev)
	}
}

// A subagent's tool calls (working + agent~) leave a main-thread Task wait
// alone.
func TestClaudeHooks_SubagentActivityKeepsMainWait(t *testing.T) {
	a, path, _ := waitingAdapter(t)
	cmds := hookCommands(t)
	runHook(t, cmds["PreToolUse"], path, `{"tool_name":"Task","tool_input":{}}`)
	runHook(t, cmds["PreToolUse"], path, `{"agent_id":"sub-1","agent_type":"Explore","tool_name":"Bash"}`)
	runHook(t, cmds["PostToolUse"], path, `{"agent_id":"sub-1","agent_type":"Explore","tool_name":"Bash"}`)
	got, _ := a.Waiting()
	require.Equal(t, protocol.WaitSubagent, got)
	runHook(t, cmds["PostToolUse"], path, `{"tool_name":"Task"}`)
	got, _ = a.Waiting()
	require.Empty(t, got)
}

func TestClaudeHooks_WaitClearedByStaleWorking(t *testing.T) {
	a, path, _ := waitingAdapter(t)
	appendLine(t, path, "working")
	appendLine(t, path, "wait+ shell")
	require.Equal(t, protocol.StateNeedsInput, a.Detect(nil, hookStaleWorking))
	got, _ := a.Waiting()
	require.Empty(t, got)
}

func TestClaudeHooks_WaitClearedByTruncation(t *testing.T) {
	a, path, _ := waitingAdapter(t)
	appendLine(t, path, "wait+ shell")
	got, _ := a.Waiting()
	require.Equal(t, protocol.WaitShell, got)
	require.NoError(t, os.WriteFile(path, nil, 0o600))
	got, _ = a.Waiting()
	require.Empty(t, got)
}

func TestClaudeHooks_ImplementsWaitReporter(t *testing.T) {
	var _ WaitReporter = (*ClaudeHooks)(nil)
}
