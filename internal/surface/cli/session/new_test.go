package session

import (
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestUniqueSlug(t *testing.T) {
	sessions := []protocol.SessionSummary{{Slug: "fix-bug"}, {Slug: "fix-bug-2"}, {Slug: "session"}}
	require.Equal(t, "other", uniqueSlug("other", sessions))
	require.Equal(t, "fix-bug-3", uniqueSlug("fix-bug", sessions))
	require.Equal(t, "session-2", uniqueSlug("session", sessions))
	require.Equal(t, "x", uniqueSlug("x", nil))
}

// captureStdout returns what fn writes to os.Stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	prev := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	defer func() { os.Stdout = prev }()
	fn()
	_ = w.Close()
	return <-done
}

// The README form puts flags after the prompt; they must reach the daemon,
// not the prompt. SessionAdded is broadcast, so a session another client
// spawned at the same time must not be reported as ours.
func TestRunNew_FlagsAfterPromptAndOwnSession(t *testing.T) {
	sock, seen := recordingDaemon(t, func(w *protocol.Writer, env protocol.Envelope) {
		if env.Type != protocol.IntentNewSession {
			return
		}
		var req protocol.NewSession
		_ = json.Unmarshal(env.Data, &req)
		_ = w.WriteEvent(protocol.EventSessionAdded, "", protocol.SessionSummary{ShortID: "0ther", Slug: "someone-else"})
		_ = w.WriteEvent(protocol.EventSessionAdded, "", protocol.SessionSummary{ShortID: "ab12", Slug: req.Slug})
	})

	var err error
	out := captureStdout(t, func() {
		err = RunNew([]string{"hello rex", "--tool", "echo", "--model", "short", "--slug", "demo", "-socket", sock})
	})
	require.NoError(t, err)
	require.Equal(t, "ab12\tdemo\n", out)

	env := nextIntent(t, seen)
	var req protocol.NewSession
	require.NoError(t, json.Unmarshal(env.Data, &req))
	require.Equal(t, "demo", req.Slug)
	require.Equal(t, "echo", req.ToolID)
	require.Equal(t, "short", req.ModelID)
	require.Equal(t, "hello rex", req.Title)
	require.Equal(t, "hello rex", req.InitialPrompt)
}
