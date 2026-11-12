package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// renderPrompt builds the bottom prompt line, sized to width.
func renderPrompt(m Model, width int) string {
	glyph := promptGlyph(m)
	var line string
	switch m.Focus {
	case FocusCommand:
		if m.Attach != nil {
			line = styleArrow.Render(glyph) + " " + styleDim.Render("attached — command bar open in popup")
			break
		}
		line = styleArrowYellow.Render(":") + " " + stylePrimary.Render(m.CmdText) + cursorBlock(m)
	case FocusSearch:
		return renderSearchPrompt(m, width)
	case FocusPrompt:
		body := stylePrimary.Render(m.PromptText) + cursorBlock(m)
		if m.PromptText == "" {
			body = cursorBlock(m) + styleDim.Render("type then enter to spawn a session — esc to cancel")
		}
		line = styleArrow.Render(glyph) + " " + body
	case FocusAttach:
		line = styleArrow.Render(glyph) + " " + styleDim.Render("attached — ^] to detach")
	default:
		body := m.PromptText
		if body == "" {
			body = styleDim.Render("press i to describe a task for a new session")
		} else {
			body = stylePrimary.Render(body)
		}
		line = styleArrow.Render(glyph) + " " + body
	}
	return padLine("  "+line, width)
}

// promptGlyph returns the configured prompt glyph or the λ default.
func promptGlyph(m Model) string {
	if m.Store != nil {
		if g, _ := m.Store.Get("prompt_glyph").(string); g != "" {
			return g
		}
	}
	return "λ"
}

// cursorLit draws the lit cursor. termenv drops every attribute under the
// Ascii profile (NO_COLOR, dumb terminals), which would leave a plain space,
// so there it is a solid block glyph instead.
func cursorLit() string {
	if lipgloss.ColorProfile() == termenv.Ascii {
		return "█"
	}
	return styleCursor.Render(" ")
}

func cursorBlock(m Model) string {
	// reduce_motion: render a steady cursor.
	if m.Store != nil {
		if rm, _ := m.Store.Get("reduce_motion").(bool); rm {
			return cursorLit()
		}
	}
	if m.SpinnerTick%10 < 5 {
		return cursorLit()
	}
	return styleDim.Render(" ")
}

// renderHelpLine builds the bottom help line, sized to width.
func renderHelpLine(m Model, width int) string {
	if m.reconnect != nil {
		// Outranks m.Err: intents fail while disconnected, and their errors
		// would hide why.
		return padLine("  "+styleStateFailed.Render(reconnectStatus), width)
	}
	if m.Err != "" {
		// Errors echo daemon messages and session strings: sanitize.
		return padLine("  "+styleStateFailed.Render("err: "+protocol.FlattenDisplayText(m.Err)), width)
	}
	if m.Focus == FocusRename {
		parts := []string{
			styleKey.Render("enter") + styleDim.Render(" save"),
			styleKey.Render("esc") + styleDim.Render(" cancel"),
			styleKey.Render("ctrl+u") + styleDim.Render(" clear"),
			styleKey.Render("ctrl+w") + styleDim.Render(" delete word"),
			styleDim.Render("empty = show slug"),
		}
		return padLine("  "+strings.Join(parts, styleMuted.Render(" · ")), width)
	}
	if m.Focus == FocusSearch {
		return searchHelpLine(width)
	}
	if m.PendingChord == "z" {
		return padLine("  "+styleKey.Render("z")+styleDim.Render(": h completed · 1-9 section · 0 show all"), width)
	}
	if m.Notice != "" {
		return padLine("  "+styleBootReady.Render(truncate(m.Notice, width-2)), width)
	}
	// Five keys (three on mobile); the rest live under ?.
	keys := helpLineKeys
	if mobileLayout(m) {
		keys = helpLineKeysMobile
	}
	var parts []string
	for _, k := range keys {
		parts = append(parts, styleKey.Render(k)+styleDim.Render(" "+helpLineLabel(k)))
	}
	sep := styleMuted.Render(" · ")
	return padLine("  "+strings.Join(parts, sep), width)
}

// helpLineLabel is the short label of the binding whose Keys is k. A key in
// helpLineKeys without a Line label is a table bug, caught by
// TestHelpLineKeysHaveLabels.
func helpLineLabel(k string) string {
	for _, b := range bindings {
		if b.Keys == k {
			return b.Line
		}
	}
	return ""
}
