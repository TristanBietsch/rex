package state

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestStore_SetWaitReasonPatchesChangesOnly(t *testing.T) {
	st := NewStore()
	started := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, st.Add(&Session{ID: "a", ShortID: "a", Slug: "a", State: protocol.StateNeedsInput, LastEventAt: started}))
	var mu sync.Mutex
	var patches []map[string]any
	st.Subscribe(func(e Event) {
		if e.Kind == EventUpdated {
			mu.Lock()
			patches = append(patches, e.Patch)
			mu.Unlock()
		}
	})

	require.NoError(t, st.SetWaitReason("a", protocol.WaitReview))
	require.NoError(t, st.SetWaitReason("a", protocol.WaitReview)) // unchanged: no patch
	require.NoError(t, st.SetWaitReason("a", ""))
	require.Error(t, st.SetWaitReason("missing", protocol.WaitInput))
	require.Error(t, st.SetWaitReason("a", "bogus"), "only known reasons")

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []map[string]any{{"wait_reason": "review"}, {"wait_reason": ""}}, patches)
	sess, _ := st.Get("a")
	require.Empty(t, sess.Summary().WaitReason)
	require.True(t, sess.Summary().LastEventAt.Equal(started), "a reason change is not activity")
}

// A reason persisted in meta.json does not come back: sessions that were
// live load as crashed.
func TestLoadAll_DropsWaitReason(t *testing.T) {
	root := t.TempDir()
	s := &Session{ID: "8f6a1b2c-1234-4abc-89ab-cdefabcdef01", ShortID: "8f6a", Slug: "x",
		State: protocol.StateNeedsInput, WaitReason: protocol.WaitReview}
	require.NoError(t, WriteMeta(root, s))
	loaded, err := LoadAll(root)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	require.Empty(t, loaded[0].WaitReason)
}

func TestSession_SummaryCarriesWaitReason(t *testing.T) {
	s := &Session{ID: "a", WaitReason: protocol.WaitInput}
	require.Equal(t, protocol.WaitInput, s.Summary().WaitReason)
}
