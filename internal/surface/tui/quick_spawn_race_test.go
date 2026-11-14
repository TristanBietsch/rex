package tui

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// The quick-spawn Cmd runs on its own goroutine. It must send the slug Update
// chose (PendingSelectSlug, so the board selects the new session), whatever
// happens to the session list before it runs: a SessionRemoved used to
// rewrite the backing array the Cmd still read.
func TestQuickSpawn_SlugFixedInUpdate(t *testing.T) {
	store := settings.NewStore()
	require.NoError(t, store.Set("default_spawn_tool", "claude"))
	require.NoError(t, store.Set("default_spawn_model", "opus"))
	fake := &recordingClient{}
	m := boardModel(
		protocol.SessionSummary{ID: "a", Slug: "cc.opus.fix-auth-bug", State: protocol.StateWorking},
		protocol.SessionSummary{ID: "b", Slug: "other", State: protocol.StateWorking},
	)
	m.Store = store
	m.spawner = fake
	m.Focus = FocusPrompt
	m.PromptText = "fix auth bug"
	before := append([]protocol.SessionSummary(nil), m.Sessions...)

	m, cmd := updateKey(m, key("enter"))
	require.NotNil(t, cmd)
	require.Equal(t, "cc.opus.fix-auth-bug-2", m.PendingSelectSlug)

	// The session holding the slug goes away before the Cmd runs.
	m = m.applyEvent(staleEnv(t, protocol.EventSessionRemoved, protocol.SessionRemoved{SessionID: "a"}))
	_ = cmd()
	require.NotNil(t, fake.last)
	require.Equal(t, m.PendingSelectSlug, fake.last.Slug)
	require.Equal(t, "cc.opus.fix-auth-bug", before[0].Slug, "an earlier copy of the list is untouched")
}

// SessionRemoved builds a new slice: Model copies made before it keep their
// sessions.
func TestSessionRemoved_DoesNotRewriteSharedSlice(t *testing.T) {
	m := boardModel(
		protocol.SessionSummary{ID: "a", Slug: "a"},
		protocol.SessionSummary{ID: "b", Slug: "b"},
	)
	old := m.Sessions
	m = m.applyEvent(staleEnv(t, protocol.EventSessionRemoved, protocol.SessionRemoved{SessionID: "a"}))
	require.Len(t, m.Sessions, 1)
	require.Equal(t, "a", old[0].ID)
	require.Equal(t, "b", old[1].ID)
}
