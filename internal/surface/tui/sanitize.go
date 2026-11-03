package tui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// sanitizeText makes an untrusted string safe to print inside one line of
// rex chrome. Session strings (title, name, slug, description, fleet,
// last_line) and daemon error text come from agents, transcripts and other
// clients; printed raw, a "\n" splits a board row and an escape sequence
// (OSC 52 clipboard write, CSI screen clear) runs in the user's terminal
// (backlog S-03). The daemon is meant to sanitize on ingest; this is the
// render-side backstop, so the TUI stays safe against an older or
// misbehaving daemon.
//
// Invalid UTF-8 is dropped, ANSI sequences are removed whole, line breaks
// and tabs become spaces, and every other C0/C1 control rune and DEL is
// dropped. The result is valid UTF-8 with no control runes, and
// sanitizeText is idempotent.
//
// It applies only to text that carries no rex styling: lipgloss output would
// lose its colors. internal/features/summarizer has a sibling
// (firstSafeLine) and the wire protocol package is getting a shared one;
// they should converge there.
func sanitizeText(s string) string {
	if isPlainText(s) {
		return s // fast path: the board renders every cell every frame
	}
	s = ansi.Strip(strings.ToValidUTF8(s, ""))
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			return ' '
		case unicode.IsControl(r):
			return -1
		default:
			return r
		}
	}, s)
}

// isPlainText reports whether s needs no sanitizing: no control runes (so
// no ESC and no line breaks) and no invalid UTF-8. A literal U+FFFD also
// takes the slow path, which leaves it unchanged.
func isPlainText(s string) bool {
	for _, r := range s {
		if r == utf8.RuneError || unicode.IsControl(r) {
			return false
		}
	}
	return true
}
