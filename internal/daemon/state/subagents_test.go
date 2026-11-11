package state

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestStore_SetSubagentsPatchesChangesOnly(t *testing.T) {
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

	require.NoError(t, st.SetSubagents("a", 2))
	require.NoError(t, st.SetSubagents("a", 2)) // unchanged: no patch
	require.NoError(t, st.SetSubagents("a", 0))
	require.Error(t, st.SetSubagents("missing", 1))

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []map[string]any{{"subagents": 2}, {"subagents": 0}}, patches)
	sess, _ := st.Get("a")
	require.Equal(t, 0, sess.Summary().Subagents)
	require.True(t, sess.Summary().LastEventAt.Equal(started), "a count change is not activity")
}

// A count persisted in meta.json does not come back: subagents do not
// outlive the daemon.
func TestLoadAll_DropsSubagents(t *testing.T) {
	root := t.TempDir()
	s := &Session{ID: "8f6a1b2c-1234-4abc-89ab-cdefabcdef01", ShortID: "8f6a", Slug: "x",
		State: protocol.StateDone, Subagents: 3}
	require.NoError(t, WriteMeta(root, s))
	loaded, err := LoadAll(root)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	require.Zero(t, loaded[0].Subagents)
}
