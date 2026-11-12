package state

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Canceled subscriptions are removed, not left as nil slots: every client
// connection subscribes, so the lists grew for the daemon's lifetime and
// every broadcast walked the dead slots.
func TestSubs_CancelRemoves(t *testing.T) {
	var s Subs[func(int)]
	var got []int
	a := s.Add(func(v int) { got = append(got, 10+v) })
	b := s.Add(func(v int) { got = append(got, 20+v) })
	s.Each(func(fn func(int)) { fn(1) })
	require.Equal(t, []int{11, 21}, got)

	s.Remove(a)
	s.Remove(a) // twice is harmless
	got = nil
	s.Each(func(fn func(int)) { fn(2) })
	require.Equal(t, []int{22}, got)
	require.Equal(t, 1, s.Len())
	s.Remove(b)
	require.Zero(t, s.Len())
	require.Empty(t, s.Clone())
}

func TestStoreSubscribe_CancelRemoves(t *testing.T) {
	st := NewStore()
	for range 1000 {
		cancel := st.Subscribe(func(Event) {})
		cancel()
	}
	st.subsMu.RLock()
	defer st.subsMu.RUnlock()
	require.Zero(t, st.subscribers.Len())
}
