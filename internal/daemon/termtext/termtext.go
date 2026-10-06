// Package termtext turns raw PTY bytes from full-screen agent TUIs into
// readable text: line structure recovered from cursor movement, escapes
// stripped, and UI chrome (spinners, borders, key hints) filtered out.
package termtext

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// lineBreakRe matches CSI sequences that move the cursor to another row or
// clear the screen. TUIs (claude, codex, gemini, ollama) repaint with these
// instead of '\n'; mapping them to newlines preserves the on-screen rows.
//
//	H, f — cursor absolute position
//	A, B — cursor up / down
//	E, F — cursor next / previous line
//	J    — erase display
//	d    — line position absolute
//	G    — cursor column absolute (used with \r to restart a row)
var lineBreakRe = regexp.MustCompile(`\x1b\[\d*(?:;\d+)?[HfABEFJdG]`)

// cursorForwardRe matches CUF (cursor forward), which TUIs use in place of
// runs of spaces. Mapping it to a space keeps words apart.
var cursorForwardRe = regexp.MustCompile(`\x1b\[\d*C`)

// Normalize returns b with cursor movement mapped to line breaks / spaces and
// all other escapes stripped. Line content is otherwise untouched so callers
// can still anchor regexes at `^`.
func Normalize(b []byte) string {
	s := lineBreakRe.ReplaceAllString(string(b), "\n")
	s = cursorForwardRe.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, "\r", "\n")
	return ansi.Strip(s)
}

// Lines returns the readable, non-chrome lines of b in order, with whitespace
// collapsed and consecutive duplicates (repaints) removed.
func Lines(b []byte) []string {
	raw := strings.Split(Normalize(b), "\n")
	out := make([]string, 0, len(raw))
	for _, l := range raw {
		l = collapse(l)
		if l == "" || IsChrome(l) {
			continue
		}
		if n := len(out); n > 0 && out[n-1] == l {
			continue
		}
		out = append(out, l)
	}
	return out
}

// Tail joins Lines(b) and keeps at most max bytes from the end, cut on a line
// boundary.
func Tail(b []byte, max int) string {
	s := strings.Join(Lines(b), "\n")
	if max <= 0 || len(s) <= max {
		return s
	}
	s = s[len(s)-max:]
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	return s
}

// LastLine returns the last readable line of b, or "" when none.
func LastLine(b []byte) string {
	lines := Lines(b)
	if len(lines) == 0 {
		return ""
	}
	return lines[len(lines)-1]
}

// chromeHints are status-bar / key-hint fragments agent TUIs redraw
// constantly. A line containing one is UI furniture, not agent output.
var chromeHints = []string{
	"esc to interrupt",
	"esc to cancel",
	"shift+tab",
	"auto mode",
	"accept edits",
	"bypass permissions",
	"? for shortcuts",
	"ctrl+c to",
	"ctrl+t to",
	"ctrl+r to",
	"thinking with",
	"tokens ·",
	"· ↓",
	"· ↑",
	"/? for help",
	"send a message",
	"type your message",
	"context left",
	"← for agents",
}

// IsChrome reports whether line is UI furniture rather than content: spinner
// frames, box borders, counters, key hints, or fragments with too few letters
// to carry meaning.
func IsChrome(line string) bool {
	lower := strings.ToLower(line)
	for _, h := range chromeHints {
		if strings.Contains(lower, h) {
			return true
		}
	}
	letters := 0
	for _, r := range line {
		if unicode.IsLetter(r) {
			letters++
		}
	}
	// Spinner glyphs, digits of a timer, a lone letter of a shimmer animation,
	// box-drawing rules: none have three letters.
	return letters < 3
}

// collapse trims and squeezes internal whitespace and drops control runes.
func collapse(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	space := false
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			space = b.Len() > 0
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		b.WriteRune(r)
	}
	return b.String()
}
