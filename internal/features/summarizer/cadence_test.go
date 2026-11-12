package summarizer

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCadence_DueAnchoredTicks(t *testing.T) {
	anchor := time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)
	c := DefaultCadence
	want := []time.Duration{5, 10, 15, 20, 25, 55, 85, 115}
	for i, m := range want {
		require.Equal(t, anchor.Add(m*time.Minute), c.Due(anchor, i+1), "tick %d", i+1)
	}
}

func TestCadence_Late(t *testing.T) {
	c := DefaultCadence
	for k := 1; k <= 5; k++ {
		require.False(t, c.Late(k), "tick %d is early", k)
	}
	for k := 6; k <= 9; k++ {
		require.True(t, c.Late(k), "tick %d is late", k)
	}
}

func TestCadence_Elapsed(t *testing.T) {
	anchor := time.Unix(0, 0)
	c := DefaultCadence
	require.Equal(t, 0, c.Elapsed(anchor, anchor.Add(4*time.Minute)))
	require.Equal(t, 1, c.Elapsed(anchor, anchor.Add(5*time.Minute)))
	require.Equal(t, 5, c.Elapsed(anchor, anchor.Add(54*time.Minute)))
	require.Equal(t, 6, c.Elapsed(anchor, anchor.Add(55*time.Minute)))
	require.Equal(t, 0, c.Elapsed(anchor, anchor.Add(-time.Hour)))
}

func TestCadence_ZeroValueIsDefault(t *testing.T) {
	require.Equal(t, DefaultCadence, Cadence{}.orDefault())
	c := Cadence{EarlyEvery: time.Second, EarlyCount: 2, LateEvery: 3 * time.Second}
	require.Equal(t, c, c.orDefault())
}

func TestDetailUp(t *testing.T) {
	require.Equal(t, DetailMedium, DetailShort.Up())
	require.Equal(t, DetailLong, DetailMedium.Up())
	require.Equal(t, DetailLong, DetailLong.Up())
	require.Equal(t, 180, DetailLong.Up().MaxRunes())
}
