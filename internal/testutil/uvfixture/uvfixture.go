// Package uvfixture generates terminal byte streams shaped like what
// ultraviolet (the renderer inside Antigravity) writes, and the screen they
// must produce. It is hand-written from ultraviolet's behavior, not captured:
// a centered five-line block logo drawn with truecolor SGR per cell, cursor
// moves by hard tabs and CUF, ECH for long blank runs and REP for runs of
// identical styled cells. rex's emulators (the daemon mirror and the attach
// popup) must lay it out the way a real terminal does.
//
// Test code only; nothing outside _test.go files imports it.
package uvfixture

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Tag is drawn on the last row, started with hard tabs from column 0, so it
// only lands where a real terminal puts it if the tab stops reach that far.
const Tag = "v0.1.0-uv"

const tabWidth = 8

// logo is the five-line block art, all lines the same width. Row 2 is drawn
// with a background-colored interior (REP of a styled space); the interiors of
// row 4 is a default-blank interior (ECH then CUF).
var logo = [5]string{
	"  ▄███████████████▄  ",
	" ████▀▀▀▀▀▀▀▀▀▀▀████ ",
	"████▀           ▀████",
	" ▀████▄▄▄▄▄▄▄▄▄▄████▀",
	"▀▀███▀         ▀███▀▀",
}

// styledRow is the logo row whose interior blanks carry a background color.
const styledRow = 2

// Logo returns the logo lines.
func Logo() []string { return logo[:] }

// LogoWidth is the width in cells of every logo line.
func LogoWidth() int { return utf8.RuneCountInString(logo[0]) }

// Layout is where Frame puts things on a cols x rows screen.
type Layout struct {
	Left     int // column of the logo's first cell
	Top      int // row of the logo's first line
	RuleRow  int // row of the '=' rule under the logo
	TagRow   int // row of Tag
	TagCol   int // column of Tag's first cell
	LogoCols int
}

// LayoutFor returns the layout for a screen. Margins on both sides of the
// logo differ by at most one cell.
func LayoutFor(cols, rows int) Layout {
	lw := LogoWidth()
	top := (rows - len(logo) - 2) / 2
	return Layout{
		Left:     (cols - lw) / 2,
		Top:      top,
		RuleRow:  top + len(logo) + 1,
		TagRow:   rows - 1,
		TagCol:   (cols - len(Tag) - 2) / tabWidth * tabWidth,
		LogoCols: lw,
	}
}

// Expected is the screen Frame must produce: rows lines of exactly cols
// runes, blanks as spaces.
func Expected(cols, rows int) []string {
	l := LayoutFor(cols, rows)
	grid := make([][]rune, rows)
	for y := range grid {
		grid[y] = []rune(strings.Repeat(" ", cols))
	}
	for i, line := range logo {
		copy(grid[l.Top+i][l.Left:], []rune(line))
	}
	copy(grid[l.RuleRow][l.Left:], []rune(strings.Repeat("=", l.LogoCols)))
	copy(grid[l.TagRow][l.TagCol:], []rune(Tag))
	out := make([]string, rows)
	for y := range grid {
		out[y] = string(grid[y])
	}
	return out
}

// Frame is a full repaint for a cols x rows screen: hide the cursor, clear,
// then each row positioned with CUP and filled the way ultraviolet fills it.
// It needs cols >= LogoWidth()+len(Tag)+2 and rows >= 9.
func Frame(cols, rows int) []byte {
	if cols < LogoWidth()+len(Tag)+2 || rows < len(logo)+4 {
		panic(fmt.Sprintf("uvfixture: %dx%d is too small", cols, rows))
	}
	l := LayoutFor(cols, rows)
	var b bytes.Buffer
	b.WriteString("\x1b[?25l\x1b[H\x1b[2J")

	for i, line := range logo {
		fmt.Fprintf(&b, "\x1b[%d;1H", l.Top+i+1)
		drawLogoLine(&b, line, i == styledRow, l.Left)
		b.WriteString("\x1b[0m")
	}

	// The rule: one '=' then REP for the other 20.
	fmt.Fprintf(&b, "\x1b[%d;1H", l.RuleRow+1)
	moveTo(&b, 0, l.Left)
	fmt.Fprintf(&b, "\x1b[38;2;120;120;140m=\x1b[%db\x1b[0m", l.LogoCols-1)

	// The tag: from column 0, one HT per stop, then the text.
	fmt.Fprintf(&b, "\x1b[%d;1H", l.TagRow+1)
	b.WriteString(strings.Repeat("\t", l.TagCol/tabWidth))
	b.WriteString("\x1b[38;2;180;180;200m" + Tag + "\x1b[0m")
	return b.Bytes()
}

// drawLogoLine draws one logo line at screen column left, the cursor starting
// at column 0 (just after a CUP to column 1). Blank
// runs are skipped by moving; cells with a glyph get their own truecolor
// gradient SGR; a styled-blank interior is drawn with a space plus REP.
func drawLogoLine(b *bytes.Buffer, line string, styled bool, left int) {
	cursor := 0
	cells := []rune(line)
	first, last := -1, -1
	for i, r := range cells {
		if r != ' ' {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	for i := 0; i < len(cells); {
		r := cells[i]
		if r == ' ' {
			j := i
			for j < len(cells) && cells[j] == ' ' {
				j++
			}
			n := j - i
			interior := styled && i > first && j <= last
			switch {
			case i > last:
				// Trailing blanks: nothing to draw.
			case interior:
				// A run of background-colored spaces: one space, then REP.
				b.WriteString("\x1b[48;2;40;40;70m ")
				if n > 1 {
					fmt.Fprintf(b, "\x1b[%db", n-1)
				}
				b.WriteString("\x1b[49m")
				cursor = left + j
			default:
				if n >= tabWidth {
					fmt.Fprintf(b, "\x1b[%dX", n) // ECH: erase, then move over it
				}
				cursor = moveTo(b, cursor, left+j)
			}
			i = j
			continue
		}
		if cursor != left+i {
			moveTo(b, cursor, left+i)
		}
		red, green, blue := 90+i*7, 140+(i*5)%90, 255-i*6
		fmt.Fprintf(b, "\x1b[38;2;%d;%d;%dm%c", red, green, blue, r)
		cursor = left + i + 1
		i++
	}
}

// moveTo moves the cursor from column from to column to (to >= from): hard
// tabs while the next stop is at or before to, then CUF for the rest. It
// returns to.
func moveTo(b *bytes.Buffer, from, to int) int {
	for next := (from/tabWidth + 1) * tabWidth; next <= to; next += tabWidth {
		b.WriteByte('\t')
		from = next
	}
	if to > from {
		fmt.Fprintf(b, "\x1b[%dC", to-from)
	}
	return to
}
