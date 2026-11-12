package tui

import (
	"fmt"

	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/features/overview"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// renderHeader renders the header (logo+counts row, chips row, overview line,
// padded to `width` columns. The counts follow the board columns, so every
// session lands in exactly one bucket and they add up to the stats
// overlay's total: queued counts as working (its column). The screensaver
// shows the same numbers (columnCounts).
func renderHeader(m Model, width int) string {
	out, _, _ := headerTopRow(m, width)
	return renderHeaderRest(m, out, width)
}

// headerTopRow is the logo and counts row with the usage meter at its right
// end, and the meter's columns [lo, hi).
func headerTopRow(m Model, width int) (row string, lo, hi int) {
	cols, failed := columnCounts(m.Sessions, rowFilterOf(m).ctx)

	marked := 0
	for _, s := range m.Sessions {
		if !s.RetireAt.IsZero() {
			marked++
		}
	}

	logo := styleLogo.Render("∴ REX")
	counts := renderHeaderCounts(m, cols[groupNeeds], cols[groupWaiting], cols[groupWorking], cols[groupDone], failed)
	mobile := mobileLayout(m)
	if marked > 0 && m.Detailed && !mobile { // marked rows already count as done
		counts += styleDim.Render(fmt.Sprintf("  · %d marked done", marked))
	}
	topRow := "  " + logo + counts

	if m.Detailed && !mobile {
		topRow += styleDim.Render("  · detailed (v)")
	}
	return headerTop(m, topRow, width)
}

// renderHeaderRest adds the rows under the top row: filter chips, the
// overview line and the summary backend banner.
func renderHeaderRest(m Model, out string, width int) string {
	mobile := mobileLayout(m)
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
	if showOverviewLine(m) {
		out += "\n" + renderOverviewLine(m, width)
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
		text := "  summary backend: " + reason + " — install: https://ollama.com  ·  pull: ollama pull " + model
		if mobile {
			text = "  summary backend: " + reason
		}
		banner := styleDim.Render(text)
		out += "\n" + padLine(banner, width)
	}

	return out
}

// overviewOn reports whether the overview_enabled setting is on. The default
// (no store) is off.
func overviewOn(m Model) bool {
	if m.Store == nil {
		return false
	}
	on, _ := m.Store.Get("overview_enabled").(bool)
	return on
}

// showOverviewLine reports whether the header carries the overview line. It
// is reserved whenever the setting is on, so the board does not jump when a
// headline arrives or goes.
func showOverviewLine(m Model) bool { return overviewOn(m) }

// overviewLead prefixes the overview line.
const overviewLead = "  » "

// renderOverviewLine renders the overview line: how many sessions are ready
// for review (counted here, from the sessions, so it is true even when the
// headline is stale or wrong), then the daemon's headline and its age. The
// headline is untrusted model text: flattened and cut to the room left. When
// the daemon has sent none (older daemon, overview off there, nothing to say
// yet) only the count shows.
func renderOverviewLine(m Model, width int) string {
	review := len(overview.Group(m.Sessions)[overview.Review])
	count := "nothing to review"
	countStyle := styleDim
	if review > 0 {
		count = fmt.Sprintf("%d to review", review)
		countStyle = styleNeeds
	}
	line := styleDim.Render(overviewLead) + countStyle.Render(count)
	if o := m.Overview; o != nil && o.Enabled {
		if headline := protocol.FlattenDisplayText(o.Headline); headline != "" {
			age := ""
			if !o.GeneratedAt.IsZero() {
				age = " · " + durationAgoAt(m.now(), o.GeneratedAt)
			}
			room := width - ansi.StringWidth(overviewLead+count) - 2 - ansi.StringWidth(age)
			if room >= minHeadlineCells {
				line += "  " + styleHeaderMeta.Render(truncateWords(headline, room))
				if age != "" {
					line += styleMuted.Render(age)
				}
			}
		}
	}
	return padLine(line, width)
}

// minHeadlineCells is the least room worth showing a headline in.
const minHeadlineCells = 8

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
// verbose: "  4 ready · 1 waiting · 2 working · 7 done"
// glyphs:  "  ◆ 4  ◷ 1  ⟳ 2  ● 7"
// numbers: "  4 · 3 · 7" (waiting folded into working: a bare number has no
// label to say it is a fourth bucket)
// The waiting count is left out while it is 0.
func renderHeaderCounts(m Model, needs, waiting, working, done, failed int) string {
	style := "verbose"
	if m.Store != nil {
		if v, _ := m.Store.Get("header_style").(string); v != "" {
			style = v
		}
	}
	if mobileLayout(m) {
		style = "glyphs" // the short counts: the verbose ones are 45 columns
	}
	var s string
	switch style {
	case "glyphs":
		wait := ""
		if waiting > 0 {
			wait = fmt.Sprintf("%s %d  ", waitingGlyph, waiting)
		}
		s = fmt.Sprintf("    ◆ %d  %s⟳ %d  ● %d", needs, wait, working, done)
	case "numbers":
		s = fmt.Sprintf("    %d · %d · %d", needs, working+waiting, done)
	default:
		wait := ""
		if waiting > 0 {
			wait = fmt.Sprintf(" · %d %s", waiting, boardGroups[groupWaiting].Count)
		}
		s = fmt.Sprintf("    %d ready%s · %d working · %d done", needs, wait, working, done)
	}
	out := styleHeaderMeta.Render(s)
	if failed > 0 {
		if style == "verbose" {
			out += styleStateFailed.Render(fmt.Sprintf("  · %d failed", failed))
		} else {
			out += styleStateFailed.Render(fmt.Sprintf("  ✕ %d", failed))
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
