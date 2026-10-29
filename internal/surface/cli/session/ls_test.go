package session

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// -show-archived was parsed but ignored; archived sessions now stay out of
// rex ls unless it is given.
func TestRunLs_ShowArchived(t *testing.T) {
	sessions := []protocol.SessionSummary{
		{ID: "7d4f3c8a-1234-4abc-89ab-cdefabcdef01", ShortID: "7d4f", Slug: "live", Title: "fix bug"},
		{ID: "b203a999-1234-4abc-89ab-cdefabcdef02", ShortID: "b203", Slug: "old", Title: archivedPrefix + "ship it"},
	}

	sock, _ := snapshotDaemon(t, sessions, nil)
	out := captureStdout(t, func() { require.NoError(t, RunLs([]string{"-socket", sock})) })
	require.Contains(t, out, "live")
	require.NotContains(t, out, "old")

	sock, _ = snapshotDaemon(t, sessions, nil)
	out = captureStdout(t, func() { require.NoError(t, RunLs([]string{"-show-archived", "-socket", sock})) })
	require.Contains(t, out, "live")
	require.Contains(t, out, "old")
}

func TestRunArchive_AlreadyArchived(t *testing.T) {
	sessions := []protocol.SessionSummary{
		{ID: "b203a999-1234-4abc-89ab-cdefabcdef02", ShortID: "b203", Slug: "old", Title: archivedPrefix + "ship it"},
	}
	sock, seen := snapshotDaemon(t, sessions, nil)
	out := captureStdout(t, func() { require.NoError(t, RunArchive([]string{"old", "-socket", sock})) })
	require.Contains(t, out, "already archived")
	require.Empty(t, seen, "no second prefix is sent")
}
