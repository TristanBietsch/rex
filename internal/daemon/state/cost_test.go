package state

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestStore_SetCost_PatchesAndPersists(t *testing.T) {
	st := NewStore()
	started := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, st.Add(&Session{ID: "a", ShortID: "a", Slug: "a", State: protocol.StateWorking, LastEventAt: started}))
	var mu sync.Mutex
	var patches []map[string]any
	st.Subscribe(func(e Event) {
		if e.Kind == EventUpdated {
			mu.Lock()
			patches = append(patches, e.Patch)
			mu.Unlock()
		}
	})

	require.NoError(t, st.SetCost("a", 1.25))
	require.NoError(t, st.SetCost("a", 1.25)) // unchanged: no patch
	require.Error(t, st.SetCost("missing", 1))

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []map[string]any{{"cost_usd": 1.25}}, patches)
	sess, _ := st.Get("a")
	require.True(t, sess.Summary().LastEventAt.Equal(started), "a cost change is not activity")
	back := fromSummary(sess.Summary())
	require.NotNil(t, back.CostUSD)
	require.InDelta(t, 1.25, *back.CostUSD, 1e-9)
}
