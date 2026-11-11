package adapter

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func appendLine(t *testing.T, path, line string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString(line + "\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())
}

func TestClaudeHooks_WorkingUntilFirstHook(t *testing.T) {
	a := NewClaudeHooks(filepath.Join(t.TempDir(), "hooks.log"))
	require.Equal(t, protocol.StateWorking, a.Detect(nil, time.Second))
}

func TestClaudeHooks_FollowsLastHookEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hooks.log")
	a := NewClaudeHooks(path)

	appendLine(t, path, "needs_input") // SessionStart
	require.Equal(t, protocol.StateNeedsInput, a.Detect(nil, 0))

	appendLine(t, path, "working") // UserPromptSubmit
	require.Equal(t, protocol.StateWorking, a.Detect(nil, 0))

	appendLine(t, path, "needs_input") // Stop
	require.Equal(t, protocol.StateNeedsInput, a.Detect(nil, 0))
	require.Equal(t, protocol.StateNeedsInput, a.Detect(nil, 0), "unchanged file keeps state")
}

func TestClaudeHooks_IgnoresUnknownLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hooks.log")
	a := NewClaudeHooks(path)
	appendLine(t, path, "needs_input")
	require.Equal(t, protocol.StateNeedsInput, a.Detect(nil, 0))
	appendLine(t, path, "garbage")
	require.Equal(t, protocol.StateNeedsInput, a.Detect(nil, 0))
}

// An Esc-interrupted turn fires no Stop hook; a long silence ends "working".
func TestClaudeHooks_StaleWorkingBecomesNeedsInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hooks.log")
	a := NewClaudeHooks(path)
	appendLine(t, path, "working")
	require.Equal(t, protocol.StateWorking, a.Detect(nil, 2*time.Second))
	require.Equal(t, protocol.StateNeedsInput, a.Detect(nil, hookStaleWorking))
}

func TestClaudeHooks_NeverReadyForPaste(t *testing.T) {
	require.False(t, NewClaudeHooks("x").IsReadyForInput([]byte(`> Try "x"`), time.Hour))
}

// The generated --settings JSON must parse, and each hook command must append
// its state word to $REX_HOOK_FILE when run by a shell (as Claude does).
func TestClaudeHookArgs_CommandsWriteStateFile(t *testing.T) {
	args := ClaudeHookArgs()
	require.Equal(t, "--settings", args[0])

	var cfg struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	require.NoError(t, json.Unmarshal([]byte(args[1]), &cfg))
	for _, ev := range []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "Notification", "Stop"} {
		require.Contains(t, cfg.Hooks, ev)
	}
	require.Equal(t, "*", cfg.Hooks["PreToolUse"][0].Matcher)

	path := filepath.Join(t.TempDir(), "with space", "hooks.log")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	run := func(event string) {
		cmd := exec.Command("sh", "-c", cfg.Hooks[event][0].Hooks[0].Command)
		cmd.Env = append(os.Environ(), HookFileEnv+"="+path)
		require.NoError(t, cmd.Run())
	}
	a := NewClaudeHooks(path)
	run("UserPromptSubmit")
	require.Equal(t, protocol.StateWorking, a.Detect(nil, 0))
	run("Stop")
	require.Equal(t, protocol.StateNeedsInput, a.Detect(nil, 0))

	// Missing env / unwritable path must still exit 0 (never block Claude).
	cmd := exec.Command("sh", "-c", cfg.Hooks["Stop"][0].Hooks[0].Command)
	cmd.Env = append(os.Environ(), HookFileEnv+"=/nonexistent/dir/hooks.log")
	require.NoError(t, cmd.Run())
}

