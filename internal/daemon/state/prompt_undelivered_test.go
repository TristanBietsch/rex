package state

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestStore_SetPromptUndeliveredPatchesOnce(t *testing.T) {
	st := NewStore()
	require.NoError(t, st.Add(&Session{ID: "a", ShortID: "a", Slug: "a", State: protocol.StateWorking}))
	var mu sync.Mutex
	var patches []map[string]any
	st.Subscribe(func(e Event) {
		if e.Kind == EventUpdated {
			mu.Lock()
			patches = append(patches, e.Patch)
			mu.Unlock()
		}
	})
	require.NoError(t, st.SetPromptUndelivered("a"))
	require.NoError(t, st.SetPromptUndelivered("a")) // already set: no patch
	require.Error(t, st.SetPromptUndelivered("missing"))

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []map[string]any{{"prompt_undelivered": true}}, patches)
	sess, _ := st.Get("a")
	require.True(t, sess.Summary().PromptUndelivered)
}
