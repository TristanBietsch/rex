package state

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func slugSess(id, short, slug string) *Session {
	return &Session{ID: id, ShortID: short, ToolID: "echo", ModelID: "short", Slug: slug, CWD: "/tmp", State: protocol.StateWorking}
}

func TestStore_AddUniqueRejectsTakenSlug(t *testing.T) {
	store := NewStore()
	require.NoError(t, store.AddUnique(slugSess("id-a", "aaaa", "alpha")))

	err := store.AddUnique(slugSess("id-b", "bbbb", "alpha"))
	var taken *SlugTakenError
	require.ErrorAs(t, err, &taken)
	require.Equal(t, "alpha", taken.Slug)
	require.Equal(t, "id-a", taken.ID)
	require.Equal(t, `slug "alpha" already in use by aaaa`, err.Error())
	_, ok := store.Get("id-b")
	require.False(t, ok, "rejected session must not be inserted")

	// Done sessions still hold their slug: they remain selectable.
	require.NoError(t, store.Transition("id-a", protocol.StateDone))
	require.Error(t, store.AddUnique(slugSess("id-c", "cccc", "alpha")))
	require.NoError(t, store.AddUnique(slugSess("id-d", "dddd", "beta")))
}

func TestStore_AddUniqueFreesSlugAfterRemove(t *testing.T) {
	store := NewStore()
	require.NoError(t, store.AddUnique(slugSess("id-a", "aaaa", "alpha")))
	require.NoError(t, store.Remove("id-a"))
	require.NoError(t, store.AddUnique(slugSess("id-b", "bbbb", "alpha")))
}

func TestStore_UpdateRenameRejectsSlugOfOtherSession(t *testing.T) {
	store := NewStore()
	require.NoError(t, store.Add(slugSess("id-a", "aaaa", "alpha")))
	require.NoError(t, store.Add(slugSess("id-b", "bbbb", "beta")))

	var patches int
	cancel := store.Subscribe(func(e Event) {
		if e.Kind == EventUpdated {
			patches++
		}
	})
	defer cancel()

	err := store.UpdateRename("id-b", "alpha", "")
	var taken *SlugTakenError
	require.ErrorAs(t, err, &taken)
	require.Equal(t, "aaaa", taken.ShortID)
	b, _ := store.Get("id-b")
	require.Equal(t, "beta", b.Slug, "slug unchanged on rejection")
	require.Zero(t, patches, "no broadcast on rejection")

	// Renaming to its own slug is a confirmed no-op, not a collision.
	require.NoError(t, store.UpdateRename("id-a", "alpha", ""))
	require.Equal(t, 1, patches)
	// Title-only renames skip the slug check entirely.
	require.NoError(t, store.UpdateRename("id-b", "", "new title"))
}

func TestStore_UpdateRenameUnknownSession(t *testing.T) {
	store := NewStore()
	err := store.UpdateRename("nope", "x", "")
	require.Error(t, err)
	require.False(t, errors.As(err, new(*SlugTakenError)))
}

// Concurrent renames of different sessions to the same slug: exactly one
// wins. Run with -race.
func TestStore_ConcurrentRenamesToSameSlug(t *testing.T) {
	for round := 0; round < 50; round++ {
		store := NewStore()
		const n = 8
		for i := 0; i < n; i++ {
			require.NoError(t, store.Add(slugSess(fmt.Sprintf("id-%d", i), fmt.Sprintf("%04x", i), fmt.Sprintf("s-%d", i))))
		}
		var wg sync.WaitGroup
		errs := make([]error, n)
		start := make(chan struct{})
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				errs[i] = store.UpdateRename(fmt.Sprintf("id-%d", i), "contested", "")
			}(i)
		}
		close(start)
		wg.Wait()
		wins := 0
		for _, err := range errs {
			if err == nil {
				wins++
				continue
			}
			require.ErrorAs(t, err, new(*SlugTakenError))
		}
		require.Equal(t, 1, wins, "round %d", round)
		holders := 0
		for _, s := range store.Snapshot() {
			if s.Slug == "contested" {
				holders++
			}
		}
		require.Equal(t, 1, holders)
	}
}

// Concurrent spawns with the same slug: exactly one is added.
func TestStore_ConcurrentAddUniqueSameSlug(t *testing.T) {
	store := NewStore()
	const n = 16
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if store.AddUnique(slugSess(fmt.Sprintf("id-%d", i), fmt.Sprintf("%04x", i), "same")) == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	require.Equal(t, 1, wins)
	require.Len(t, store.All(), 1)
}

// Duplicates already on disk load fine through Add and are reported.
func TestStore_DuplicateSlugs(t *testing.T) {
	store := NewStore()
	require.NoError(t, store.Add(slugSess("id-a", "aaaa", "dup")))
	require.NoError(t, store.Add(slugSess("id-b", "bbbb", "dup")))
	require.NoError(t, store.Add(slugSess("id-c", "cccc", "solo")))

	d := store.DuplicateSlugs()
	require.Len(t, d, 1)
	require.ElementsMatch(t, []string{"aaaa", "bbbb"}, d["dup"])

	// Renaming one of the duplicates away resolves it; renaming onto the
	// remaining holder is rejected.
	require.NoError(t, store.UpdateRename("id-b", "dup-2", ""))
	require.Empty(t, store.DuplicateSlugs())
	require.Error(t, store.UpdateRename("id-b", "dup", ""))
}
