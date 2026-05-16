package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"

	"github.com/tristanbietsch/rex/internal/protocol"
)

// renderHeader renders the two-line header (logo+counts+button row, chips row),
// padded to `width` columns.
func renderHeader(m Model, width int) string {
	var working, needsInput, done, failed, crashed int
	for _, s := range m.Sessions {
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
		}
	}

	logo := styleHeaderApp.Render("∴ REX")
	counts := renderHeaderCounts(m, needsInput, working, done, failed+crashed)
	left := logo + counts
	right := styleNewBtn.Render("+ new agent")

	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(right)
	gap := width - leftW - rightW - 4
	if gap < 1 {
		gap = 1
	}
	spacer := repeatRune(' ', gap)
	topRow := "  " + left + spacer + right

	chips := renderFilterChips(m)
	chipRow := padLine("  "+chips, width)

	out := padLine(topRow, width) + "\n" + chipRow

	if m.BackendUnavailable {
		reason := m.BackendUnavailableReason
		if reason == "" {
			reason = "ollama unreachable"
		}
		banner := styleDim.Render(
			"  summary backend: " + reason + " — install: https://ollama.com  ·  pull: ollama pull gemma2:2b",
		)
		out += "\n" + padLine(banner, width)
	}

	return out
}

// renderHeaderCounts formats the counts segment of the header per header_style.
// verbose: "  4 awaiting input · 2 working · 7 completed"
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
		s = fmt.Sprintf("    %d awaiting input · %d working · %d completed", needs, working, done)
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
	tools := []string{"all", "claude", "codex", "gemini", "ollama"}
	out := ""
	for i, t := range tools {
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

func repeatRune(r rune, n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]rune, n)
	for i := range b {
		b[i] = r
	}
	return string(b)
}
