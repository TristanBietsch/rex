package state

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func namedSess(id, short, slug, name string) *Session {
	s := slugSess(id, short, slug)
	s.Name = name
	return s
}

func TestStore_AddUniqueRejectsTakenName(t *testing.T) {
	store := NewStore()
	require.NoError(t, store.AddUnique(namedSess("id-a", "aaaa", "a", "Fix Auth")))

	for _, name := range []string{"Fix Auth", "fix auth", " FIX  AUTH ", "fix\u200b auth"} {
		err := store.AddUnique(namedSess("id-b", "bbbb", "b", name))
		var taken *NameTakenError
		require.ErrorAs(t, err, &taken, name)
		require.Equal(t, "Fix Auth", taken.Name, "error names the holder's stored name")
		require.Equal(t, "id-a", taken.ID)
		require.Equal(t, "aaaa", taken.ShortID)
		require.Equal(t, `name "Fix Auth" already in use by aaaa`, err.Error())
		_, ok := store.Get("id-b")
		require.False(t, ok, "rejected session must not be inserted")
	}
	// A different name, and no name at all, are fine; many sessions may be unnamed.
	require.NoError(t, store.AddUnique(namedSess("id-c", "cccc", "c", "fix auth 2")))
	require.NoError(t, store.AddUnique(namedSess("id-d", "dddd", "d", "")))
	require.NoError(t, store.AddUnique(namedSess("id-e", "eeee", "e", "")))
}

func TestStore_AddUniqueSlugCheckedBeforeName(t *testing.T) {
	store := NewStore()
	require.NoError(t, store.AddUnique(namedSess("id-a", "aaaa", "same", "Same")))
	err := store.AddUnique(namedSess("id-b", "bbbb", "same", "same"))
	require.ErrorAs(t, err, new(*SlugTakenError))
}

func TestStore_NameHeldInEveryStateUntilRemoved(t *testing.T) {
	for _, st := range []protocol.State{protocol.StateQueued, protocol.StateWorking, protocol.StateNeedsInput,
		protocol.StateDone, protocol.StateFailed, protocol.StateCrashed} {
		store := NewStore()
		a := namedSess("id-a", "aaaa", "a", "held")
		a.State = st
		require.NoError(t, store.Add(a))
		require.ErrorAs(t, store.AddUnique(namedSess("id-b", "bbbb", "b", "held")), new(*NameTakenError), st)
	}

	// Marked done (retired, not cleared yet) still holds it.
	store := NewStore()
	require.NoError(t, store.Add(namedSess("id-a", "aaaa", "a", "held")))
	_, err := store.SetRetire("id-a", time.Now().Add(time.Hour), time.Now())
	require.NoError(t, err)
	require.ErrorAs(t, store.AddUnique(namedSess("id-b", "bbbb", "b", "held")), new(*NameTakenError))
	_, err = store.SetName("id-a", "held")
	require.NoError(t, err)

	// Removing the session frees the name.
	require.NoError(t, store.Remove("id-a"))
	require.NoError(t, store.AddUnique(namedSess("id-b", "bbbb", "b", "HELD")))
}

func TestStore_SetNameRejectsTakenName(t *testing.T) {
	store := NewStore()
	require.NoError(t, store.Add(namedSess("id-a", "aaaa", "a", "alpha")))
	require.NoError(t, store.Add(namedSess("id-b", "bbbb", "b", "beta")))

	changed, err := store.SetName("id-b", "ALPHA")
	var taken *NameTakenError
	require.ErrorAs(t, err, &taken)
	require.False(t, changed)
	require.Equal(t, "aaaa", taken.ShortID)
	b, _ := store.Get("id-b")
	require.Equal(t, "beta", b.Name, "name unchanged on rejection")

	// Own name, and a case or whitespace change of it: allowed.
	changed, err = store.SetName("id-b", "beta")
	require.NoError(t, err)
	require.False(t, changed)
	changed, err = store.SetName("id-b", "Beta")
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "Beta", b.Name)

	// Clearing always works, and a cleared name frees it.
	_, err = store.SetName("id-a", "")
	require.NoError(t, err)
	_, err = store.SetName("id-b", "alpha")
	require.NoError(t, err)

	_, err = store.SetName("nope", "x")
	require.Error(t, err)
	require.False(t, errors.As(err, new(*NameTakenError)))
}

