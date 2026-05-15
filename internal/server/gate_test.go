package server

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConcurrencyGate_Uncapped(t *testing.T) {
	g := &concurrencyGate{max: 0}
	for i := 0; i < 100; i++ {
		require.True(t, g.TryAcquire())
	}
}

func TestConcurrencyGate_Capped(t *testing.T) {
	g := &concurrencyGate{max: 2}
	require.True(t, g.TryAcquire())
	require.True(t, g.TryAcquire())
	require.False(t, g.TryAcquire(), "third acquire should fail at cap 2")
	g.Release()
	require.True(t, g.TryAcquire(), "release should free a slot")
}

func TestConcurrencyGate_SetMax_RaisesCap(t *testing.T) {
	g := &concurrencyGate{max: 1}
	require.True(t, g.TryAcquire())
	require.False(t, g.TryAcquire())
	g.SetMax(3)
	require.True(t, g.TryAcquire())
	require.True(t, g.TryAcquire())
	require.False(t, g.TryAcquire())
}

func TestConcurrencyGate_SetMax_LowersCapBelowActive(t *testing.T) {
	g := &concurrencyGate{max: 5}
	for i := 0; i < 3; i++ {
		require.True(t, g.TryAcquire())
	}
	g.SetMax(2)
	require.False(t, g.TryAcquire(), "active(3) >= max(2), should reject")
	g.Release()
	require.False(t, g.TryAcquire(), "still active(2) == max(2)")
	g.Release()
	require.True(t, g.TryAcquire(), "active(1) < max(2), should accept")
}

func TestConcurrencyGate_SetMax_Uncap(t *testing.T) {
	g := &concurrencyGate{max: 1}
	require.True(t, g.TryAcquire())
	require.False(t, g.TryAcquire())
	g.SetMax(0)
	require.True(t, g.TryAcquire())
}
