package tui

import (
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/tristanbietsch/rex/internal/protocol"
)

// boardGroup defines one section of the kanban. All board grouping (rendering,
// navigation, jump-to-section) reads from boardGroups so the three views stay
// in sync. Adding a new state means updating exactly one place.
type boardGroup struct {
	Title string
	Match func(protocol.State) bool
}

// boardGroups is the canonical kanban layout. Completed includes all terminal
// states (done/failed/crashed); distinct markers (●/✕/○) preserve the
// at-a-glance distinction within the column.
var boardGroups = []boardGroup{
	{"Needs input", func(s protocol.State) bool { return s == protocol.StateNeedsInput }},
	{"Working", func(s protocol.State) bool { return s == protocol.StateWorking }},
	{"Completed", func(s protocol.State) bool {
		return s == protocol.StateDone || s == protocol.StateFailed || s == protocol.StateCrashed
	}},
}

// filterByGroup applies the group predicate plus the active tool filter.
func filterByGroup(sessions []protocol.SessionSummary, g boardGroup, filter string) []protocol.SessionSummary {
	out := make([]protocol.SessionSummary, 0, len(sessions))
	for _, s := range sessions {
		if !g.Match(s.State) {
			continue
		}
		if filter != "all" && filter != "" && s.ToolID != filter {
			continue
		}
		out = append(out, s)
	}
	return out
}

// fleetPalette is a small set of distinct lipgloss colors that work on both
// dark and light terminals. The color for a fleet name is selected by:
//
//	fnv32(name) % len(fleetPalette)
//
// This ensures the same name always maps to the same color.
var fleetPalette = []lipgloss.Color{
	"#5B8DEF", // blue
	"#E5B341", // amber
	"#4ADE80", // green
	"#EF4444", // red
	"#A78BFA", // violet
	"#22D3EE", // cyan
	"#FB923C", // orange
	"#F472B6", // pink
	"#84CC16", // lime
	"#14B8A6", // teal
}

// fleetColor returns a stable, deterministic color for the given fleet name.
func fleetColor(name string) lipgloss.Color {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	idx := int(h.Sum32()) % len(fleetPalette)
	return fleetPalette[idx]
}

// spinnerFrameSets is the full catalog of spinner glyph sets. Selected at
// render time via the `spinner` setting.
var spinnerFrameSets = map[string][]string{
	"braille":    {"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
	"ascii_line": {"|", "/", "-", "\\"},
	"moon":       {"◐", "◓", "◑", "◒"},
	"pulse":      {"·", "•", "●", "•"},
	"blocks":     {"░", "▒", "▓", "█", "▓", "▒"},
}

// spinnerFramesFor returns the active spinner frame set, honoring reduce_motion.
func spinnerFramesFor(m Model) []string {
	if m.Store != nil {
		if rm, _ := m.Store.Get("reduce_motion").(bool); rm {
			return []string{"*"}
		}
		if id, _ := m.Store.Get("spinner").(string); id != "" {
			if frames, ok := spinnerFrameSets[id]; ok {
				return frames
			}
		}
	}
	return spinnerFrameSets["braille"]
}

// renderBoard renders the three sections sized to fit `width` x `height`.
// Long boards scroll: m.ScrollOffset skips that many lines from the top.
func renderBoard(m Model, width, height int) string {
	gapBetween := densityGap(m)
	var lines []string
	for i, g := range boardGroups {
		rows := filterByGroup(m.Sessions, g, m.Filter)
		if i > 0 {
			for j := 0; j < gapBetween; j++ {
				lines = append(lines, "")
			}
		}
		lines = append(lines, "  "+styleSectionTitle.Render(g.Title))
		if len(rows) == 0 {
			lines = append(lines, "    "+styleMuted.Render("(none)"))
		} else {
			for _, s := range rows {
				lines = append(lines, renderRow(m, s, width))
			}
		}
	}

	// Apply scroll: skip the first ScrollOffset lines.
	off := m.ScrollOffset
	if off < 0 {
		off = 0
	}
	if off > len(lines) {
		off = len(lines)
	}
	lines = lines[off:]

	// Show only `height` rows of board content; pad if shorter, truncate if longer.
	if height > 0 && len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}

	return strings.Join(lines, "\n")
}

// Column widths in the row grid (mockup: 1.4ch 5ch 22ch 1fr 18ch 5ch with ~1ch gaps).
const (
	colMarker = 1
	colID     = 4
	colSlug   = 22
	colModel  = 18
	colTime   = 5
	colGap    = 2
	rowIndent = 3 // " " + arrow/space + " "
)

func rowLayout(width int) (descW int) {
	used := rowIndent + colMarker + colGap + colID + colGap + colSlug + colGap + colGap + colModel + colGap + colTime
	descW = width - used
	if descW < 8 {
		descW = 8
	}
	return descW
}

