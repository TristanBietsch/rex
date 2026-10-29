package pty

import (
	"strings"
	"unicode"
	"unicode/utf8"

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

// sanitizePaste makes an initial prompt safe to paste into an agent's input
// box. It drops ESC, so an "\x1b[201~" in the prompt can't end the bracketed
// paste early and have the rest typed as keystrokes, and every other control
// that a terminal would act on: C0 except \n and \t, DEL, C1, and invalid
// UTF-8 (whose bytes could read as 8-bit C1).
func sanitizePaste(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for len(s) > 0 {
		r, size := utf8.DecodeRuneInString(s)
		s = s[size:]
		switch {
		case r == utf8.RuneError && size <= 1:
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
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
//  3. Finish with protocol.SingleLine: it drops C1 controls, bidi overrides
//     and invalid UTF-8, collapses repeated whitespace and trims, since
//     stripped output often leaves runs of spaces where escapes used to be.
func sanitizeForDisplay(line string) string {
	if line == "" {
		return ""
	}
	// Byte-wise is safe: ASCII bytes never occur inside a multi-byte UTF-8
	// sequence, and invalid bytes are left for SingleLine to drop.
	b := []byte(ansi.Strip(line))
	for i, c := range b {
		if c < 0x20 || c == 0x7f {
			b[i] = ' '
		}
	}
	return protocol.SingleLine(string(b))
}
