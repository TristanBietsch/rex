package tui

import (
	"testing"

	"github.com/stretchr/testify/require"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/tristanbietsch/rex/internal/protocol"
)

func TestTTFRBucket(t *testing.T) {
	cases := []struct {
		secs float64
		want int
	}{
		{0.5, 0},  // <1s
		{1.0, 1},  // 1-3s
		{2.9, 1},
		{3.0, 2},  // 3-10s
		{9.9, 2},
		{10.0, 3}, // 10-30s
		{29.9, 3},
		{30.0, 4}, // 30s-2m
		{119.9, 4},
		{120.0, 5}, // 2-5m
		{299.9, 5},
		{300.0, 6}, // 5m+
		{9999, 6},
	}
	for _, c := range cases {
		got := ttfrBucket(c.secs)
		require.Equal(t, c.want, got, "ttfrBucket(%v)", c.secs)
	}
}

func TestDurationBucket(t *testing.T) {
	cases := []struct {
		secs float64
		want int
	}{
		{0, 0},     // <1m
		{59.9, 0},
		{60.0, 1},  // 1-5m
		{299.9, 1},
		{300.0, 2}, // 5-15m
		{899.9, 2},
		{900.0, 3}, // 15-60m
		{3599.9, 3},
		{3600.0, 4}, // 1h+
		{99999, 4},
	}
	for _, c := range cases {
		got := durationBucket(c.secs)
		require.Equal(t, c.want, got, "durationBucket(%v)", c.secs)
	}
}

func TestHistogramCounts(t *testing.T) {
	values := []float64{0.5, 1.5, 2.0, 10.0, 400.0}
	buckets := make([]int, 7)
	histogramCounts(values, buckets, ttfrBucket)
	require.Equal(t, 1, buckets[0], "<1s")
	require.Equal(t, 2, buckets[1], "1-3s")
	require.Equal(t, 0, buckets[2], "3-10s")
	require.Equal(t, 1, buckets[3], "10-30s")
	require.Equal(t, 0, buckets[4], "30s-2m")
	require.Equal(t, 0, buckets[5], "2-5m")
	require.Equal(t, 1, buckets[6], "5m+")
}

func TestStatsOverlay_OpenClose(t *testing.T) {
	m := Model{
		Focus:  FocusBoard,
		Width:  120,
		Height: 40,
	}

	// :stats command opens the overlay.
	m2, _ := executeCommand(m, "stats")
	require.Equal(t, FocusStats, m2.Focus)

	// :stats again closes it.
	m3, _ := executeCommand(m2, "stats")
	require.Equal(t, FocusBoard, m3.Focus)

	// Esc closes the overlay.
	m4, _ := updateKey(m2, tea.KeyMsg{Type: tea.KeyEsc})
	require.Equal(t, FocusBoard, m4.Focus)
}

func TestStatsOverlay_Render(t *testing.T) {
	m := Model{
		Focus:  FocusStats,
		Width:  120,
		Height: 40,
		Sessions: []protocol.SessionSummary{
			{ID: "1", State: protocol.StateDone, Tokens: 1500, OutputBytes: 6000},
			{ID: "2", State: protocol.StateWorking, Tokens: 200},
		},
	}
	out := m.View()
	require.NotEmpty(t, out)
	require.Contains(t, out, "Stats")
}
