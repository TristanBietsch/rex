package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/tristanbietsch/rex/internal/protocol"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func renderBoard(m Model) string {
	var b strings.Builder

	groups := []struct {
		title string
		state protocol.State
	}{
		{"Needs input", protocol.StateNeedsInput},
		{"Working", protocol.StateWorking},
		{"Completed", protocol.StateDone},
	}

	for _, g := range groups {
		rows := filterByState(m.Sessions, g.state, m.Filter)
		b.WriteString(styleSectionTitle.Render(g.title))
		b.WriteString("\n")
		if len(rows) == 0 {
			b.WriteString(styleMuted.Render("  (none)"))
			b.WriteString("\n\n")
			continue
		}
		for _, s := range rows {
			b.WriteString(renderRow(m, s))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	return b.String()
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

func renderRow(m Model, s protocol.SessionSummary) string {
	marker := stateMarker(s.State, m.SpinnerTick)
	id := styleMuted.Render(fmt.Sprintf("%-4s", s.ShortID))
	slug := styleSlug.Render(padOrTruncate(s.Slug, 22))
	desc := styleDim.Render(padOrTruncate(s.LastLine, 40))
	model := styleDim.Render(padOrTruncate(modelLabel(s), 18))
	ago := styleDim.Render(fmt.Sprintf("%5s", durationAgo(s.LastEventAt)))

	row := fmt.Sprintf("%s %s %s %s %s %s", marker, id, slug, desc, model, ago)
	if m.SelectedID == s.ID {
		row = styleSelected.Render(row)
	}
	return "  " + row
}

func stateMarker(st protocol.State, tick int) string {
	switch st {
	case protocol.StateWorking:
		return styleStateWorking.Render(spinnerFrames[tick%len(spinnerFrames)])
	case protocol.StateNeedsInput:
		return styleStateNeeds.Render("◆")
	case protocol.StateDone:
		return styleStateDone.Render("●")
	case protocol.StateFailed:
		return styleStateFailed.Render("✕")
	case protocol.StateCrashed:
		return styleStateCrashed.Render("○")
	}
	return " "
}

func modelLabel(s protocol.SessionSummary) string {
	if s.Effort != "" {
		return s.ModelID + " · " + s.Effort
	}
	return s.ModelID
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
