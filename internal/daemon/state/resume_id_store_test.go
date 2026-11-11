package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestStore_SetResumeID(t *testing.T) {
	st := NewStore()
	started := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, st.Add(&Session{ID: "a", ShortID: "a", Slug: "a", State: protocol.StateWorking, LastEventAt: started}))
	var patches []map[string]any
	st.Subscribe(func(e Event) {
		if e.Kind == EventUpdated {
			patches = append(patches, e.Patch)
		}
	})

	changed, err := st.SetResumeID("a", testResumeID)
	require.NoError(t, err)
	require.True(t, changed)
	changed, err = st.SetResumeID("a", "7D4F3C8A-1234-4ABC-89AB-CDEFABCDEF01")
	require.NoError(t, err)
	require.False(t, changed, "same id in another case is no change")
	changed, err = st.SetResumeID("a", "11111111-2222-4333-8444-555555555555")
	require.NoError(t, err)
	require.True(t, changed, "the latest id wins")

	_, err = st.SetResumeID("a", "--resume")
	require.ErrorContains(t, err, "invalid resume id")
	_, err = st.SetResumeID("a", "")
	require.Error(t, err)
	_, err = st.SetResumeID("missing", testResumeID)
	require.ErrorContains(t, err, "not found")

	sess, _ := st.Get("a")
	require.Equal(t, "11111111-2222-4333-8444-555555555555", sess.Summary().ResumeID)
	require.True(t, sess.Summary().LastEventAt.Equal(started), "an id change is not activity")
	require.Empty(t, patches, "the caller persists first, then broadcasts the patch")
}
