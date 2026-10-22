package tui

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestResolveLocal(t *testing.T) {
	m := boardModel(
		protocol.SessionSummary{ID: "id-a", ShortID: "aaaa", Slug: "dup"},
		protocol.SessionSummary{ID: "id-b", ShortID: "bbbb", Slug: "dup"},
		protocol.SessionSummary{ID: "id-c", ShortID: "cccc", Slug: "solo"},
	)

	id, msg := resolveLocal(m, "dup")
	require.Empty(t, id, "duplicate slug must not resolve to the first match")
	require.Equal(t, "ambiguous: dup matches aaaa, bbbb; use short id", msg)

	for sel, want := range map[string]string{"bbbb": "id-b", "id-a": "id-a", "solo": "id-c"} {
		id, msg = resolveLocal(m, sel)
		require.Equal(t, want, id, sel)
		require.Empty(t, msg)
	}

	id, msg = resolveLocal(m, "nope")
	require.Empty(t, id)
	require.Equal(t, "no match for nope", msg)
}

func TestCommand_AmbiguousSlugSetsErr(t *testing.T) {
	fr := &fakeRetirer{}
	m := boardModel(
		protocol.SessionSummary{ID: "id-a", ShortID: "aaaa", Slug: "dup", State: protocol.StateDone},
		protocol.SessionSummary{ID: "id-b", ShortID: "bbbb", Slug: "dup", State: protocol.StateDone},
	)
	m.retirer = fr

	m2, cmd := executeCommand(m, "done dup")
	require.Nil(t, cmd)
	require.Contains(t, m2.Err, "ambiguous")
	require.Empty(t, fr.retired)

	m3, _ := executeCommand(m, "rm dup")
	require.Contains(t, m3.Err, "use short id")

	m4, _ := executeCommand(m, "rename dup x")
	require.Contains(t, m4.Err, "ambiguous")
}

func TestCommand_RenameRejectsBadSlugLocally(t *testing.T) {
	m := boardModel(protocol.SessionSummary{ID: "id-a", ShortID: "aaaa", Slug: "a"})
	m2, cmd := executeCommand(m, "rename aaaa bad\x1bslug")
	require.Nil(t, cmd)
	require.Contains(t, m2.Err, "rename: slug must not contain control characters")
}

// A spawn the daemon rejects (slug taken by a concurrent spawn) lands on the
// status line and drops the pending selection.
func TestApplyEvent_SpawnSlugTakenSurfaces(t *testing.T) {
	m := boardModel(protocol.SessionSummary{ID: "id-a", ShortID: "aaaa", Slug: "cc.opus.fix"})
	m.PendingSelectSlug = "cc.opus.fix-2"
	data, err := json.Marshal(protocol.ErrorEvent{Code: protocol.ErrCodeBadIntent, Message: `slug "cc.opus.fix-2" already in use by bbbb`})
	require.NoError(t, err)

	m = m.applyEvent(protocol.Envelope{Kind: protocol.KindEvent, Type: protocol.EventError, Data: data})
	require.Equal(t, `daemon: slug "cc.opus.fix-2" already in use by bbbb`, m.Err)
	require.Empty(t, m.PendingSelectSlug)
}
