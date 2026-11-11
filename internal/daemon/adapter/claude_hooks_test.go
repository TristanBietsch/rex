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
