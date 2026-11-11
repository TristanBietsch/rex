package adapter

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestClaudeHooks_WaitReason(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hooks.log")
	a := NewClaudeHooks(path)
	var _ WaitReasoner = a

	require.Empty(t, a.WaitReason(), "nothing reported yet")

	appendLine(t, path, "needs_input")
	appendLine(t, path, "turn-done") // Stop
	require.Equal(t, protocol.WaitReview, a.WaitReason())

	appendLine(t, path, "working") // UserPromptSubmit
	require.Empty(t, a.WaitReason(), "working clears it")

	appendLine(t, path, "needs_input")
	appendLine(t, path, "notify permission_prompt")
	require.Equal(t, protocol.WaitInput, a.WaitReason())

	appendLine(t, path, "turn-done")
	appendLine(t, path, "needs_input")
	appendLine(t, path, "notify idle_prompt") // 60 s after Stop
	require.Equal(t, protocol.WaitReview, a.WaitReason(), "idle_prompt keeps a review")

	appendLine(t, path, "working")
	appendLine(t, path, "needs_input")
	appendLine(t, path, "notify idle_prompt")
	require.Empty(t, a.WaitReason(), "idle_prompt keeps an unknown reason unknown")

	appendLine(t, path, "notify elicitation_dialog")
	require.Equal(t, protocol.WaitInput, a.WaitReason())

	appendLine(t, path, "notify") // no type in the payload
	require.Equal(t, protocol.WaitInput, a.WaitReason(), "empty type is an input")

	appendLine(t, path, "turn-done")
	appendLine(t, path, "sid 8f6a1b2c-1234-4abc-89ab-cdefabcdef01")
	require.Equal(t, protocol.WaitReview, a.WaitReason(), "a sid line does not clear it")

	appendLine(t, path, "agents-reset") // SessionStart of a new conversation
	require.Empty(t, a.WaitReason(), "SessionStart clears it")

	appendLine(t, path, "turn-done")
	appendLine(t, path, "agent+ a1")
	appendLine(t, path, "agent~ a1")
	appendLine(t, path, "agent- a1")
	require.Equal(t, protocol.WaitReview, a.WaitReason(), "subagent lines do not clear it")
}

// Stop and Notification commands, run by sh with Claude-style JSON on stdin,
// write the lines the adapter reads.
func TestClaudeHookArgs_StopAndNotificationLines(t *testing.T) {
	var cfg struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	require.NoError(t, json.Unmarshal([]byte(ClaudeHookArgs()[1]), &cfg))

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

	run("UserPromptSubmit", "")
	run("Stop", `{"session_id":"s","hook_event_name":"Stop"}`)
	require.Equal(t, protocol.StateNeedsInput, a.Detect(nil, 0))
	require.Equal(t, protocol.WaitReview, a.WaitReason())

	run("Notification", `{"hook_event_name":"Notification","notification_type":"idle_prompt","message":"waiting"}`)
	require.Equal(t, protocol.WaitReview, a.WaitReason(), "idle_prompt keeps the review")

	run("UserPromptSubmit", "")
	run("Notification", `{"notification_type":"permission_prompt","message":"Claude needs your permission"}`)
	require.Equal(t, protocol.StateNeedsInput, a.Detect(nil, 0))
	require.Equal(t, protocol.WaitInput, a.WaitReason())

	// A type quoted inside the message must not win, and odd characters are cut.
	run("UserPromptSubmit", "")
	run("Notification", `{"message":"say \"notification_type\": \"idle_prompt\"","notification_type":"x;rm -rf $HOME"}`)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(raw), "notify xrm-rfHOME\n")
	require.Equal(t, protocol.WaitInput, a.WaitReason())

	// No notification_type at all: a bare notify line, an input.
	run("UserPromptSubmit", "")
	run("Notification", `{"message":"hi"}`)
	raw, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(raw), "needs_input\nnotify \n")
	require.Equal(t, protocol.WaitInput, a.WaitReason())

	// Still exits 0 when the file cannot be written.
	bad := exec.Command("sh", "-c", cfg.Hooks["Notification"][0].Hooks[0].Command)
	bad.Env = append(os.Environ(), HookFileEnv+"=/nonexistent/dir/hooks.log")
	bad.Stdin = strings.NewReader(`{"notification_type":"idle_prompt"}`)
	require.NoError(t, bad.Run())
}