func TestStore_CheckNew(t *testing.T) {
	store := NewStore()
	require.NoError(t, store.Add(namedSess("id-a", "aaaa", "a", "Alpha")))
	require.ErrorAs(t, store.CheckNew("a", ""), new(*SlugTakenError))
	require.ErrorAs(t, store.CheckNew("b", "alpha"), new(*NameTakenError))
	require.NoError(t, store.CheckNew("b", "beta"))
	require.NoError(t, store.CheckNew("b", ""))
}

// Duplicates from an older daemon load through Add, are reported, and are
// never renamed. A holder may keep (or recase) its own name, not take
// another taken one.
func TestStore_DuplicateNames(t *testing.T) {
	store := NewStore()
	require.NoError(t, store.Add(namedSess("id-a", "aaaa", "a", "dup")))
	require.NoError(t, store.Add(namedSess("id-b", "bbbb", "b", "DUP")))
	require.NoError(t, store.Add(namedSess("id-c", "cccc", "c", "solo")))
	require.NoError(t, store.Add(namedSess("id-d", "dddd", "d", "")))
	require.NoError(t, store.Add(namedSess("id-e", "eeee", "e", "")))

	d := store.DuplicateNames()
	require.Len(t, d, 1)
	for _, shorts := range d {
		require.ElementsMatch(t, []string{"aaaa", "bbbb"}, shorts)
	}
	_, err := store.SetName("id-b", "Dup")
	require.NoError(t, err, "own name, even while another session shares it")
	_, err = store.SetName("id-b", "solo")
	require.ErrorAs(t, err, new(*NameTakenError))

	_, err = store.SetName("id-b", "dup-2")
	require.NoError(t, err)
	require.Empty(t, store.DuplicateNames())
	_, err = store.SetName("id-b", "dup")
	require.ErrorAs(t, err, new(*NameTakenError))
}

// Concurrent spawns with one name: exactly one is added. Run with -race.
func TestStore_ConcurrentAddUniqueSameName(t *testing.T) {
	for round := 0; round < 20; round++ {
		store := NewStore()
		const n = 16
		var wg sync.WaitGroup
		errs := make([]error, n)
		start := make(chan struct{})
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				name := "Contested"
				if i%2 == 1 {
					name = "contested"
				}
				errs[i] = store.AddUnique(namedSess(fmt.Sprintf("id-%d", i), fmt.Sprintf("%04x", i), fmt.Sprintf("s-%d", i), name))
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
			require.ErrorAs(t, err, new(*NameTakenError))
		}
		require.Equal(t, 1, wins, "round %d", round)
		require.Len(t, store.All(), 1)
	}
}

// Concurrent SetName of different sessions to one name: exactly one wins.
func TestStore_ConcurrentSetNameSameName(t *testing.T) {
	for round := 0; round < 20; round++ {
		store := NewStore()
		const n = 8
		for i := 0; i < n; i++ {
			require.NoError(t, store.Add(namedSess(fmt.Sprintf("id-%d", i), fmt.Sprintf("%04x", i), fmt.Sprintf("s-%d", i), "")))
		}
		var wg sync.WaitGroup
		errs := make([]error, n)
		start := make(chan struct{})
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				_, errs[i] = store.SetName(fmt.Sprintf("id-%d", i), "contested")
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
			require.ErrorAs(t, err, new(*NameTakenError))
		}
		require.Equal(t, 1, wins, "round %d", round)
		holders := 0
		for _, s := range store.Snapshot() {
			if s.Name == "contested" {
				holders++
			}
		}
		require.Equal(t, 1, holders)
	}
}
