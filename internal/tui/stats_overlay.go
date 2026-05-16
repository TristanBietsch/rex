package tui

import (
	"fmt"
	"strings"

	"github.com/tristanbietsch/rex/internal/protocol"
)

// ttfrBucket returns the index of the bucket for a time-to-first-response duration in seconds.
// Buckets: <1s, 1-3s, 3-10s, 10-30s, 30s-2m, 2-5m, 5m+
func ttfrBucket(seconds float64) int {
	switch {
	case seconds < 1:
		return 0
	case seconds < 3:
		return 1
	case seconds < 10:
		return 2
	case seconds < 30:
		return 3
	case seconds < 120:
		return 4
	case seconds < 300:
		return 5
	default:
		return 6
	}
}

// durationBucket returns the index of the bucket for a wall-clock session duration in seconds.
// Buckets: <1m, 1-5m, 5-15m, 15-60m, 1h+
func durationBucket(seconds float64) int {
	switch {
	case seconds < 60:
		return 0
	case seconds < 300:
		return 1
	case seconds < 900:
		return 2
	case seconds < 3600:
		return 3
	default:
		return 4
	}
}

// histogramCounts fills a bucket slice from a slice of per-session values and a bucket function.
// buckets must be pre-allocated to the desired length.
func histogramCounts(values []float64, buckets []int, fn func(float64) int) {
	for _, v := range values {
		idx := fn(v)
		if idx >= 0 && idx < len(buckets) {
			buckets[idx]++
		}
	}
}

// renderStatsOverlay renders the :stats overlay content (no border — border is
// applied by centerOverlay).
func renderStatsOverlay(m Model) string {
	sessions := m.Sessions

	// State counts.
	var working, needsInput, done, failed, crashed, queued int
	for _, s := range sessions {
		switch s.State {
		case protocol.StateWorking:
			working++
		case protocol.StateNeedsInput:
			needsInput++
		case protocol.StateDone:
			done++
		case protocol.StateFailed:
			failed++
		case protocol.StateCrashed:
			crashed++
		case protocol.StateQueued:
			queued++
		}
	}

	// Token totals.
	var totalTokens, totalOutputBytes int64
	for _, s := range sessions {
		totalTokens += s.Tokens
		totalOutputBytes += s.OutputBytes
	}

	// TTFR histogram: use (LastEventAt - StartedAt) as a proxy for done/working sessions.
	// A proper TTFR would require the first output event timestamp which we don't persist;
	// this is the best available approximation.
	ttfrLabels := []string{"<1s", "1-3s", "3-10s", "10-30s", "30s-2m", "2-5m", "5m+"}
	ttfrBuckets := make([]int, len(ttfrLabels))
	var ttfrValues []float64
	for _, s := range sessions {
		if s.State == protocol.StateDone || s.State == protocol.StateWorking {
			if !s.StartedAt.IsZero() && !s.LastEventAt.IsZero() {
				ttfrValues = append(ttfrValues, s.LastEventAt.Sub(s.StartedAt).Seconds())
			}
		}
	}
	histogramCounts(ttfrValues, ttfrBuckets, ttfrBucket)

	// Duration histogram: (LastEventAt - StartedAt) for all terminal sessions.
	durLabels := []string{"<1m", "1-5m", "5-15m", "15-60m", "1h+"}
	durBuckets := make([]int, len(durLabels))
	var durValues []float64
	for _, s := range sessions {
		if !s.StartedAt.IsZero() && !s.LastEventAt.IsZero() {
			durValues = append(durValues, s.LastEventAt.Sub(s.StartedAt).Seconds())
		}
	}
	histogramCounts(durValues, durBuckets, durationBucket)

	var lines []string

	// Title.
	lines = append(lines, styleSectionTitle.Render("Session Stats"))
	lines = append(lines, "")

	// State counts.
	lines = append(lines, styleDim.Render("  State counts"))
	lines = append(lines, fmt.Sprintf("  %-16s %d", "needs input", needsInput))
	lines = append(lines, fmt.Sprintf("  %-16s %d", "working", working))
	lines = append(lines, fmt.Sprintf("  %-16s %d", "queued", queued))
	lines = append(lines, fmt.Sprintf("  %-16s %d", "done", done))
	lines = append(lines, fmt.Sprintf("  %-16s %d", "failed", failed))
	lines = append(lines, fmt.Sprintf("  %-16s %d", "crashed", crashed))
	lines = append(lines, fmt.Sprintf("  %-16s %d", "total", len(sessions)))
	lines = append(lines, "")

	// Token totals (only shown if Feature A landed and any tokens were counted).
	if totalTokens > 0 || totalOutputBytes > 0 {
		lines = append(lines, styleDim.Render("  Token usage (approx)"))
		lines = append(lines, fmt.Sprintf("  %-16s %s", "total tokens", formatTokens(totalTokens)))
		lines = append(lines, fmt.Sprintf("  %-16s %.1f KB", "output bytes", float64(totalOutputBytes)/1024))
		lines = append(lines, "")
	}

	// TTFR histogram.
	lines = append(lines, styleDim.Render("  Time-to-first-response"))
	lines = append(lines, renderHistogram(ttfrLabels, ttfrBuckets))
	lines = append(lines, "")

	// Duration histogram.
	lines = append(lines, styleDim.Render("  Session duration"))
	lines = append(lines, renderHistogram(durLabels, durBuckets))
	lines = append(lines, "")

	lines = append(lines, styleMuted.Render("  esc or :stats to close"))

	return strings.Join(lines, "\n")
}

// renderHistogram renders a compact ASCII bar chart for the given labels/counts.
func renderHistogram(labels []string, counts []int) string {
	// Find max for scaling.
	maxCount := 1
	for _, c := range counts {
		if c > maxCount {
			maxCount = c
		}
	}
	const barWidth = 20
	var sb strings.Builder
	for i, label := range labels {
		n := counts[i]
		filled := n * barWidth / maxCount
		bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
		sb.WriteString(fmt.Sprintf("  %-8s %s %d\n", label, styleDim.Render(bar), n))
	}
	return strings.TrimRight(sb.String(), "\n")
}