func TestClaudeHooks_CountsRunningSubagents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hooks.log")
	a := NewClaudeHooks(path)
	require.Zero(t, a.Subagents())

	appendLine(t, path, "agent+ a1")
	appendLine(t, path, "working")
	appendLine(t, path, "agent+ b2")
	appendLine(t, path, "agent+ a1") // resumed: same id again
	require.Equal(t, 2, a.Subagents())
	require.Equal(t, protocol.StateWorking, a.Detect(nil, 0), "subagent lines do not hide the state")

	appendLine(t, path, "agent- a1")
	appendLine(t, path, "agent- zz") // never started
	require.Equal(t, 1, a.Subagents())

	appendLine(t, path, "agents-reset")
	appendLine(t, path, "needs_input")
	require.Zero(t, a.Subagents())
	require.Equal(t, protocol.StateNeedsInput, a.Detect(nil, 0))
}

// A line still being written is held until its newline arrives.
func TestClaudeHooks_PartialLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hooks.log")
	a := NewClaudeHooks(path)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	defer f.Close()
	_, err = f.WriteString("agent+ ab")
	require.NoError(t, err)
	require.Zero(t, a.Subagents())
	_, err = f.WriteString("cd\n")
	require.NoError(t, err)
	require.Equal(t, 1, a.Subagents())
	appendLine(t, path, "agent- abcd")
	require.Zero(t, a.Subagents())
}

func TestClaudeHooks_TruncatedFileStartsOver(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hooks.log")
	a := NewClaudeHooks(path)
	appendLine(t, path, "agent+ a1")
	appendLine(t, path, "agent+ b2")
	require.Equal(t, 2, a.Subagents())
	require.NoError(t, os.WriteFile(path, []byte("needs_input\n"), 0o600))
	require.Zero(t, a.Subagents())
	require.Equal(t, protocol.StateNeedsInput, a.Detect(nil, 0))
}

// The SubagentStart/SubagentStop commands, run by sh with Claude-style JSON
// on stdin, write paired lines; internal helpers (no agent_type) are skipped
// and ids are cut to a safe character set.
func TestClaudeHookArgs_SubagentCommands(t *testing.T) {
	args := ClaudeHookArgs()
	var cfg struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	require.NoError(t, json.Unmarshal([]byte(args[1]), &cfg))
	require.Empty(t, cfg.Hooks["SubagentStart"][0].Matcher, "every agent_type")

	path := filepath.Join(t.TempDir(), "with space", "hooks.log")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	run := func(event, stdin string) {
		t.Helper()
		cmd := exec.Command("sh", "-c", cfg.Hooks[event][0].Hooks[0].Command)
		cmd.Env = append(os.Environ(), HookFileEnv+"="+path)
		cmd.Stdin = strings.NewReader(stdin)
		require.NoError(t, cmd.Run())
	}
	a := NewClaudeHooks(path)
	run("SubagentStart", `{"session_id":"s","hook_event_name":"SubagentStart","agent_id":"agent-1","agent_type":"Explore"}`)
	run("SubagentStart", `{"session_id":"s","agent_id":"agent-2","agent_type":"general-purpose"}`)
	run("SubagentStart", `{"session_id":"s","agent_id":"helper","agent_type":""}`) // internal
	require.Equal(t, 2, a.Subagents())

	// last_assistant_message quoting "agent_id" (escaped) must not win.
	run("SubagentStop", `{"agent_id":"agent-1","agent_type":"Explore","last_assistant_message":"set \"agent_id\": \"agent-2\" done"}`)
	require.Equal(t, 1, a.Subagents())

	run("SubagentStart", `{"agent_id":"x;rm -rf $HOME","agent_type":"Plan"}`)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(raw), "agent+ xrm-rfHOME\n", "id cut to [A-Za-z0-9_.:-]")
	require.NotContains(t, string(raw), "helper")

	// SessionStart resets the set before its state word.
	cmd := exec.Command("sh", "-c", cfg.Hooks["SessionStart"][0].Hooks[0].Command)
	cmd.Env = append(os.Environ(), HookFileEnv+"="+path)
	require.NoError(t, cmd.Run())
	require.Zero(t, a.Subagents())
	require.Equal(t, protocol.StateNeedsInput, a.Detect(nil, 0))

	// Missing env / unwritable path must still exit 0.
	bad := exec.Command("sh", "-c", cfg.Hooks["SubagentStop"][0].Hooks[0].Command)
	bad.Env = append(os.Environ(), HookFileEnv+"=/nonexistent/dir/hooks.log")
	bad.Stdin = strings.NewReader(`{"agent_id":"a","agent_type":"Explore"}`)
	require.NoError(t, bad.Run())
}

