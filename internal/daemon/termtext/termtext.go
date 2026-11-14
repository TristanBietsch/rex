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
// can still anchor regexes at `^`. The result is always valid UTF-8.
//
// Invalid UTF-8 is dropped first: ansi.Strip's parser passes an escape
// sequence that follows an invalid lead byte ("\xc9\x1b[31m"), and a split
// rune at a read boundary is noise either way.
func Normalize(b []byte) string {
	s := strings.ToValidUTF8(string(b), "")
	s = lineBreakRe.ReplaceAllString(s, "\n")
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

// inputMarkers start the row where full-screen agents draw their input box
// (claude ❯, codex ›) or a selection cursor in a dialog.
var inputMarkers = []string{"❯", "›"}

// bottomInputRe matches agy's input row: a bare ">" prompt, optionally
// followed by the user's draft, optionally inside a box border.
var bottomInputRe = regexp.MustCompile(`^[│┃|]?\s*>(?: |$)`)

// maxRowsBelowInput is how many non-blank rows may sit under agy's input row
// (its footer) for the row still to count as the input box.
const maxRowsBelowInput = 3

// ScreenLines returns the readable content of a rendered screen (rows joined
// by '\n', e.g. vt10x String()). Everything from the agent's input row down —
// the prompt box, mode toggles, model/cwd status bars — is UI, not output, so
// it is cut before chrome filtering.
func ScreenLines(screen string) []string {
	rows := strings.Split(screen, "\n")
	for i := len(rows) - 1; i >= 0; i-- {
		row := strings.TrimSpace(rows[i])
		for _, mk := range inputMarkers {
			if strings.HasPrefix(row, mk) {
				rows = rows[:i]
				return Lines([]byte(strings.Join(rows, "\n")))
			}
		}
	}
	if i := bottomInputRow(rows); i >= 0 {
		rows = rows[:i]
	}
	return Lines([]byte(strings.Join(rows, "\n")))
}

// bottomInputRow returns the index of agy's input row, or -1. Only the
// bottom-most ">" row qualifies, and only when what is below it is footer
// (blank or chrome rows, or at most maxRowsBelowInput rows): a ">" row
// higher up is a markdown quote or history and stays.
func bottomInputRow(rows []string) int {
	for i := len(rows) - 1; i >= 0; i-- {
		if !bottomInputRe.MatchString(strings.TrimSpace(rows[i])) {
			continue
		}
		below, allChrome := 0, true
		for _, r := range rows[i+1:] {
			r = collapse(r)
			if r == "" {
				continue
			}
			below++
			if !IsChrome(r) {
				allChrome = false
			}
		}
		if allChrome || below <= maxRowsBelowInput {
			return i
		}
		return -1
	}
	return -1
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

// turnDoneRe matches Claude's turn timers: the spinner verb while working
// ("✳ Unfurling… (5s · thought for 4s)") and the end-of-turn line
// ("✻ Cooked for 2s · done").
var turnDoneRe = regexp.MustCompile(`^\S+ \p{L}+(?:…| for \d+(?:\.\d+)?[smh]\b)`)

// agySpinnerRe matches agy's ASCII-ellipsis spinner rows: a spinner glyph
// then one to three words ("⣷  Generating...", "◐ Reading files... (3s)"), or
// a lone "Thinking..." word. Only braille frames (U+2800-U+28FF) and the
// quarter-circle glyphs count as spinner glyphs: Claude's "⏺" bullet and "-"
// or "*" list bullets start agent prose ("⏺ Let me check...") that must stay.
var agySpinnerRe = regexp.MustCompile(`^(?:[\x{2800}-\x{28FF}◐◓◑◒◴◵◶◷]\s+\p{L}+(?:\s+\p{L}+){0,2}|\p{L}+)\.\.\.(?:\s+\(.*\))?$`)

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
	if turnDoneRe.MatchString(line) || agySpinnerRe.MatchString(line) || strings.HasPrefix(line, "⚠") {
		return true
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
