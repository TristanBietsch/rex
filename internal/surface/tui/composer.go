package tui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

// Composer is the new-agent page's multi-line task editor (R16 D12: hand
// rolled, rune safe). Lines holds the text one slice per line; Row and Col
// are the cursor, a line index and a rune index into it. The zero value is
// one empty line. It only edits: which key launches, inserts a newline or
// moves focus stays with the page.
type Composer struct {
	Lines    [][]rune
	Row, Col int
}

// norm makes the zero value usable and keeps the cursor inside the text.
func (c *Composer) norm() {
	if len(c.Lines) == 0 {
		c.Lines = [][]rune{nil}
	}
	c.Row = min(max(c.Row, 0), len(c.Lines)-1)
	c.Col = min(max(c.Col, 0), len(c.Lines[c.Row]))
}

// composerText cleans typed or pasted text: \r\n and a lone \r become \n,
// escape sequences, the bidi controls (Trojan Source) and every control but
// \n and \t are dropped, and invalid UTF-8 becomes U+FFFD.
func composerText(s string) string {
	s = strings.ToValidUTF8(s, "�")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = ansi.Strip(s)
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case unicode.IsControl(r), r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069:
			return -1
		}
		return r
	}, s)
}

// Insert puts s at the cursor; each "\n" in it starts a new line.
func (c *Composer) Insert(s string) {
	c.norm()
	for i, part := range strings.Split(composerText(s), "\n") {
		if i > 0 {
			c.Newline()
		}
		if part == "" {
			continue
		}
		rs := []rune(part)
		line := c.Lines[c.Row]
		next := make([]rune, 0, len(line)+len(rs))
		next = append(next, line[:c.Col]...)
		next = append(next, rs...)
		next = append(next, line[c.Col:]...)
		c.Lines[c.Row] = next
		c.Col += len(rs)
	}
}

// Newline splits the line at the cursor; the cursor starts the new line.
func (c *Composer) Newline() {
	c.norm()
	line := c.Lines[c.Row]
	head := append([]rune(nil), line[:c.Col]...)
	tail := append([]rune(nil), line[c.Col:]...)
	lines := make([][]rune, 0, len(c.Lines)+1)
	lines = append(lines, c.Lines[:c.Row]...)
	lines = append(lines, head, tail)
	lines = append(lines, c.Lines[c.Row+1:]...)
	c.Lines = lines
	c.Row++
	c.Col = 0
}

// Backspace deletes the rune before the cursor; at the start of a line it
// joins the line to the one above.
func (c *Composer) Backspace() {
	c.norm()
	if c.Col > 0 {
		line := c.Lines[c.Row]
		c.Lines[c.Row] = append(line[:c.Col-1:c.Col-1], line[c.Col:]...)
		c.Col--
		return
	}
	if c.Row == 0 {
		return
	}
	prev := c.Lines[c.Row-1]
	col := len(prev)
	c.Lines[c.Row-1] = append(prev[:col:col], c.Lines[c.Row]...)
	c.Lines = append(c.Lines[:c.Row], c.Lines[c.Row+1:]...)
	c.Row--
	c.Col = col
}

// DeleteWord deletes the word before the cursor and the white space after
// it; at the start of a line it joins the line above, as Backspace does.
func (c *Composer) DeleteWord() {
	c.norm()
	if c.Col == 0 {
		c.Backspace()
		return
	}
	line := c.Lines[c.Row]
	i := c.Col
	for i > 0 && unicode.IsSpace(line[i-1]) {
		i--
	}
	for i > 0 && !unicode.IsSpace(line[i-1]) {
		i--
	}
	c.Lines[c.Row] = append(line[:i:i], line[c.Col:]...)
	c.Col = i
}

// ClearLine empties the cursor's line.
func (c *Composer) ClearLine() {
	c.norm()
	c.Lines[c.Row] = nil
	c.Col = 0
}

// Move moves the cursor dRow lines and dCol runes. Up and down keep the
// column, clamped to the line; left at a line start goes to the end of the
// line above and right at a line end to the start of the line below.
func (c *Composer) Move(dRow, dCol int) {
	c.norm()
	if dRow != 0 {
		c.Row += dRow
		c.norm()
	}
	for ; dCol < 0; dCol++ {
		switch {
		case c.Col > 0:
			c.Col--
		case c.Row > 0:
			c.Row--
			c.Col = len(c.Lines[c.Row])
		}
	}
	for ; dCol > 0; dCol-- {
		switch {
		case c.Col < len(c.Lines[c.Row]):
			c.Col++
		case c.Row < len(c.Lines)-1:
			c.Row++
			c.Col = 0
		}
	}
}

// Home moves the cursor to the start of its line.
func (c *Composer) Home() { c.norm(); c.Col = 0 }

// End moves the cursor to the end of its line.
func (c *Composer) End() { c.norm(); c.Col = len(c.Lines[c.Row]) }

// Clear empties the editor.
func (c *Composer) Clear() { *c = Composer{} }

// String is the text, lines joined by "\n".
func (c Composer) String() string {
	parts := make([]string, len(c.Lines))
	for i, l := range c.Lines {
		parts[i] = string(l)
	}
	return strings.Join(parts, "\n")
}

// Empty reports whether there is no text at all.
func (c Composer) Empty() bool {
	for _, l := range c.Lines {
		if len(l) > 0 {
			return false
		}
	}
	return true
}

// runeCells is how many cells r takes on screen; a tab draws as one space
// and anything unprintable as nothing.
func runeCells(r rune) int {
	if r == '\t' {
		return 1
	}
	return runewidth.RuneWidth(r)
}

// View is the text wrapped at width cells, exactly rows lines, windowed so
// the cursor's line shows. focused draws cursor (one cell) at the cursor;
// tabs draw as a space. Lines are plain text apart from cursor.
func (c Composer) View(width, rows int, focused bool, cursor string) []string {
	c.norm()
	width = max(width, 2)
	rows = max(rows, 1)
	var (
		out  []string
		curV int
	)
	for li, line := range c.Lines {
		var (
			b     strings.Builder
			cells int
		)
		flush := func() {
			out = append(out, b.String())
			b.Reset()
			cells = 0
		}
		for ci := 0; ci <= len(line); ci++ {
			if li == c.Row && ci == c.Col {
				if cells+1 > width {
					flush()
				}
				curV = len(out)
				if focused {
					b.WriteString(cursor)
					cells += ansi.StringWidth(cursor)
				}
			}
			if ci == len(line) {
				break
			}
			r := line[ci]
			rw := runeCells(r)
			if cells+rw > width {
				flush()
			}
			if r == '\t' {
				r = ' '
			}
			if rw > 0 && utf8.ValidRune(r) {
				b.WriteRune(r)
				cells += rw
			}
		}
		flush()
	}
	start := 0
	if len(out) > rows {
		start = min(max(curV-rows/2, 0), len(out)-rows)
		start = max(start, curV-rows+1)
		start = min(start, curV)
	}
	view := out[start:min(start+rows, len(out))]
	for len(view) < rows {
		view = append(view, "")
	}
	return view
}
