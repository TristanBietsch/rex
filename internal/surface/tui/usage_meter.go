package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Header usage meter (R10-6): the right end of the header's top row. Compact
// shows each live tool's worst quota window as a bar and a percent; full
// shows every window with its reset time. Colors come from quotaStyle.

const (
	meterBarCells = 5
	meterGap      = 2 // the least space between the counts and the meter
	meterMargin   = 2 // space right of the meter
)

// usageMeterMode is the usage_meter setting, "off" while usage is off or the
// daemon has sent no quotas.
func usageMeterMode(m Model) string {
	if !m.featureOn("usage") || !m.featureOn("usage_meter") {
		return "off"
	}
	if m.Usage == nil || !m.Usage.Enabled {
		return "off"
	}
	if m.Store == nil {
		return "compact"
	}
	return m.Store.String("usage_meter")
}

// meterTools groups the plan-wide quotas of tools with a live session by
// tool: those with a percentage (full mode also takes a spend limit).
func meterTools(m Model, full bool) map[string][]protocol.UsageQuota {
	live := map[string]bool{}
	for _, s := range m.Sessions {
		if !isTerminalState(s.State) {
			live[s.ToolID] = true
		}
	}
	out := map[string][]protocol.UsageQuota{}
	for _, q := range m.Usage.Quotas {
		if !live[q.Tool] || q.Model != "" {
			continue
		}
		if q.UsedPct == nil && (!full || q.UsedUSD == nil || q.LimitUSD == nil) {
			continue
		}
		out[q.Tool] = append(out[q.Tool], q)
	}
	return out
}

// meterEntry is one tool's part of the meter, plain and styled.
func meterEntry(m Model, tool string, qs []protocol.UsageQuota, full bool) (plain, styled string) {
	name := truncate(tool, 12)
	plain, styled = name, styleDim.Render(name)
	add := func(q protocol.UsageQuota, label bool) {
		stale := usageStale(m.now(), q)
		var cell string
		var pct float64
		if q.UsedPct != nil {
			pct = *q.UsedPct
			cell = usageBar(pct, meterBarCells) + fmt.Sprintf(" %.0f%%", pct)
		} else {
			cell = fmt.Sprintf("$%.0f/$%.0f", *q.UsedUSD, *q.LimitUSD)
			if *q.LimitUSD > 0 {
				pct = *q.UsedUSD / *q.LimitUSD * 100
			}
		}
		if label {
			cell = usageWindow(q.Window) + " " + cell
		}
		if full && !stale {
			if r := usageResetsIn(m.now(), q.ResetsAt); r != "" {
				cell += " " + r
			}
		}
		if stale {
			cell += "?" // a text marker: the dim color alone is lost without color
		}
		plain += " " + cell
		styled += " " + quotaStyle(pct, stale).Render(cell)
	}
	if full {
		for _, q := range qs {
			add(q, true)
		}
		return plain, styled
	}
	worst := qs[0]
	for _, q := range qs[1:] {
		if pctOf(q) > pctOf(worst) {
			worst = q
		}
	}
	add(worst, false)
	return plain, styled
}

func pctOf(q protocol.UsageQuota) float64 {
	if q.UsedPct != nil {
		return *q.UsedPct
	}
	return 0
}

// usageMeter renders the meter for room cells and returns it with its width,
// leaving out tools from the right until it fits; "" when none does.
func usageMeter(m Model, room int) (string, int) {
	mode := usageMeterMode(m)
	if mode == "off" {
		return "", 0
	}
	full := mode == "full"
	byTool := meterTools(m, full)
	tools := make([]string, 0, len(byTool))
	for t := range byTool {
		tools = append(tools, t)
	}
	slices.Sort(tools)
	var plains, styleds []string
	for _, t := range tools {
		p, s := meterEntry(m, t, byTool[t], full)
		plains, styleds = append(plains, p), append(styleds, s)
	}
	sep := " · "
	for n := len(plains); n > 0; n-- {
		w := ansi.StringWidth(strings.Join(plains[:n], sep))
		if w <= room {
			return strings.Join(styleds[:n], styleMuted.Render(sep)), w
		}
	}
	return "", 0
}

// headerTop is the header's top row padded to width, and the columns
// [lo, hi) of its usage meter (0, 0 when it shows none). The meter yields to
// the counts: it only takes the room they leave.
func headerTop(m Model, topRow string, width int) (row string, lo, hi int) {
	room := width - ansi.StringWidth(topRow) - meterGap - meterMargin
	meter, mw := usageMeter(m, room)
	if mw == 0 {
		return padLine(topRow, width), 0, 0
	}
	lo = width - meterMargin - mw
	gap := lo - ansi.StringWidth(topRow)
	return topRow + strings.Repeat(" ", gap) + meter + strings.Repeat(" ", meterMargin), lo, lo + mw
}

// headerMeterHit reports whether a click at screen (x, y) is on the meter.
func headerMeterHit(m Model, x, y int) bool {
	if y-1 != 0 {
		return false
	}
	w := m.Width
	if w <= 0 {
		w = 100
	}
	_, lo, hi := headerTopRow(m, contentWidth(w))
	return hi > lo && x >= lo && x < hi
}
