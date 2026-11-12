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
	var l blockList
	for _, b := range v {
		switch b := b.(type) {
		case Section:
			l.section("# " + b.Title)
		case KV:
			if b.Txt != "" {
				l.add(b.Txt)
				continue
			}
			l.add(txtKV(b))
		case Table:
			l.add(txtTable(b))
		case Checks:
			l.add(p.txtChecks(b))
		case Note:
			l.add(b.Text)
		case Line:
			if b.Silent {
				continue
			}
			if b.Txt != "" {
				l.add(b.Txt)
			} else {
				l.add(b.Text)
			}
		case Para, Pre:
			l.add(blockText(b))
		case Defs:
			l.add(txtDefs(b))
		case Header, Hint:
		}
	}
	return l.String()
}

func txtDefs(d Defs) string {
	w := defNameWidth(d.Items)
	lines := make([]string, len(d.Items))
	for i, it := range d.Items {
		switch {
		case it.Desc == "":
			lines[i] = "  " + it.Name
		case displayWidth(it.Name) > defNameMax:
			lines[i] = "  " + it.Name + "  " + it.Desc
		default:
			lines[i] = "  " + padRight(it.Name, w) + "  " + it.Desc
		}
	}
	return strings.Join(lines, "\n")
}

func blockText(b Block) string {
	switch b := b.(type) {
	case Para:
		return b.Text
	case Pre:
		return b.Text
	}
	return ""
}

func pairLabel(p Pair) string {
	if p.Label != "" {
		return p.Label
	}
	return p.Key
}

func txtKV(kv KV) string {
	pairs := kv.pairs(FormatTXT)
	w := 0
	for _, p := range pairs {
		w = max(w, displayWidth(pairLabel(p)))
	}
	lines := make([]string, len(pairs))
	for i, p := range pairs {
		lines[i] = padRight(pairLabel(p), w) + "  " + p.Val.txt()
	}
	return strings.Join(lines, "\n")
}

func txtTable(t Table) string {
	idx := visibleCols(t.Cols, FormatTXT)
	if len(idx) == 0 {
		return "" // every column is for another format
	}
	if len(t.Rows) == 0 && t.Empty != "" {
		return t.Empty
	}
	if t.TxtRecords {
		lines := make([]string, len(t.Rows))
		for i, r := range t.Rows {
			parts := make([]string, len(idx))
			for n, c := range idx {
				parts[n] = r[c].txt()
			}
			lines[i] = strings.Join(parts, "\t")
		}
		if t.TxtFoot != "" {
			lines = append(lines, t.TxtFoot)
		}
		return strings.Join(lines, "\n")
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
