package pty

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// hasVisibleText reports whether b contains any printable (non-whitespace,
// non-control) character after stripping ANSI escape sequences.
//
// Codex / Claude / Gemini blink the cursor while idle by emitting escape-only
// chunks like `\x1b[?25l` and `\x1b[?25h` every ~500ms. If we treat those as
// "agent did something," the heuristic adapter's idle timer never crosses the
// 1200ms threshold and the session is stuck in "working" forever even though
// it's parked at a prompt.
func hasVisibleText(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	stripped := ansi.Strip(string(b))
	for _, r := range stripped {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			continue
		}
		if unicode.IsControl(r) {
			continue
		}
		return true
	}
	return false
}

// sanitizeForDisplay strips ANSI escape sequences and bare control bytes from a
// single line of raw PTY output. This is what gets surfaced as `last_line` in
// the TUI's board column — if we let cursor-positioning escapes (ESC[H,
// ESC[2J, ESC[K, …) through, the terminal will execute them when lipgloss
// renders the row, which corrupts the screen and causes the jitter the user
// sees on every new agent.
//
// We:
//  1. Drop invalid UTF-8, which would otherwise let ansi.Strip pass the
//     escape that follows an invalid lead byte.
//  2. Strip CSI/OSC sequences via charmbracelet/x/ansi.Strip.
//  3. Replace remaining control runes with a single space so they can't
//     reposition the cursor or ring the terminal: C0 (\r \t \v \b BEL …),
//     DEL, and C1 (U+0080–U+009F), whose U+009B and U+009D are the 8-bit
//     CSI and OSC introducers.
//  4. Collapse repeated whitespace and trim, since stripped output often
//     leaves runs of spaces where escapes used to be.
//
// The result is valid UTF-8 with no control runes, no leading or trailing
// space and no double spaces.
func sanitizeForDisplay(line string) string {
	if line == "" {
		return ""
	}
	stripped := ansi.Strip(strings.ToValidUTF8(line, ""))

	var b strings.Builder
	b.Grow(len(stripped))
	prevSpace := false
	for _, r := range stripped {
		// Replace C0, DEL and C1 controls with a space — never let them through.
		if unicode.IsControl(r) {
			if !prevSpace && b.Len() > 0 {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		if r == ' ' {
			if !prevSpace && b.Len() > 0 {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	// Controls are spaces by now; this drops bidi overrides (they reorder
	// what clients draw after them) and turns U+2028/U+2029 into spaces.
	return strings.TrimRight(protocol.FlattenDisplayText(b.String()), " ")
}

// TypedText makes text safe to type into an agent's PTY (the initial
// prompt, a Reply): escape sequences, C0 controls other than newline and
// tab, DEL, C1 controls and invalid UTF-8 are dropped, and "\r\n" and "\r"
// become "\n". Without this, an ESC[201~ in a prompt closed the bracketed
// paste early and everything after it reached the agent as keystrokes (a
// "\r" there submits, other controls act as key bindings).
func TypedText(s string) string {
	s = strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(strings.ToValidUTF8(s, ""))
	return strings.Map(func(r rune) rune {
		if r != '\n' && r != '\t' && unicode.IsControl(r) {
			return -1
		}
		return r
	}, ansi.Strip(s))
}
