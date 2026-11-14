package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestSetLoop_BroadcastsAndPersists(t *testing.T) {
	root := t.TempDir()
	st := NewStore()
	sess := &Session{ID: "s1", ShortID: "s1", Slug: "s", State: protocol.StateWorking, StartedAt: time.Now()}
	require.NoError(t, st.Add(sess))
	var got []Event
	st.Subscribe(func(e Event) { got = append(got, e) })

	require.NoError(t, st.SetLoop("s1", "loop-0000000a", 2))
	require.Len(t, got, 1)
	require.Equal(t, map[string]any{"loop_id": "loop-0000000a", "loop_iter": 2}, got[0].Patch)
	require.NoError(t, st.SetLoop("s1", "loop-0000000a", 2), "no change, no event")
	require.Len(t, got, 1)
	require.Error(t, st.SetLoop("nope", "loop-0000000a", 1))

	require.NoError(t, WriteMeta(root, sess))
	back, err := LoadMeta(root, "s1")
	require.NoError(t, err)
	require.Equal(t, "loop-0000000a", back.LoopID)
	require.Equal(t, 2, back.LoopIter)
}

// meta.json is not trusted: a loop id that is not ours is dropped.
func TestLoadMeta_DropsBadLoopID(t *testing.T) {
	for _, tc := range []struct {
		id   string
		iter int
	}{{"../../x", 1}, {"loop-0000000a", 0}, {"loop-0000000a", 5000}} {
		s := fromSummary(protocol.SessionSummary{ID: "a", LoopID: tc.id, LoopIter: tc.iter})
		require.Empty(t, s.LoopID)
		require.Zero(t, s.LoopIter)
	}
	s := fromSummary(protocol.SessionSummary{ID: "a", LoopID: "loop-0000000a", LoopIter: 3})
	require.Equal(t, "loop-0000000a", s.LoopID)
}
