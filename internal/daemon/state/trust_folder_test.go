package state

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestTrustFolder_SurvivesMetaRoundTrip(t *testing.T) {
	dir := t.TempDir()
	sess := &Session{ID: "id1", State: protocol.StateDone, TrustFolder: true}
	require.True(t, sess.Summary().TrustFolder)
	require.True(t, fromSummary(sess.Summary()).TrustFolder)

	require.NoError(t, WriteMeta(dir, sess))
	got, err := LoadMeta(dir, "id1")
	require.NoError(t, err)
	require.True(t, got.TrustFolder)

	off := &Session{ID: "id2", State: protocol.StateDone}
	require.False(t, fromSummary(off.Summary()).TrustFolder)
}
