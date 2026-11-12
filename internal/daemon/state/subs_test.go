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

// A nil list (a session nobody subscribed to) is empty; a range can stop
// early.
func TestSubs_NilAndEarlyStop(t *testing.T) {
	var none *Subs[func()]
	require.Zero(t, none.Len())
	for range none.All() {
		t.Fatal("nil list yielded")
	}
	var s Subs[int]
	s.Add(1)
	s.Add(2)
	for v := range s.All() {
		require.Equal(t, 1, v)
		break
	}
	require.Equal(t, []int{1, 2}, s.Clone())
}
