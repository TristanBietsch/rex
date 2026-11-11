package core

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

func displayWidth(s string) int { return ansi.StringWidth(s) }

// renderTXT is the plain, stable format for grep and awk: no ANSI, no
// Header or Hint blocks, no wrapping. Tables use fixed column widths (Col.Width)
// truncated by display width.
func (p *Printer) renderTXT(v View) string {
	var blocks []string
	for _, b := range v {
		switch b := b.(type) {
		case Section:
			blocks = append(blocks, "# "+b.Title)
		case KV:
			if b.Txt != "" {
				blocks = append(blocks, b.Txt)
				continue
			}
			blocks = append(blocks, txtKV(b))
		case Table:
			blocks = append(blocks, txtTable(b))
		case Checks:
			blocks = append(blocks, p.txtChecks(b))
		case Note:
			blocks = append(blocks, b.Text)
		case Line:
			if b.Txt != "" {
				blocks = append(blocks, b.Txt)
			} else {
				blocks = append(blocks, b.Text)
			}
		case Header, Hint:
		}
	}
	return joinBlocks(blocks)
}

func pairLabel(p Pair) string {
	if p.Label != "" {
		return p.Label
	}
	return p.Key
}

func txtKV(kv KV) string {
	w := 0
	for _, p := range kv.Pairs {
		w = max(w, displayWidth(pairLabel(p)))
	}
	lines := make([]string, len(kv.Pairs))
	for i, p := range kv.Pairs {
		lines[i] = padRight(pairLabel(p), w) + "  " + p.Val.txt()
	}
	return strings.Join(lines, "\n")
}

func txtTable(t Table) string {
	idx := visibleCols(t.Cols, FormatTXT)
	if len(t.Rows) == 0 && t.Empty != "" {
		return t.Empty
	}
	cell := func(r []Cell, i int) string {
		s := r[i].txt()
		if m := t.Cols[i].Max; m > 0 {
			s = Truncate(s, m)
		}
		return s
	}
	widths := make([]int, len(idx))
	for n, i := range idx {
		c := t.Cols[i]
		widths[n] = c.Width
		if widths[n] == 0 {
			w := displayWidth(strings.ToUpper(c.Title))
			for _, r := range t.Rows {
				w = max(w, displayWidth(cell(r, i)))
			}
			widths[n] = w
		}
	}
	line := func(get func(i int) string) string {
		parts := make([]string, len(idx))
		for n, i := range idx {
			s := get(i)
			switch {
			case n == len(idx)-1 && t.Cols[i].Align == AlignLeft:
			case t.Cols[i].Align == AlignRight:
				s = padLeft(s, widths[n])
			default:
				s = padRight(s, widths[n])
			}
			parts[n] = s
		}
		return strings.Join(parts, "  ")
	}
	lines := []string{line(func(i int) string { return strings.ToUpper(t.Cols[i].Title) })}
	for _, r := range t.Rows {
		lines = append(lines, line(func(i int) string { return cell(r, i) }))
	}
	return strings.Join(lines, "\n")
}

func (p *Printer) txtChecks(c Checks) string {
	var lines []string
	for _, r := range c.Rows {
		lines = append(lines, p.checkGlyph(r.Status)+" "+r.Name+"  "+r.Detail)
		if r.Hint != "" {
			lines = append(lines, "  "+p.Glyphs.Hint+" "+r.Hint)
		}
	}
	return strings.Join(lines, "\n")
}

func (p *Printer) checkGlyph(status string) string {
	switch status {
	case "pass":
		return p.Glyphs.OK
	case "warn":
		return p.Glyphs.Warn
	case "fail":
		return p.Glyphs.Fail
	default:
		return p.Glyphs.Info
	}
}
