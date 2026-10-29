package tui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// renderPrompt builds the bottom prompt line, sized to width.
func renderPrompt(m Model, width int) string {
	glyph := promptGlyph(m)
	var line string
	// Input text keeps its tail (and the cursor after it) on screen:
	// "  " + glyph + " " + text + cursor.
	room := width - 4 - ansi.StringWidth(glyph)
	switch m.Focus {
	case FocusCommand:
		if m.Attach != nil {
			line = styleArrow.Render(glyph) + " " + styleDim.Render("attached — command bar open in popup")
			break
		}
		line = styleArrowYellow.Render(":") + " " + stylePrimary.Render(tailFit(m.CmdText, width-5)) + cursorBlock(m)
	case FocusPrompt:
		// PromptText keeps pasted line breaks; the bar is one row.
		body := stylePrimary.Render(tailFit(inputRow(m.PromptText), room)) + cursorBlock(m)
		if m.PromptText == "" {
			body = cursorBlock(m) + styleDim.Render("type then enter to spawn a session — esc to cancel")
		}
		line = styleArrow.Render(glyph) + " " + body
	case FocusAttach:
		line = styleArrow.Render(glyph) + " " + styleDim.Render("attached — ^] to detach")
	default:
		// PromptText is cleared whenever the prompt loses focus.
		line = styleArrow.Render(glyph) + " " + styleDim.Render("press i to describe a task for a new session")
	}
	return padLine("  "+line, width)
}

// tailFit keeps the end of input text s within n columns, cutting the front
// to "…", so the cursor after it stays on screen as the text grows.
func tailFit(s string, n int) string {
	n = max(n, 1)
	if w := ansi.StringWidth(s); w > n {
		return ansi.TruncateLeft(s, w-n+1, "…")
	}
	return s
}

// typedText is what a key event's runes add to a one-line input. A bracketed
// paste delivers newlines, tabs and escapes verbatim; protocol.SingleLine
// flattens them, and one space is kept where the paste began or ended with
// whitespace so pasting "foo " then typing "bar" still reads "foo bar".
func typedText(rs []rune) string {
	return inputRow(string(rs))
}

// inputRow flattens input text s to one row with protocol.SingleLine,
// keeping one space where s begins or ends with whitespace (a line break
// included), so the cursor drawn after it sits where the next character
// goes.
func inputRow(s string) string {
	out := protocol.SingleLine(s)
	if s == "" {
		return out
	}
	if first, _ := utf8.DecodeRuneInString(s); unicode.IsSpace(first) {
		out = " " + out
	}
	if last, _ := utf8.DecodeLastRuneInString(s); unicode.IsSpace(last) && out != " " {
		out += " "
	}
	return out
}

// typedPromptText is what a key event's runes add to the λ prompt or the
// wizard's task text. Those become the agent's initial prompt, so a pasted
// list or code block keeps its line breaks and tabs ("\r\n" and "\r" become
// "\n"); ESC and every other C0/C1 control, bidi overrides and invalid
// UTF-8 are dropped. Draw the result with inputRow, which flattens it.
func typedPromptText(rs []rune) string {
	var b strings.Builder
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case r == '\r':
			b.WriteByte('\n')
			if i+1 < len(rs) && rs[i+1] == '\n' {
				i++
			}
		case r == '\n' || r == '\t' || r == ' ':
			b.WriteRune(r)
		case r == utf8.RuneError || !utf8.ValidRune(r):
		case unicode.IsSpace(r):
			b.WriteByte(' ')
		case protocol.SingleLine(string(r)) == "":
			// A control or bidi override: what SingleLine drops.
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// promptGlyph returns the configured prompt glyph or the λ default.
func promptGlyph(m Model) string {
	if m.Store != nil {
		// Free text in config.yaml: keep it to one row.
		g, _ := m.Store.Get("prompt_glyph").(string)
		if g = protocol.SingleLine(g); g != "" {
			return g
		}
	}
	return "λ"
}

func cursorBlock(m Model) string {
	// reduce_motion: render a steady cursor.
	if m.Store != nil {
		if rm, _ := m.Store.Get("reduce_motion").(bool); rm {
			return lipgloss.NewStyle().Background(colorFgPrimary).Foreground(lipgloss.Color("#0F1115")).Render(" ")
		}
	}
	if m.SpinnerTick%10 < 5 {
		return lipgloss.NewStyle().Background(colorFgPrimary).Foreground(lipgloss.Color("#0F1115")).Render(" ")
	}
	return styleDim.Render(" ")
}

// renderHelpLine builds the bottom help line, sized to width.
func renderHelpLine(m Model, width int) string {
	if m.Err != "" {
		// Errors wrap daemon and OS messages that can span lines.
		return padLine("  "+styleStateFailed.Render("err: "+protocol.SingleLine(m.Err)), width)
	}
	if m.Focus == FocusRename {
		parts := []string{
			styleHeaderApp.Render("enter") + styleDim.Render(" save"),
			styleHeaderApp.Render("esc") + styleDim.Render(" cancel"),
			styleHeaderApp.Render("ctrl+u") + styleDim.Render(" clear"),
			styleHeaderApp.Render("ctrl+w") + styleDim.Render(" delete word"),
			styleDim.Render("empty = show slug"),
		}
		return padLine("  "+strings.Join(parts, styleMuted.Render(" · ")), width)
	}
	if m.Notice != "" {
		return padLine("  "+styleBootReady.Render(truncate(m.Notice, width-2)), width)
	}
	parts := []string{
		styleHeaderApp.Render("i") + styleDim.Render(" focus"),
		styleHeaderApp.Render("enter") + styleDim.Render(" open"),
		styleHeaderApp.Render("n") + styleDim.Render(" new"),
		styleHeaderApp.Render("r") + styleDim.Render(" rename"),
		styleHeaderApp.Render("cc") + styleDim.Render(" done/undo"),
		styleHeaderApp.Render("t") + styleDim.Render(" filter"),
		styleHeaderApp.Render(":") + styleDim.Render(" command"),
		styleHeaderApp.Render("dd") + styleDim.Render(" delete"),
		styleHeaderApp.Render("?") + styleDim.Render(" help"),
	}
	sep := styleMuted.Render(" · ")
	return padLine("  "+strings.Join(parts, sep), width)
}
