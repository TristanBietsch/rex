package adapter

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func appendLine(t *testing.T, path, line string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
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
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
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