func TestClaudeHooks_BlankLinesAndUnreadableFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hooks.log")
	a := NewClaudeHooks(path)
	appendLine(t, path, "")
	appendLine(t, path, "agent+ a1")
	appendLine(t, path, "  ")
	require.Equal(t, 1, a.Subagents())

	// A directory where the file should be cannot be read: keep the last
	// known state and count, never panic.
	dir := t.TempDir()
	b := NewClaudeHooks(dir)
	require.Equal(t, protocol.StateWorking, b.Detect(nil, 0))
	require.Zero(t, b.Subagents())
}

// A subagent quiet for subagentQuiet stops counting; any sign of life
// (start or a tool call) keeps it, or brings it back.
func TestClaudeHooks_QuietSubagentsExpire(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hooks.log")
	a := NewClaudeHooks(path)
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	now := t0
	a.now = func() time.Time { return now }

	appendLine(t, path, "agent+ busy")
	appendLine(t, path, "agent+ lost") // its SubagentStop never comes
	require.Equal(t, 2, a.Subagents())

	now = t0.Add(10 * time.Minute)
	appendLine(t, path, "agent~ busy")
	require.Equal(t, 2, a.Subagents())

	now = t0.Add(subagentQuiet + time.Minute)
	require.Equal(t, 1, a.Subagents(), "lost is quiet past the window; busy was seen 6m ago")

	now = t0.Add(10*time.Minute + subagentQuiet + time.Minute)
	require.Zero(t, a.Subagents())

	appendLine(t, path, "agent~ busy") // still running after all
	require.Equal(t, 1, a.Subagents())
}

func runHook(t *testing.T, command, path, stdin string) {
	t.Helper()
	cmd := exec.Command("sh", "-c", command)
	cmd.Env = append(os.Environ(), HookFileEnv+"="+path)
	cmd.Stdin = strings.NewReader(stdin)
	require.NoError(t, cmd.Run())
}

func hookCommands(t *testing.T) map[string]string {
	t.Helper()
	var cfg struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	require.NoError(t, json.Unmarshal([]byte(ClaudeHookArgs()[1]), &cfg))
	out := map[string]string{}
	for ev, groups := range cfg.Hooks {
		out[ev] = groups[0].Hooks[0].Command
	}
	return out
}

// Only the top-level agent_id counts: not a nested object's, not one in an
// array after it, and pretty-printed JSON works too.
func TestClaudeHookArgs_TopLevelAgentID(t *testing.T) {
	cmds := hookCommands(t)
	path := filepath.Join(t.TempDir(), "hooks.log")
	runHook(t, cmds["SubagentStart"], path,
		`{"session_id":"s","agent_id":"top1","agent_type":"Explore","transcript":{"agent_id":"nested"},"background_tasks":[{"agent_id":"b1"},{"agent_id":"b2"}]}`)
	runHook(t, cmds["SubagentStart"], path, "{\n  \"session_id\": \"s\",\n  \"agent_id\": \"top2\",\n\t\"agent_type\": \"Plan\"\n}\n")
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "agent+ top1\nagent+ top2\n", string(raw))
}

