package state

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

const testResumeID = "7d4f3c8a-1234-4abc-89ab-cdefabcdef01"

func TestSummary_CarriesResumeID(t *testing.T) {
	s := &Session{ID: "a", ResumeID: testResumeID}
	require.Equal(t, testResumeID, s.Summary().ResumeID)
}

// meta.json is not trusted for argv: an invalid id is dropped on load, a
// valid one survives a crashed session, and upper case is lowered.
func TestLoadAll_ResumeID(t *testing.T) {
	root := t.TempDir()
	good := &Session{ID: "8f6a1b2c-1234-4abc-89ab-cdefabcdef01", ShortID: "8f6a", Slug: "good",
		State: protocol.StateWorking, ResumeID: testResumeID}
	upper := &Session{ID: "8f6a1b2c-1234-4abc-89ab-cdefabcdef02", ShortID: "8f6b", Slug: "upper",
		State: protocol.StateDone, ResumeID: "7D4F3C8A-1234-4ABC-89AB-CDEFABCDEF01"}
	bad := &Session{ID: "8f6a1b2c-1234-4abc-89ab-cdefabcdef03", ShortID: "8f6c", Slug: "bad",
		State: protocol.StateDone, ResumeID: "--dangerously-skip-permissions"}
	for _, s := range []*Session{good, upper, bad} {
		require.NoError(t, WriteMeta(root, s))
	}
	loaded, err := LoadAll(root)
	require.NoError(t, err)
	got := map[string]*Session{}
	for _, s := range loaded {
		got[s.Slug] = s
	}
	require.Equal(t, protocol.StateCrashed, got["good"].State)
	require.Equal(t, testResumeID, got["good"].ResumeID, "kept on a crashed session")
	require.Equal(t, testResumeID, got["upper"].ResumeID, "lowercased on ingest")
	require.Empty(t, got["bad"].ResumeID, "invalid id dropped")
}
