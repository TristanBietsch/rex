package workspace

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// wsWithID makes a workspace with id under base.
func wsWithID(t *testing.T, base, id string) *Workspace {
	t.Helper()
	root := mkWS(t, filepath.Join(base, id), `{"id":"`+id+`","name":"w"}`)
	return loadWS(t, root)
}

// Each store stands in for a separate rex process: its own TrustStore (so
// its own mutex) on the same file. Without a file lock the read-modify-write
// of one loses the other's change.
func TestTrust_ConcurrentWritersLoseNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "workspace-trust.json")
	base := t.TempDir()
	const n = 12
	ws := make([]*Workspace, n)
	for i := range ws {
		ws[i] = wsWithID(t, base, fmt.Sprintf("%08x", i+1))
	}
	var wg sync.WaitGroup
	for i := range ws {
		wg.Go(func() {
			s, err := OpenTrust(path)
			require.NoError(t, err)
			require.NoError(t, s.Trust(ws[i], ""))
		})
	}
	wg.Wait()
	s, err := OpenTrust(path)
	require.NoError(t, err)
	for _, w := range ws {
		require.True(t, s.Trusted(w), "%s kept", w.ID)
	}
}

// rex ws trust -rm racing rex ws trust of another workspace: the revoke
// must never be lost.
func TestTrust_RevokeRacingTrustIsNotLost(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "workspace-trust.json")
	base := t.TempDir()
	for round := range 20 {
		victim := wsWithID(t, base, fmt.Sprintf("a%07x", round))
		others := make([]*Workspace, 4)
		for i := range others {
			others[i] = wsWithID(t, base, fmt.Sprintf("b%03x%04x", round, i))
		}
		s, err := OpenTrust(path)
		require.NoError(t, err)
		require.NoError(t, s.Trust(victim, ""))

		var wg sync.WaitGroup
		wg.Go(func() {
			r, err := OpenTrust(path)
			require.NoError(t, err)
			changed, err := r.Revoke(victim.ID, "")
			require.NoError(t, err)
			require.True(t, changed)
		})
		for _, o := range others {
			wg.Go(func() {
				w, err := OpenTrust(path)
				require.NoError(t, err)
				require.NoError(t, w.Trust(o, ""))
			})
		}
		wg.Wait()
		require.False(t, s.Trusted(victim), "round %d: the revoke was lost", round)
	}
}

// A writer waits for another process's lock, and gives up with an error
// (writing nothing) when it is never released.
func TestTrust_WaitsForTheLockThenGivesUp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "workspace-trust.json")
	w := wsWithID(t, t.TempDir(), "0000abcd")
	s, err := OpenTrust(path)
	require.NoError(t, err)

	old := trustLockWait
	trustLockWait = 300 * time.Millisecond
	t.Cleanup(func() { trustLockWait = old })

	unlock, err := lockTrustFile(path)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- s.Trust(w, "") }()
	time.Sleep(50 * time.Millisecond)
	unlock()
	require.NoError(t, <-done, "trusted once the lock was released")
	require.True(t, s.Trusted(w))

	unlock, err = lockTrustFile(path)
	require.NoError(t, err)
	defer unlock()
	_, err = s.Revoke(w.ID, "")
	require.ErrorContains(t, err, "locked")
	require.True(t, s.Trusted(w), "nothing written without the lock")
}
