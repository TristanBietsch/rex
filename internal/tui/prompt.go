package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	styleArrow       = lipgloss.NewStyle().Foreground(colorWorking)
	styleArrowYellow = lipgloss.NewStyle().Foreground(colorNeeds)
)

// renderPrompt builds the bottom λ prompt line, sized to width.
func renderPrompt(m Model, width int) string {
	var line string
	switch m.Focus {
	case FocusCommand:
		line = styleArrowYellow.Render(":") + " " + stylePrimary.Render(m.CmdText) + cursorBlock(m)
	case FocusPrompt:
		body := stylePrimary.Render(m.PromptText) + cursorBlock(m)
		if m.PromptText == "" {
			body = cursorBlock(m) + styleDim.Render("type then enter to spawn a session — esc to cancel")
		}
		line = styleArrow.Render("λ") + " " + body
	default:
		body := m.PromptText
		if body == "" {
			body = styleDim.Render("press i to describe a task for a new session")
		} else {
			body = stylePrimary.Render(body)
		}
		line = styleArrow.Render("λ") + " " + body
	}
	return padLine("  "+line, width)
}

func cursorBlock(m Model) string {
	if m.SpinnerTick%10 < 5 {
		return lipgloss.NewStyle().Background(colorFgPrimary).Foreground(lipgloss.Color("#0F1115")).Render(" ")
	}
	return styleDim.Render(" ")
}

// renderHelpLine builds the bottom help line, sized to width.
func renderHelpLine(m Model, width int) string {
	if m.Err != "" {
		return padLine("  "+styleStateFailed.Render("err: "+m.Err), width)
	}
	parts := []string{
		styleHeaderApp.Render("i") + styleDim.Render(" focus"),
		styleHeaderApp.Render("enter") + styleDim.Render(" open"),
		styleHeaderApp.Render("n") + styleDim.Render(" new"),
		styleHeaderApp.Render("t") + styleDim.Render(" filter"),
		styleHeaderApp.Render(":") + styleDim.Render(" command"),
		styleHeaderApp.Render("/") + styleDim.Render(" slash"),
		styleHeaderApp.Render("dd") + styleDim.Render(" delete"),
		styleHeaderApp.Render("?") + styleDim.Render(" help"),
	}
	sep := styleMuted.Render(" · ")
	return padLine("  "+strings.Join(parts, sep), width)
}
