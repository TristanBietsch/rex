package tui

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// The wizard asks for no name: effort leads straight to describe, and the
// request carries none (rename on the board sets one later).
func TestWizard_NoNameStep(t *testing.T) {
	m := wizardOn(&WizardState{Tools: wizTools(), ToolIdx: 2})
	m = wizKey(m, "enter", "enter")
	require.Equal(t, wizDescribe, m.Wizard.Step)
	m = typeText(m, "fix auth")
	require.Empty(t, m.Wizard.request("/w", nil).Name)
}

// An older daemon drops skills silently; the SessionAdded summary shows it.
func TestWizard_OldDaemonIgnoringSkillsIsReportedOnAdd(t *testing.T) {
	added := func(sum protocol.SessionSummary) protocol.Envelope {
		data, err := json.Marshal(sum)
		require.NoError(t, err)
		return protocol.Envelope{Kind: protocol.KindEvent, Type: protocol.EventSessionAdded, Data: data}
	}
	m := boardModel()
	m.PendingSelectSlug = "cc.opus.x"
	m.PendingSpawn = pendingSpawn{Skills: []string{"a"}}
	m = m.applyEvent(added(protocol.SessionSummary{ID: "1", Slug: "cc.opus.x"}))
	require.Equal(t, "this rex-daemon ignored skills; run `rex daemon restart`", m.Err)
	require.Equal(t, pendingSpawn{}, m.PendingSpawn)

	m = boardModel()
	m.PendingSelectSlug = "cc.opus.x"
	m.PendingSpawn = pendingSpawn{Skills: []string{"a"}}
	m = m.applyEvent(added(protocol.SessionSummary{ID: "1", Slug: "cc.opus.x", Skills: []string{"a"}}))
	require.Empty(t, m.Err, "a daemon that kept the skills is fine")

	// A rejected spawn drops the expectation with the pending selection.
	m = boardModel()
	m.PendingSelectSlug = "cc.opus.x"
	m.PendingSpawn = pendingSpawn{Skills: []string{"a"}}
	data, err := json.Marshal(protocol.ErrorEvent{Code: protocol.ErrCodeBadIntent, Message: "nope"})
	require.NoError(t, err)
	m = m.applyEvent(protocol.Envelope{Kind: protocol.KindEvent, Type: protocol.EventError, Data: data})
	require.Equal(t, pendingSpawn{}, m.PendingSpawn)

	require.Empty(t, pendingSpawn{}.ignoredBy(protocol.SessionSummary{}))
}