// SessionStart resets the set for a new or cleared conversation, not after a
// compaction; a subagent's PostToolUse is a sign of life, the main
// session's is not.
func TestClaudeHookArgs_SessionStartAndPostToolUse(t *testing.T) {
	cmds := hookCommands(t)
	path := filepath.Join(t.TempDir(), "hooks.log")
	runHook(t, cmds["SessionStart"], path, `{"session_id":"s","hook_event_name":"SessionStart","source":"compact"}`)
	runHook(t, cmds["SessionStart"], path, `{"session_id":"s","hook_event_name":"SessionStart","source":"startup"}`)
	runHook(t, cmds["SessionStart"], path, `{"session_id":"s","source":"clear"}`)
	runHook(t, cmds["PostToolUse"], path, `{"session_id":"s","tool_name":"Bash","tool_input":{"command":"ls"}}`)
	runHook(t, cmds["PostToolUse"], path, `{"session_id":"s","agent_id":"sub-7","agent_type":"Explore","tool_name":"Read"}`)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "needs_input\nsid s\n"+
		"agents-reset\nneeds_input\nsid s\n"+
		"agents-reset\nneeds_input\nsid s\n"+
		"working\n"+
		"working\nagent~ sub-7\n", string(raw))
}

const testSID = "7d4f3c8a-1234-4abc-89ab-cdefabcdef01"

// Both SessionStart branches (new/cleared conversation and compaction)
// report the session id in the same single write as the state word; the id
// is cut to safe characters and a missing one writes an empty (ignored) line.
func TestClaudeHookArgs_SessionStartReportsSID(t *testing.T) {
	cmds := hookCommands(t)
	path := filepath.Join(t.TempDir(), "hooks.log")
	runHook(t, cmds["SessionStart"], path, `{"session_id":"`+testSID+`","source":"startup"}`)
	runHook(t, cmds["SessionStart"], path, `{"session_id":"`+testSID+`","source":"compact"}`)
	runHook(t, cmds["SessionStart"], path, `{"source":"clear"}`)
	runHook(t, cmds["SessionStart"], path, `{"session_id":"$(touch pwned);x y","source":"startup"}`)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t,
		"agents-reset\nneeds_input\nsid "+testSID+"\n"+
			"needs_input\nsid "+testSID+"\n"+
			"agents-reset\nneeds_input\nsid \n"+
			"agents-reset\nneeds_input\nsid touchpwnedxy\n", string(raw))
	require.NoFileExists(t, filepath.Join(filepath.Dir(path), "pwned"))
}

func TestClaudeHooks_ResumeIDFromSID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hooks.log")
	a := NewClaudeHooks(path)
	var _ ResumeIDReporter = a
	require.Empty(t, a.ResumeID(), "no file yet")

	appendLine(t, path, "needs_input")
	require.Empty(t, a.ResumeID())

	appendLine(t, path, "sid "+testSID)
	require.Equal(t, testSID, a.ResumeID())

	// Invalid ids are ignored and do not clobber a good one.
	for _, bad := range []string{"sid", "sid ", "sid s", "sid --resume", "sid " + testSID + "0"} {
		appendLine(t, path, bad)
	}
	require.Equal(t, testSID, a.ResumeID())

	// /clear starts a new conversation: the latest valid id wins. Upper case
	// is lowered on ingest.
	appendLine(t, path, "sid 11111111-2222-4333-8444-ABCDEFABCDEF")
	require.Equal(t, "11111111-2222-4333-8444-abcdefabcdef", a.ResumeID())
	appendLine(t, path, "sid "+testSID)
	require.Equal(t, testSID, a.ResumeID())
}

// A sid line is not a state change and must not clear anything else.
func TestClaudeHooks_SIDLeavesStateAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hooks.log")
	a := NewClaudeHooks(path)
	appendLine(t, path, "agent+ a1")
	appendLine(t, path, "needs_input")
	appendLine(t, path, "sid "+testSID)
	require.Equal(t, protocol.StateNeedsInput, a.Detect(nil, 0))
	require.Equal(t, 1, a.Subagents())
}
