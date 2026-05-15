package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/tristanbietsch/rex/internal/protocol"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// renderBoard renders the three sections sized to fit `width` x `height`.
func renderBoard(m Model, width, height int) string {
	groups := []struct {
		title string
		state protocol.State
	}{
		{"Needs input", protocol.StateNeedsInput},
		{"Working", protocol.StateWorking},
		{"Completed", protocol.StateDone},
	}

	var lines []string
	for i, g := range groups {
		rows := filterByState(m.Sessions, g.state, m.Filter)
		if i > 0 {
			lines = append(lines, padLine("", width))
		}
		lines = append(lines, padLine("  "+styleSectionTitle.Render(g.title), width))
		if len(rows) == 0 {
			lines = append(lines, padLine("    "+styleMuted.Render("(none)"), width))
		} else {
			for _, s := range rows {
				lines = append(lines, renderRow(m, s, width))
			}
		}
	}

	// Truncate to height with a "more" indicator if necessary.
	if height > 0 && len(lines) > height {
		shown := lines[:height-1]
		extra := len(lines) - (height - 1)
		shown = append(shown, padLine("    "+styleMuted.Render(fmt.Sprintf("… %d more", extra)), width))
		lines = shown
	}

	for len(lines) < height {
		lines = append(lines, padLine("", width))
	}

	return strings.Join(lines, "\n")
}

func filterByState(sessions []protocol.SessionSummary, st protocol.State, filter string) []protocol.SessionSummary {
	out := make([]protocol.SessionSummary, 0, len(sessions))
	for _, s := range sessions {
		if s.State != st {
			continue
		}
		if filter != "all" && filter != "" && s.ToolID != filter {
			continue
		}
		out = append(out, s)
	}
	return out
}

// Column widths in the row grid (mockup: 1.4ch 5ch 22ch 1fr 18ch 5ch with ~1ch gaps).
const (
	colMarker = 1
	colID     = 4
	colSlug   = 22
	colModel  = 18
	colTime   = 5
	colGap    = 2
	rowIndent = 2
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

	bg := lipgloss.NoColor{}
	var rowStyle lipgloss.Style
	if selected {
		rowStyle = lipgloss.NewStyle().Background(colorBgElev)
	} else {
		rowStyle = lipgloss.NewStyle()
	}
	_ = bg
	cell := func(c lipgloss.Color, bold bool, text string, w int) string {
		st := lipgloss.NewStyle().Foreground(c).Inherit(rowStyle)
		if bold {
			st = st.Bold(true)
		}
		return st.Width(w).MaxWidth(w).Render(truncate(text, w))
	}
	timeCell := func(text string, w int) string {
		return lipgloss.NewStyle().Foreground(colorFgDim).Inherit(rowStyle).Width(w).Align(lipgloss.Right).Render(text)
	}

	marker := stateMarkerCell(s.State, m.SpinnerTick, selected)
	id := cell(colorFgMuted, false, s.ShortID, colID)
	slug := cell(colorFgPrimary, true, s.Slug, colSlug)
	descColor := colorFgDim
	if selected {
		descColor = colorFgPrimary
	}
	desc := cell(descColor, false, s.LastLine, descW)
	model := cell(colorFgDim, false, modelLabel(s), colModel)
	t := timeCell(durationAgo(s.LastEventAt), colTime)

	gap := rowStyle.Render(strings.Repeat(" ", colGap))
	indent := rowStyle.Render(strings.Repeat(" ", rowIndent))

	row := indent + marker + gap + id + gap + slug + gap + desc + gap + model + gap + t
	row = rowStyle.Width(width).Render(row)

	if until, ok := m.BlinkUntil[s.ID]; ok && time.Now().Before(until) {
		if time.Now().UnixMilli()/200%2 == 0 {
			row = lipgloss.NewStyle().Background(colorDone).Foreground(lipgloss.Color("#0F1115")).Width(width).Render(strings.TrimRight(row, " "))
		}
	}
	return row
}

func stateMarkerCell(st protocol.State, tick int, selected bool) string {
	style := lipgloss.NewStyle().Bold(true).Width(colMarker)
	if selected {
		style = style.Background(colorBgElev)
	}
	switch st {
	case protocol.StateWorking:
		return style.Foreground(colorWorking).Render(spinnerFrames[tick%len(spinnerFrames)])
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

func stateMarker(st protocol.State, tick int) string {
	return stateMarkerCell(st, tick, false)
}

func modelLabel(s protocol.SessionSummary) string {
	if s.Effort != "" {
		return s.ModelID + " · " + s.Effort
	}
	return s.ModelID
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
