package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var styleArrow = lipgloss.NewStyle().Foreground(colorWorking)

func renderPrompt(m Model) string {
	switch m.Focus {
	case FocusCommand:
		return styleStateNeeds.Render(":") + " " + m.CmdText + cursorBlock(m)
	default:
		body := m.PromptText
		if body == "" && m.Focus != FocusPrompt {
			body = styleDim.Render("describe a task for a new session")
		} else if m.Focus == FocusPrompt {
			body = m.PromptText + cursorBlock(m)
		}
		return styleArrow.Render("λ") + " " + body
	}
}

func cursorBlock(m Model) string {
	if m.SpinnerTick%10 < 5 {
		return lipgloss.NewStyle().Background(colorFgPrimary).Foreground(colorBgBase).Render(" ")
	}
	return " "
}

func renderHelpLine(m Model) string {
	if m.Err != "" {
		return styleStateFailed.Render("err: " + m.Err)
	}
	parts := []string{
		styleHeaderApp.Render("i") + styleDim.Render(" focus"),
		styleHeaderApp.Render("enter") + styleDim.Render(" open"),
		styleHeaderApp.Render("n") + styleDim.Render(" new"),
		styleHeaderApp.Render("t") + styleDim.Render(" filter"),
		styleHeaderApp.Render(":") + styleDim.Render(" command"),
		styleHeaderApp.Render("dd") + styleDim.Render(" delete"),
		styleHeaderApp.Render("?") + styleDim.Render(" help"),
	}
	sep := styleMuted.Render(" · ")
	return strings.Join(parts, sep)
}