func renderRow(m Model, s protocol.SessionSummary, width int) string {
	descW := rowLayout(width)
	selected := m.SelectedID == s.ID

	// Marker is the only indicator for the selected row — no bg fill,
	// avoids the chunky highlight artifacts.
	var markerPrefix string
	if selected {
		markerPrefix = styleArrow.Render("▸")
	} else if s.Fleet != "" {
		// Fleet indicator: tinted left border using the fleet's stable color.
		markerPrefix = lipgloss.NewStyle().Foreground(fleetColor(s.Fleet)).Render("▍")
	} else {
		markerPrefix = " "
	}

	marker := stateMarkerCellFor(m, s.State, m.SpinnerTick, false)
	id := lipgloss.NewStyle().Foreground(colorFgMuted).Width(colID).Render(truncate(s.ShortID, colID))
	slugCol := colorFgPrimary
	if !selected {
		slugCol = colorFgPrimary
	}
	slug := lipgloss.NewStyle().Foreground(slugCol).Bold(true).Width(colSlug).Render(truncate(s.Slug, colSlug))
	descColor := colorFgDim
	if selected {
		descColor = colorFgPrimary
	}
	descSource := s.Description
	if descSource == "" {
		descSource = s.LastLine // bootstrap fallback
	}
	if anim, ok := m.DescAnim[s.ID]; ok && anim.Active(time.Now()) {
		descSource = renderAnimFrame(anim, descW, time.Now())
	}
	desc := lipgloss.NewStyle().Foreground(descColor).Width(descW).Render(truncate(descSource, descW))
	model := lipgloss.NewStyle().Foreground(colorFgDim).Width(colModel).Render(truncate(modelLabel(s), colModel))
	t := lipgloss.NewStyle().Foreground(colorFgDim).Width(colTime).Align(lipgloss.Right).Render(durationAgo(s.LastEventAt))

	gap := strings.Repeat(" ", colGap)
	indent := " " + markerPrefix + " " // 3 chars total: marker arrow indent

	row := indent + marker + gap + id + gap + slug + gap + desc + gap + model + gap + t

	if blinkEnabled(m) {
		if until, ok := m.BlinkUntil[s.ID]; ok && time.Now().Before(until) {
			if time.Now().UnixMilli()/200%2 == 0 {
				row = lipgloss.NewStyle().Foreground(colorDone).Render(row)
			}
		}
	}
	return row
}

// densityGap returns the number of blank lines to insert between section
// groups based on the row_density setting.
func densityGap(m Model) int {
	if m.Store == nil {
		return 1
	}
	switch v, _ := m.Store.Get("row_density").(string); v {
	case "compact":
		return 0
	case "roomy":
		return 2
	default:
		return 1
	}
}

// blinkEnabled returns true when "done" rows should flash green. Off when the
// blinking_enabled setting is false or reduce_motion is on.
func blinkEnabled(m Model) bool {
	if m.Store == nil {
		return true
	}
	if rm, _ := m.Store.Get("reduce_motion").(bool); rm {
		return false
	}
	b, ok := m.Store.Get("blinking_enabled").(bool)
	if !ok {
		return true
	}
	return b
}

// stateMarkerCellFor renders the marker glyph for a state, sourcing the
// spinner from the live store on m.
func stateMarkerCellFor(m Model, st protocol.State, tick int, selected bool) string {
	style := lipgloss.NewStyle().Bold(true).Width(colMarker)
	if selected {
		style = style.Background(colorBgElev)
	}
	switch st {
	case protocol.StateWorking:
		frames := spinnerFramesFor(m)
		return style.Foreground(colorWorking).Render(frames[tick%len(frames)])
	case protocol.StateNeedsInput:
		return style.Foreground(colorNeeds).Render("◆")
	case protocol.StateDone:
		return style.Foreground(colorDone).Render("●")
	case protocol.StateFailed:
		return style.Foreground(colorFailed).Render("✕")
	case protocol.StateCrashed:
		return style.Foreground(colorCrashed).Render("○")
	}
	return style.Render(" ")
}

func modelLabel(s protocol.SessionSummary) string {
	base := s.ModelID
	if s.Effort != "" {
		base = base + " · " + s.Effort
	}
	if s.Tokens > 0 {
		base = base + " · " + formatTokens(s.Tokens)
	}
	return base
}

// formatTokens humanizes a token count for the board cell.
// < 1000  → "<N> tk"
// < 100000 → "<N.N>K tk"
// ≥ 100000 → "<N>K tk"
func formatTokens(n int64) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d tk", n)
	case n < 100000:
		k := float64(n) / 1000.0
		return fmt.Sprintf("%.1fK tk", k)
	default:
		k := n / 1000
		return fmt.Sprintf("%dK tk", k)
	}
}

func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n == 1 {
		return string(runes[:1])
	}
	return string(runes[:n-1]) + "…"
}

func padOrTruncate(s string, n int) string {
	if len(s) > n {
		if n <= 1 {
			return s[:n]
		}
		return s[:n-1] + "…"
	}
	return s + strings.Repeat(" ", n-len(s))
}

func durationAgo(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours())/24)
	}
}
