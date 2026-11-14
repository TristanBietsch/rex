package tui

import (
	"fmt"
	"log/slog"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/tristanbietsch/rex/internal/catalog/theme"
)

// runBorder is `:border` (and `::border` in the attach popup): the window
// frame for this run. It is never saved; the window_border settings are the
// permanent choice.
func runBorder(m Model, c cmdCall) (Model, tea.Cmd) {
	return borderCommand(m, c.Args), nil
}

// borderCommand applies :border's arguments. No argument toggles the frame;
// on, off, next, random, a color name or #rrggbb set it for this run. A bad
// argument sets m.Err and changes nothing. The frame itself follows at the
// next Update (syncLayout), which also resizes an attached agent.
func borderCommand(m Model, args []string) Model {
	if len(args) > 1 {
		m.Err = "border: " + borderUsage()
		return m
	}
	on := true
	arg := ""
	if len(args) == 1 {
		arg = strings.ToLower(args[0])
	}
	switch arg {
	case "":
		on = !borderEnabled(m)
	case "on":
	case "off":
		on = false
	case "next":
		m.borderRunColor = theme.NextEdge(borderEdge(m, borderPalette(m).Dark).ID, borderPalette(m).Dark).ID
	case "random":
		m.borderRunColor = "random"
	default:
		c, err := theme.ParseEdge(arg)
		if err != nil {
			m.Err = "border: " + borderUsage()
			return m
		}
		m.borderRunColor = c.ID
	}
	m.borderRunOn = &on
	m.Err = ""
	m.Notice = borderNotice(m)
	slog.Info("tui: border", "on", on, "color", m.borderRunColor)
	return m
}

func borderUsage() string {
	return "use :border [on|off|next|random|<color>|#rrggbb]; colors: " + strings.Join(theme.EdgeIDs(), ", ")
}

// borderNotice is the status line after :border.
func borderNotice(m Model) string {
	if !borderEnabled(m) {
		return "border: off"
	}
	p := borderPalette(m)
	c := borderEdge(m, p.Dark)
	s := "border: " + c.ID
	if borderColorChoice(m) == "random" {
		if _, ok := wsEdge(m.wsScope()); ok {
			s += " (workspace color · :border next)"
		} else {
			s += " (random · :border next)"
		}
	}
	switch {
	case m.TermW > 0 && frameInset(m) == 0:
		s += fmt.Sprintf(" · hidden: needs a %dx%d terminal", frameMinInnerW+2, frameMinInnerH+2)
	case lipgloss.ColorProfile() == termenv.Ascii:
		s += " · NO_COLOR is set: drawn without color"
	}
	return s
}
