package state

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestWorkspaceID_SummaryRoundTripOmitempty(t *testing.T) {
	sum := protocol.SessionSummary{ID: "a"}
	b, err := json.Marshal(sum)
	require.NoError(t, err)
	require.NotContains(t, string(b), "workspace_id")

	sum.WorkspaceID = "7f3b89a2"
	b, err = json.Marshal(sum)
	require.NoError(t, err)
	require.Contains(t, string(b), `"workspace_id":"7f3b89a2"`)
	var back protocol.SessionSummary
	require.NoError(t, json.Unmarshal(b, &back))
	require.Equal(t, "7f3b89a2", back.WorkspaceID)
}

// A crashed session keeps its workspace across a restart.
func TestWorkspaceID_SurvivesRestart(t *testing.T) {
	root := t.TempDir()
	sess := &Session{ID: "s1", ShortID: "s1", Slug: "s", State: protocol.StateCrashed,
		StartedAt: time.Now(), WorkspaceID: "7f3b89a2"}
	require.NoError(t, WriteMeta(root, sess))
	back, err := LoadMeta(root, "s1")
	require.NoError(t, err)
	require.Equal(t, "7f3b89a2", back.WorkspaceID)
	require.Equal(t, "7f3b89a2", back.Summary().WorkspaceID)
}

// meta.json is not trusted: an id of the wrong shape is dropped.
func TestFromSummary_DropsBadWorkspaceID(t *testing.T) {
	for _, id := range []string{"../../x", "7F3B89A2", "7f3b89", "zzzzzzzz", "7f3b89a2a"} {
		require.Empty(t, fromSummary(protocol.SessionSummary{ID: "a", WorkspaceID: id}).WorkspaceID, id)
	}
}
