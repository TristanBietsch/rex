package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/catalog/theme"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// The summary scanner is a Larson scanner (the KITT / Cylon / RoboCop visor
// sweep): a short band of color that runs across the summary line under the
// board and back, while the selected session is working or waiting. The band
// is a bright head with a fading tail on each side, drawn in tiers (bold,
// plain, faint) rather than blended hex values, so it works on themes that
// use the terminal's own colors.

const (
	// scanHalf is how many cells on each side of the head the band lights.
	scanHalf = 3
	// scanCellTime is how long the head takes to move one cell.
	scanCellTime = 50 * time.Millisecond
	// scanMinSweep and scanMaxSweep bound one pass, so a short line does not
	// flicker and a long one does not crawl.
	scanMinSweep = 1200 * time.Millisecond
	scanMaxSweep = 4 * time.Second
)

// settingBool is a bool setting from st, or its registry default when st is
// nil or holds no bool for id. Unlike settings.FeatureOn it takes any
// section's bool.
func settingBool(st *settings.Store, id string) bool {
	if st != nil {
		if b, ok := st.Get(id).(bool); ok {
			return b
		}
	}
	if s, ok := settings.Find(id); ok {
		b, _ := s.Default.(bool)
		return b
	}
	return false
}

// scanActive reports whether the scanner runs over s's summary: the
// summary_scan setting is on, reduce_motion is off, and s sits in Working or
// Waiting (working or queued, not marked done). Waiting sessions are working
// ones blocked on a tool, so the state test covers both sections.
func scanActive(m Model, s protocol.SessionSummary) bool {
	if !settingBool(m.Store, "summary_scan") || settingBool(m.Store, "reduce_motion") {
		return false
	}
	if isMarkedDone(s) {
		return false
	}
	return s.State == protocol.StateWorking || s.State == protocol.StateQueued
}

// scanHead is the head's cell index at elapsed for a line of n cells. The
// head runs from scanHalf cells before the text to scanHalf cells past it
// and back (a ping-pong), so the band slides fully in and out at each end.
func scanHead(n int, elapsed time.Duration) int {
	if n <= 0 {
		return -scanHalf - 1
	}
	span := n + 2*scanHalf // head positions in one pass
	sweep := min(max(time.Duration(span)*scanCellTime, scanMinSweep), scanMaxSweep)
	period := 2 * sweep
	t := elapsed % period
	if t < 0 {
		t += period
	}
	f := float64(t) / float64(sweep) // 0..2
	if f > 1 {
		f = 2 - f // on the way back
	}
	return int(f*float64(span-1)+0.5) - scanHalf
}

// scanTier is the band tier of a cell distance d from the head: 0 head
// (bold), 1 near (plain), 2 tail (faint), 3 outside the band.
func scanTier(d int) int {
	if d < 0 {
		d = -d
	}
	switch {
	case d == 0:
		return 0
	case d == 1:
		return 1
	case d <= scanHalf:
		return 2
	}
	return 3
}

// renderScan styles text cell by cell: cells in the band in the scanner
// color (by tier), the rest in base. offset is the text's first cell index
// on the scanner's line, so a wrapped preview shares one band across its
// lines. Runs of one tier are rendered together.
func renderScan(text string, offset, head int, color lipgloss.Color, base lipgloss.Style) string {
	band := scanStyles(color)
	tiers := [4]lipgloss.Style{band[0], band[1], band[2], base}
	var b, run strings.Builder
	cur := -1
	flush := func() {
		if run.Len() > 0 {
			b.WriteString(tiers[cur].Render(run.String()))
			run.Reset()
		}
	}
	i := offset
	for _, r := range text {
		t := scanTier(i - head)
		if t != cur {
			flush()
			cur = t
		}
		run.WriteRune(r)
		i++
	}
	flush()
	return b.String()
}

// scanMemo caches the resolved summary_scan_color for one value on one
// palette (paletteGen), so a frame does not re-run the contrast search.
var scanMemo struct {
	value string
	gen   int
	color lipgloss.Color
}

// scanColor is the scanner color: the live accent for "accent" (or an
// unusable value), else the summary_scan_color value laid over the live
// palette the way accent_color is (contrast-adjusted against the
// background).
func scanColor(m Model) lipgloss.Color {
	v := settings.ScanColorAccent
	if m.Store != nil {
		v = m.Store.String("summary_scan_color")
	}
	if v == "" || v == settings.ScanColorAccent {
		return colorAccent
	}
	if scanMemo.value == v && scanMemo.gen == paletteGen {
		return scanMemo.color
	}
	c := colorAccent
	if p, _, err := theme.WithAccent(livePalette, v); err == nil {
		c = lipgloss.Color(p.Accent)
	}
	scanMemo.value, scanMemo.gen, scanMemo.color = v, paletteGen, c
	return c
}

// scanning reports whether the scanner is on screen: the preview line shows
// and its session scans. Like the spinner, it needs the SpinnerTick chain.
func (m Model) scanning() bool {
	if previewRows(m) == 0 {
		return false
	}
	for _, s := range m.Sessions {
		if s.ID == m.SelectedID {
			return scanActive(m, s)
		}
	}
	return false
}
