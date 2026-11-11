package server

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// StopSession (a Delete) and promoteStop (the resume starting its supervisor)
// race freely. Either the delete lands first and the promote is refused, or
// the promote lands first and the delete calls the real stop; a delete that
// returns can never be lost.
func TestReserveStop_StopSessionRacesPromote(t *testing.T) {
	for i := range 3000 {
		s := &Server{}
		r, ok := s.reserveStop("a")
		require.True(t, ok)
		var stopped atomic.Bool
		var promoted bool
		var wg sync.WaitGroup
		start := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			promoted = s.promoteStop("a", r, func() { stopped.Store(true) })
		}()
		go func() {
			defer wg.Done()
			<-start
			s.StopSession("a")
		}()
		close(start)
		wg.Wait()
		if promoted {
			require.True(t, stopped.Load(), "iteration %d: promoted but the delete never reached the real stop", i)
			require.True(t, s.hasStop("a"))
		} else {
			require.False(t, stopped.Load(), "iteration %d", i)
			require.False(t, s.hasStop("a"), "iteration %d: refused promote leaves the slot free", i)
		}
	}
}
