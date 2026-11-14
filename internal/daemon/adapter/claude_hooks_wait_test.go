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
		// Any other main-thread tool ends a stale wait (a failed call fired no Post).
		{"Read", "working\nwait-\n"},
		{"Edit", "working\nwait-\n"},
		{"mcp__x__Bash", "working\nwait-\n"},
		{"Bashful", "working\nwait-\n"},
		{"", "working\nwait-\n"},
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
		want := "working\n"
		if tool == "Task" {
			want = "working\nnest+\n" // a nested subagent follows
		}
		require.Equal(t, want, got, tool)
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
	require.Equal(t, "working\nwait-\n", got)
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
	for _, ev := range []string{"Notification", "Stop"} {
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
	for _, c := range []struct {
		ev, stdin string
		clears    bool
	}{
		{"Stop", `{"session_id":"s"}`, true},
		{"StopFailure", `{"session_id":"s","error":"rate_limit"}`, true},
		{"Notification", `{"notification_type":"idle_prompt"}`, true},
		{"SessionStart", `{"session_id":"s","source":"startup"}`, true},
		// A question keeps the wait for after the answer; a compaction
		// is not the end of the turn; an informational notice is nothing.
		{"Notification", `{"notification_type":"permission_prompt"}`, false},
		{"Notification", `{"notification_type":"elicitation_response"}`, false},
		{"SessionStart", `{"session_id":"s","source":"compact"}`, false},
	} {
		a, path, _ := waitingAdapter(t)
		runHook(t, cmds["PreToolUse"], path, `{"tool_name":"Bash"}`)
		got, _ := a.Waiting()
		require.Equal(t, protocol.WaitShell, got, c.ev)
		runHook(t, cmds[c.ev], path, c.stdin)
		runHook(t, cmds["PreToolUse"], path, `{"agent_id":"sub-1","agent_type":"x","tool_name":"Read"}`) // a lone working word
		got, _ = a.Waiting()
		if c.clears {
			require.Empty(t, got, c.stdin)
		} else {
			require.Equal(t, protocol.WaitShell, got, c.stdin)
		}
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
	appendLine(t, path, "agent- sub-1") // SubagentStop comes before the Task's own Post
	runHook(t, cmds["PostToolUse"], path, `{"tool_name":"Task"}`)
	got, _ = a.Waiting()
	require.Empty(t, got)
}

func TestClaudeHooks_WaitClearedByStaleWorking(t *testing.T) {
	a, path, _ := waitingAdapter(t)
	appendLine(t, path, "working")
	appendLine(t, path, "wait+ shell")
	require.Equal(t, protocol.StateWorking, a.Detect(nil, hookStaleWorking), "an open wait gets the long bound")
	require.Equal(t, protocol.StateNeedsInput, a.Detect(nil, subagentQuiet))
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

// A failed tool call fires PostToolUseFailure, not PostToolUse.
func TestClaudeHooks_FailureClearsWait(t *testing.T) {
	cmds := hookCommands(t)
	require.Contains(t, cmds, "PostToolUseFailure")
	require.Equal(t, "working\nwait-\n", hookFileAfter(t, "PostToolUseFailure", `{"session_id":"s","tool_name":"Bash"}`))
	require.Equal(t, "working\nagent~ sub-7\n",
		hookFileAfter(t, "PostToolUseFailure", `{"agent_id":"sub-7","agent_type":"Explore","tool_name":"Bash"}`))

	a, path, _ := waitingAdapter(t)
	runHook(t, cmds["PreToolUse"], path, `{"tool_name":"Bash"}`)
	got, _ := a.Waiting()
	require.Equal(t, protocol.WaitShell, got)
	runHook(t, cmds["PostToolUseFailure"], path, `{"tool_name":"Bash"}`)
	got, _ = a.Waiting()
	require.Empty(t, got)
}

// Fallbacks bound the wait even if no Post event ever fires.
func TestClaudeHooks_NonWaitPreAndPromptClearWait(t *testing.T) {
	cmds := hookCommands(t)
	a, path, _ := waitingAdapter(t)
	runHook(t, cmds["PreToolUse"], path, `{"tool_name":"Bash"}`)
	runHook(t, cmds["PreToolUse"], path, `{"tool_name":"Read"}`)
	got, _ := a.Waiting()
	require.Empty(t, got, "a non-wait tool ends the wait")

	runHook(t, cmds["PreToolUse"], path, `{"tool_name":"WebFetch"}`)
	got, _ = a.Waiting()
	require.Equal(t, protocol.WaitWeb, got)
	runHook(t, cmds["UserPromptSubmit"], path, `{"prompt":"hi"}`)
	got, _ = a.Waiting()
	require.Empty(t, got, "a new prompt ends the wait")

	// A subagent's non-wait Pre does not end the main thread's wait.
	runHook(t, cmds["PreToolUse"], path, `{"tool_name":"Bash"}`)
	runHook(t, cmds["PreToolUse"], path, `{"agent_id":"sub-1","agent_type":"Explore","tool_name":"Read"}`)
	got, _ = a.Waiting()
	require.Equal(t, protocol.WaitShell, got)
}

// Several main-thread Task calls fan out: the wait lasts until the last
// subagent ends.
func TestClaudeHooks_ParallelSubagentsKeepWaiting(t *testing.T) {
	a, path, now := waitingAdapter(t)
	for _, l := range []string{"working", "wait+ subagent", "wait+ subagent", "agent+ a", "agent+ b", "agent- a", "working", "wait-"} {
		appendLine(t, path, l)
	}
	got, _ := a.Waiting()
	require.Equal(t, protocol.WaitSubagent, got, "b is still running")
	appendLine(t, path, "agent- b")
	got, _ = a.Waiting()
	require.Empty(t, got, "the last one ended")

	// Without a stop, the quiet timeout ends it.
	a2, path2, now2 := waitingAdapter(t)
	for _, l := range []string{"wait+ subagent", "agent+ a", "wait-"} {
		appendLine(t, path2, l)
	}
	got, _ = a2.Waiting()
	require.Equal(t, protocol.WaitSubagent, got)
	*now2 = now2.Add(subagentQuiet + time.Second)
	got, _ = a2.Waiting()
	require.Empty(t, got)
	_ = now

	// A later wait+ starts fresh.
	a3, path3, _ := waitingAdapter(t)
	for _, l := range []string{"wait+ subagent", "agent+ a", "wait-", "wait+ shell", "agent- a"} {
		appendLine(t, path3, l)
	}
	got, _ = a3.Waiting()
	require.Equal(t, protocol.WaitShell, got)
}

// Other waits end on wait- even with subagents running.
func TestClaudeHooks_ShellWaitEndsWithSubagentsRunning(t *testing.T) {
	a, path, _ := waitingAdapter(t)
	for _, l := range []string{"agent+ a", "wait+ shell", "wait-"} {
		appendLine(t, path, l)
	}
	got, _ := a.Waiting()
	require.Empty(t, got)
}
