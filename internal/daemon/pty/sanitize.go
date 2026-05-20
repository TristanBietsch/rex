package pty

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
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
//  1. Strip CSI/OSC sequences via charmbracelet/x/ansi.Strip.
//  2. Replace remaining C0 control bytes (\r \t \v \b BEL …) with a single
//     space so they can't reposition the cursor or ring the terminal.
//  3. Collapse repeated whitespace and trim, since stripped output often
//     leaves runs of spaces where escapes used to be.
func sanitizeForDisplay(line string) string {
	if line == "" {
		return ""
	}
	stripped := ansi.Strip(line)

	var b strings.Builder
	b.Grow(len(stripped))
	prevSpace := false
	for _, r := range stripped {
		// Replace C0 controls and DEL with a space — never let them through.
		if r < 0x20 || r == 0x7f {
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
	return strings.TrimRight(b.String(), " ")
}
