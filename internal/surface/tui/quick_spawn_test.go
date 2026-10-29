package tui

import (
	"encoding/json"
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// The quick-spawn Cmd used to re-read the settings store (a plain map) and
// m.Sessions (whose backing array Update mutates in place) from its
// goroutine, and to derive the slug a second time: a data race, and a slug
// that could differ from PendingSelectSlug so the new row never got
// selected. Everything is now resolved in Update; run with -race.
func TestQuickSpawn_CmdUsesValuesResolvedInUpdate(t *testing.T) {
	d := startFakeBoardDaemon(t)
	c, err := client.Dial(d.Socket)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	_, err = c.Hello("test")
	require.NoError(t, err)
	require.NoError(t, c.Subscribe(""))
	conn := d.nextConn(t)

	store := settings.NewStore()
	require.NoError(t, store.Set("default_spawn_tool", "claude"))
	require.NoError(t, store.Set("default_spawn_model", "opus"))
	m := boardModel(
		protocol.SessionSummary{ID: "a", Slug: "cc.opus.fix-auth-bug", State: protocol.StateDone},
		protocol.SessionSummary{ID: "b", Slug: "cc.opus.other", State: protocol.StateDone},
	)
	m.Client, m.Store = c, store
	m.Focus = FocusPrompt
	m.PromptText = "fix auth bug"

	m, cmd := updateKey(m, key("enter"))
	require.Equal(t, "cc.opus.fix-auth-bug-2", m.PendingSelectSlug)

	// Update keeps running before and while the Cmd goroutine does.
	require.NoError(t, store.Set("default_spawn_tool", "codex"))
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	for i := range 50 {
		_ = store.Set("default_spawn_model", fmt.Sprintf("m%d", i))
		m.Sessions[0].Slug = fmt.Sprintf("renamed-%d", i) // as applyPatch does
	}
	require.Nil(t, <-done)

	env, err := protocol.NewReader(conn).Read()
	require.NoError(t, err)
	require.Equal(t, protocol.IntentNewSession, env.Type)
	var req protocol.NewSession
	require.NoError(t, json.Unmarshal(env.Data, &req))
	require.Equal(t, m.PendingSelectSlug, req.Slug, "the board selects the slug it sent")
	require.Equal(t, "claude", req.ToolID)
	require.Equal(t, "opus", req.ModelID)
	require.Equal(t, "fix auth bug", req.InitialPrompt)
}

// Removing a session must not rewrite the backing array that an earlier
// copy of the Model (or a Cmd that captured m.Sessions) still holds.
func TestApplyEvent_SessionRemovedLeavesOldSliceIntact(t *testing.T) {
	m := boardModel(
		protocol.SessionSummary{ID: "a"},
		protocol.SessionSummary{ID: "b"},
		protocol.SessionSummary{ID: "c"},
	)
	old := m.Sessions
	data, _ := json.Marshal(protocol.SessionRemoved{SessionID: "a"})
	m = m.applyEvent(protocol.Envelope{Type: protocol.EventSessionRemoved, Data: data})

	require.Len(t, m.Sessions, 2)
	require.Equal(t, []string{"a", "b", "c"}, []string{old[0].ID, old[1].ID, old[2].ID})
}
