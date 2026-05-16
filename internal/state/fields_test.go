package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/protocol"
)

// TestSummaryRoundTripNewFields verifies that Tokens, OutputBytes, and Fleet
// survive a WriteMeta / LoadMeta round-trip.
func TestSummaryRoundTripNewFields(t *testing.T) {
	dir := t.TempDir()
	sess := &Session{
		ID:          "abc123",
		ShortID:     "abc1",
		ToolID:      "claude",
		ModelID:     "opus",
		Slug:        "test-fleet",
		State:       protocol.StateDone,
		StartedAt:   time.Now().UTC(),
		LastEventAt: time.Now().UTC(),
		Tokens:      12345,
		OutputBytes: 49380,
		Fleet:       "my-fleet",
	}
	require.NoError(t, WriteMeta(dir, sess))

	got, err := LoadMeta(dir, sess.ID)
	require.NoError(t, err)
	require.Equal(t, int64(12345), got.Tokens)
	require.Equal(t, int64(49380), got.OutputBytes)
	require.Equal(t, "my-fleet", got.Fleet)
}

// TestSummaryRoundTripZeroFields verifies that zero/empty new fields load
// cleanly from old meta.json files that don't have those keys.
func TestSummaryRoundTripZeroFields(t *testing.T) {
	dir := t.TempDir()
	// Write a session with zero values (simulating an old meta.json).
	sess := &Session{
		ID:        "old001",
		ShortID:   "old0",
		ToolID:    "echo",
		ModelID:   "short",
		Slug:      "old-session",
		State:     protocol.StateDone,
		StartedAt: time.Now().UTC(),
	}
	require.NoError(t, WriteMeta(dir, sess))

	got, err := LoadMeta(dir, sess.ID)
	require.NoError(t, err)
	require.Equal(t, int64(0), got.Tokens)
	require.Equal(t, int64(0), got.OutputBytes)
	require.Equal(t, "", got.Fleet)
}

// TestSummaryMethod verifies Session.Summary() copies new fields correctly.
func TestSummaryMethod(t *testing.T) {
	sess := &Session{
		ID:          "s1",
		ShortID:     "s001",
		Tokens:      500,
		OutputBytes: 2000,
		Fleet:       "alpha",
	}
	sum := sess.Summary()
	require.Equal(t, int64(500), sum.Tokens)
	require.Equal(t, int64(2000), sum.OutputBytes)
	require.Equal(t, "alpha", sum.Fleet)
}
