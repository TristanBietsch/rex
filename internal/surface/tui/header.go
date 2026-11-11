package tui

import (
	"fmt"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// renderHeader renders the two-line header (logo+counts row, chips row),
// padded to `width` columns. The counts follow the board columns, so every
// session lands in exactly one bucket and they add up to the stats
// overlay's total: queued counts as working (its column). The screensaver
// shows the same numbers (columnCounts).
func renderHeader(m Model, width int) string {
	cols, failed := columnCounts(m.Sessions)

	marked := 0
	for _, s := range m.Sessions {
		if !s.RetireAt.IsZero() {
			marked++
		}
	}

	logo := styleHeaderApp.Render("∴ REX")
	counts := renderHeaderCounts(m, cols[groupNeeds], cols[groupWorking], cols[groupDone], failed)
	if marked > 0 && m.Detailed { // marked rows already count as done
		counts += styleDim.Render(fmt.Sprintf("  · %d marked done", marked))
	}
	topRow := "  " + logo + counts

	if m.Detailed {
		topRow += styleDim.Render("  · detailed (v)")
	}
	out := padLine(topRow, width)
	if showFilterChips(m) {
		chips := ""
		if m.featureOn("board_filter") {
			chips = renderFilterChips(m)
		}
		if status := renderFilterStatus(m); status != "" {
			if chips != "" {
				chips += styleChipSep.Render("   ·  ")
			}
			chips += status
		}
		out += "\n" + padLine("  "+chips, width)
	}

	if m.BackendUnavailable {
		reason := protocol.FlattenDisplayText(m.BackendUnavailableReason) // daemon-supplied
		if reason == "" {
			reason = "ollama unreachable"
		}
		model := "gemma2:2b"
		if m.Store != nil {
			if v, _ := m.Store.Get("summary_model").(string); v != "" {
				model = v
			}
		}
		banner := styleDim.Render(
			"  summary backend: " + reason + " — install: https://ollama.com  ·  pull: ollama pull " + model,
		)
		out += "\n" + padLine(banner, width)
	}

	return out
}

// showFilterChips reports whether the tool filter row is worth a line: some
// filter is active (a tool, or a hidden section), or sessions run more than
// one tool.
func showFilterChips(m Model) bool {
	f := rowFilterOf(m)
	if (f.tool != "" && f.tool != "all") || len(f.hidden) > 0 || f.searching() {
		return true
	}
	if !m.featureOn("board_filter") {
		return false // the chips are the filter; only a search keeps the row
	}
	first := ""
	for _, s := range m.Sessions {
		if first == "" {
			first = s.ToolID
		} else if s.ToolID != first {
			return true
		}
	}
	return false
}

// renderHeaderCounts formats the counts segment of the header per header_style.
// verbose: "  4 need you · 2 working · 7 done"
// glyphs:  "  ◆4 ⟳2 ●7"
// numbers: "  4·2·7"
func renderHeaderCounts(m Model, needs, working, done, failed int) string {
	style := "verbose"
	if m.Store != nil {
		if v, _ := m.Store.Get("header_style").(string); v != "" {
			style = v
		}
	}
	var s string
	switch style {
	case "glyphs":
		s = fmt.Sprintf("    ◆%d  ⟳%d  ●%d", needs, working, done)
	case "numbers":
		s = fmt.Sprintf("    %d · %d · %d", needs, working, done)
	default:
		s = fmt.Sprintf("    %d need you · %d working · %d done", needs, working, done)
	}
	out := styleHeaderMeta.Render(s)
	if failed > 0 {
		if style == "verbose" {
			out += styleStateFailed.Render(fmt.Sprintf("  · %d failed", failed))
		} else {
			out += styleStateFailed.Render(fmt.Sprintf("  ✕%d", failed))
		}
	}
	return out
}

func renderFilterChips(m Model) string {
	out := ""
	for i, t := range filterTools {
		if i > 0 {
			out += styleChipSep.Render(" · ")
		}
		if t == m.Filter {
			out += styleChipActive.Render(t)
		} else {
			out += styleChipDim.Render(t)
		}
	}
	return out
}
