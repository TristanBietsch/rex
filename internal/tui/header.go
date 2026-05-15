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
	counts := styleHeaderMeta.Render(fmt.Sprintf("    %d awaiting input · %d working · %d completed",
		needsInput, working, done))
	if failed+crashed > 0 {
		counts += styleStateFailed.Render(fmt.Sprintf("  · %d failed", failed+crashed))
	}
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

	return padLine(topRow, width) + "\n" + chipRow
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
