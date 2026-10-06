package tui

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestDeriveAgentSlug_QuickSpawnFormat(t *testing.T) {
	// Confirms the wizard's slug helper produces the format spawnSessionCmd
	// will use after Task 9. Guards against future drift.
	got := deriveAgentSlug("claude", "opus", "fix auth bug", nil)
	require.Equal(t, "cc.opus.fix-auth-bug", got)

	got = deriveAgentSlug("ollama", "llama3.1", "write readme", nil)
	require.Equal(t, "ol.llama3-1.write-readme", got)
}

// recordingClient captures NewSession intents for test assertions. It satisfies
// the sessionSpawner interface that spawnSessionCmd accepts in tests.
type recordingClient struct {
	last *protocol.NewSession
}

func (r *recordingClient) NewSession(req protocol.NewSession) error {
	r.last = &req
	return nil
}

func TestSpawnSessionCmd_UsesSettingsDefaults(t *testing.T) {
	store := settings.NewStore()
	require.NoError(t, store.Set("default_spawn_tool", "claude"))
	require.NoError(t, store.Set("default_spawn_model", "opus"))
	require.NoError(t, store.Set("default_spawn_effort", "max"))

	fake := &recordingClient{}
	cmd := spawnSessionCmd(fake, store, nil, "fix auth bug")
	require.NotNil(t, cmd)

	msg := cmd()
	require.Nil(t, msg, "successful spawn should produce no tea.Msg")
	require.NotNil(t, fake.last, "NewSession should have been invoked")

	require.Equal(t, "claude", fake.last.ToolID)
	require.Equal(t, "opus", fake.last.ModelID)
	require.Equal(t, "max", fake.last.Effort)
	require.Equal(t, "cc.opus.fix-auth-bug", fake.last.Slug)
	require.Equal(t, "fix auth bug", fake.last.InitialPrompt)
	require.Equal(t, "fix auth bug", fake.last.Title, "board shows Title until a summary exists")
}

func TestSpawnSessionCmd_DisambiguatesAgainstExistingSlugs(t *testing.T) {
	store := settings.NewStore()
	require.NoError(t, store.Set("default_spawn_tool", "claude"))
	require.NoError(t, store.Set("default_spawn_model", "opus"))

	existing := []protocol.SessionSummary{
		{Slug: "cc.opus.fix-auth-bug"},
	}

	fake := &recordingClient{}
	cmd := spawnSessionCmd(fake, store, existing, "fix auth bug")
	_ = cmd()

	require.NotNil(t, fake.last)
	require.Equal(t, "cc.opus.fix-auth-bug-2", fake.last.Slug)
}

// A spawn rejected by the daemon arrives as EventError; it must reach the
// status line instead of vanishing.
func TestApplyEvent_ErrorEventSurfaces(t *testing.T) {
	data, _ := json.Marshal(protocol.ErrorEvent{Code: "invalid", Message: "too many concurrent sessions (cap=1)"})
	m := Model{PendingSelectSlug: "cc.opus.x"}
	m = m.applyEvent(protocol.Envelope{Type: protocol.EventError, Data: data})
	require.Equal(t, "daemon: too many concurrent sessions (cap=1)", m.Err)
	require.Empty(t, m.PendingSelectSlug)
}

// Command failures must not start a second daemon listener.
func TestUpdate_CmdErrMsgDoesNotRearmListener(t *testing.T) {
	m := Model{}
	out, cmd := m.Update(CmdErrMsg{Op: "spawn", Err: errors.New("boom")})
	require.Nil(t, cmd)
	require.Equal(t, "spawn: boom", out.(Model).Err)
}

func TestApplyEvent_SessionAddedSelectsPendingSpawn(t *testing.T) {
	data, _ := json.Marshal(protocol.SessionSummary{ID: "new-id", Slug: "cc.opus.fix-auth-bug"})
	m := Model{SelectedID: "old", PendingSelectSlug: "cc.opus.fix-auth-bug"}
	m = m.applyEvent(protocol.Envelope{Type: protocol.EventSessionAdded, Data: data})
	require.Equal(t, "new-id", m.SelectedID)
	require.Empty(t, m.PendingSelectSlug)
}

func TestDropLastRune(t *testing.T) {
	require.Equal(t, "caf", dropLastRune("café"))
	require.Equal(t, "fix —", dropLastRune("fix —x"))
	require.Equal(t, "fix ", dropLastRune("fix —"))
	require.Equal(t, "", dropLastRune(""))
}
