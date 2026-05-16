package lua

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/protocol"
)

// fakeSender records calls to rex.send.
type fakeSender struct {
	calls []sendCall
}

type sendCall struct {
	sessionID string
	text      string
}

func (f *fakeSender) Send(sessionID, text string) error {
	f.calls = append(f.calls, sendCall{sessionID, text})
	return nil
}

func newTestRuntime(t *testing.T, sender func(string, string) error, lister func() []protocol.SessionSummary) *Runtime {
	t.Helper()
	r, err := New(Options{
		Sender: sender,
		Lister: lister,
	})
	require.NoError(t, err)
	t.Cleanup(r.Close)
	return r
}

func defaultLister() []protocol.SessionSummary { return nil }

// TestSessionAddedHandler verifies that a Lua handler registered with
// rex.on("session_added", fn) is invoked when OnEvent is called, and that the
// handler can call rex.send() with the session id.
func TestSessionAddedHandler(t *testing.T) {
	fs := &fakeSender{}

	r := newTestRuntime(t, fs.Send, defaultLister)

	script := `
rex.on("session_added", function(s)
  rex.send(s.id, "hello")
end)
`
	tmpFile := writeTempLua(t, script)
	require.NoError(t, r.LoadFile(tmpFile))

	summary := protocol.SessionSummary{
		ID:      "abc-123",
		ShortID: "abc",
		Slug:    "my-session",
		State:   protocol.StateWorking,
		ToolID:  "claude",
		ModelID: "claude-3-sonnet",
	}

	require.NoError(t, r.OnEvent("session_added", summary))

	require.Len(t, fs.calls, 1)
	assert.Equal(t, "abc-123", fs.calls[0].sessionID)
	assert.Equal(t, "hello", fs.calls[0].text)
}

// TestSessionAddedHandlerProtocolConstant verifies OnEvent also accepts the
// protocol constant form of the event name.
func TestSessionAddedHandlerProtocolConstant(t *testing.T) {
	fs := &fakeSender{}

	r := newTestRuntime(t, fs.Send, defaultLister)

	script := `rex.on("session_added", function(s) rex.send(s.id, "hi") end)`
	require.NoError(t, r.LoadFile(writeTempLua(t, script)))

	require.NoError(t, r.OnEvent(protocol.EventSessionAdded, protocol.SessionSummary{ID: "xyz"}))

	require.Len(t, fs.calls, 1)
	assert.Equal(t, "xyz", fs.calls[0].sessionID)
}

// TestList verifies that rex.list() exposes the injected sessions to Lua.
func TestList(t *testing.T) {
	sessions := []protocol.SessionSummary{
		{ID: "s1", ShortID: "s1", Slug: "first", State: protocol.StateWorking, ToolID: "claude", ModelID: "m1"},
		{ID: "s2", ShortID: "s2", Slug: "second", State: protocol.StateDone, ToolID: "gpt", ModelID: "m2"},
	}

	var capturedIDs []string
	fs := &fakeSender{}

	r := newTestRuntime(t, fs.Send, func() []protocol.SessionSummary { return sessions })

	// Iterate rex.list() and send a message to each session.
	script := `
rex.on("session_added", function(_)
  local list = rex.list()
  for i = 1, #list do
    rex.send(list[i].id, "ping")
  end
end)
`
	_ = capturedIDs
	require.NoError(t, r.LoadFile(writeTempLua(t, script)))
	require.NoError(t, r.OnEvent("session_added", protocol.SessionSummary{ID: "trigger"}))

	require.Len(t, fs.calls, 2)
	assert.Equal(t, "s1", fs.calls[0].sessionID)
	assert.Equal(t, "s2", fs.calls[1].sessionID)
}

// TestMissingFileTolerated verifies that LoadFile on a non-existent path
// returns nil without error.
func TestMissingFileTolerated(t *testing.T) {
	r := newTestRuntime(t, func(string, string) error { return nil }, defaultLister)
	err := r.LoadFile("/nonexistent/path/that/does/not/exist.lua")
	assert.NoError(t, err)
}

// TestSyntaxErrorReturned verifies that a Lua syntax error in init.lua
// causes LoadFile to return a non-nil error.
func TestSyntaxErrorReturned(t *testing.T) {
	r := newTestRuntime(t, func(string, string) error { return nil }, defaultLister)

	script := `this is not valid lua !!!@@@`
	tmpFile := writeTempLua(t, script)

	err := r.LoadFile(tmpFile)
	assert.Error(t, err)
}

// writeTempLua writes content to a temporary .lua file and returns its path.
func writeTempLua(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "init.lua")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}
