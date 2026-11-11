package core

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	bodyIndent = 2
	gutter     = 2
	// defaultFlexMin is the narrowest a Flex column shrinks to.
	defaultFlexMin = 6
)

// renderText is the human layout: no blank line at the top, one blank line
// between blocks, headers and sections at column 0, bodies indented two
// spaces, wrapped and shrunk to the terminal width.
func (p *Printer) renderText(v View) string {
	var l blockList
	for _, b := range v {
		switch b := b.(type) {
		case Header:
			s := p.Styles.Title.Render(b.Title)
			if b.Meta != "" {
				s += "  " + p.Styles.Dim.Render(b.Meta)
			}
			l.add(s)
		case Section:
			l.section(p.Styles.Bold.Render(b.Title + ":"))
		case KV:
			l.add(p.textKV(b))
		case Table:
			l.add(p.textTable(b))
		case Checks:
			l.add(p.textChecks(b))
		case Note:
			style := p.Styles.Dim
			if b.Tone == ToneWarn || b.Tone == ToneOK {
				style = p.Styles.Tone(b.Tone)
			}
			l.add(style.Render(b.Text))
		case Hint:
			s := p.Styles.Accent.Render(p.Glyphs.Hint + " " + b.Cmd)
			if b.Text != "" {
				s += "  " + p.Styles.Dim.Render(b.Text)
			}
			l.add(s)
		case Line:
			s := b.Text
			if b.Glyph != "" {
				s = b.Glyph + " " + s
			}
			l.add(p.Styles.Tone(b.Tone).Render(s))
		case Para:
			l.add(ansi.Wrap(b.Text, p.Width, ""))
		case Defs:
			l.add(p.textDefs(b))
		}
	}
	return l.String()
}

// defNameMax is the widest name column of a Defs list; a longer name gets
// its description on the next line.
const defNameMax = 30

func defNameWidth(items []Def) int {
	w := 0
	for _, it := range items {
		if n := displayWidth(it.Name); n <= defNameMax {
			w = max(w, n)
		}
	}
	return w
}

func (p *Printer) textDefs(d Defs) string {
	w := defNameWidth(d.Items)
	descCol := bodyIndent + w + gutter
	// On a narrow terminal each description goes under its name, indented.
	stack := p.Width-descCol < 20
	if stack {
		descCol = bodyIndent * 2
	}
	descW := max(p.Width-descCol, 10)
	pad := strings.Repeat(" ", descCol)
	var lines []string
	for _, it := range d.Items {
		name := p.Styles.Accent.Render(it.Name)
		if it.Desc == "" {
			lines = append(lines, strings.Repeat(" ", bodyIndent)+name)
			continue
		}
		parts := strings.Split(ansi.Wrap(it.Desc, descW, ""), "\n")
		head := strings.Repeat(" ", bodyIndent) + name
		if stack || displayWidth(it.Name) > defNameMax {
			lines = append(lines, head)
		} else {
			head += strings.Repeat(" ", w-displayWidth(it.Name)+gutter) + p.Styles.Dim.Render(parts[0])
			lines = append(lines, head)
			parts = parts[1:]
		}
		for _, part := range parts {
			lines = append(lines, pad+p.Styles.Dim.Render(part))
		}
	}
	return strings.Join(lines, "\n")
}

func (p *Printer) textKV(kv KV) string {
	pairs := kv.pairs(FormatText)
	if kv.Inline {
		items := make([]string, len(pairs))
		for i, pr := range pairs {
			s := pr.Val.Text + " " + pairLabel(pr)
			if pr.Glyph != "" {
				s = pr.Glyph + " " + s
			}
			items[i] = p.Styles.Tone(pr.Val.Tone).Render(s)
		}
		return strings.Join(items, "   ")
	}
	w := 0
	for _, pr := range pairs {
		w = max(w, displayWidth(pairLabel(pr)))
	}
	valW := max(p.Width-bodyIndent-w-gutter, 10)
	pad := strings.Repeat(" ", bodyIndent+w+gutter)
	var lines []string
	for _, pr := range pairs {
		label := padRight(pairLabel(pr), w)
		wrapped := strings.Split(ansi.Wrap(pr.Val.Text, valW, ""), "\n")
		for i, part := range wrapped {
			part = p.Styles.Tone(pr.Val.Tone).Render(part)
			if i == 0 {
				lines = append(lines, strings.Repeat(" ", bodyIndent)+p.Styles.Dim.Render(label)+strings.Repeat(" ", gutter)+part)
			} else {
				lines = append(lines, pad+part)
			}
		}
	}
	return strings.Join(lines, "\n")
}

func (p *Printer) textTable(t Table) string {
	var idx []int
	for _, i := range visibleCols(t.Cols, FormatText) {
		if d := t.Cols[i].DropBelow; d == 0 || p.Width >= d {
			idx = append(idx, i)
		}
	}
	if len(idx) == 0 {
		return "" // every column is for another format
	}
	indent := strings.Repeat(" ", bodyIndent)
	if len(t.Rows) == 0 && t.Empty != "" {
		return indent + p.Styles.Dim.Render(t.Empty)
	}
	// layout sizes the columns in idx, shrinking Flex ones to fit.
	layout := func(idx []int) (widths []int, total int) {
		widths = make([]int, len(idx))
		for n, i := range idx {
			c := t.Cols[i]
			w := displayWidth(c.Title)
			for _, r := range t.Rows {
				w = max(w, displayWidth(r[i].Text))
			}
			if c.Max > 0 {
				w = min(w, c.Max)
			}
			widths[n] = max(w, c.Min)
		}
		total = bodyIndent + gutter*max(len(idx)-1, 0)
		for _, w := range widths {
			total += w
		}
		for total > p.Width {
			best := -1
			for n, i := range idx {
				c := t.Cols[i]
				if c.Flex && widths[n] > max(c.Min, defaultFlexMin) && (best < 0 || widths[n] > widths[best]) {
					best = n
				}
			}
			if best < 0 {
				break
			}
			widths[best]--
			total--
		}
		return widths, total
	}
	// A table that still does not fit moves its Below columns, last first,
	// onto a second dim line under each row.
	var below []int
	widths, total := layout(idx)
	for total > p.Width {
		pos := -1
		for n, i := range idx {
			if t.Cols[i].Below {
				pos = n
			}
		}
		if pos < 0 {
			break
		}
		below = append([]int{idx[pos]}, below...)
		idx = append(idx[:pos:pos], idx[pos+1:]...)
		widths, total = layout(idx)
	}
	line := func(text func(i int) string, style func(i int) lipgloss.Style) string {
		var b strings.Builder
		b.WriteString(indent)
		for n, i := range idx {
			s := Truncate(text(i), widths[n])
			gap := strings.Repeat(" ", max(widths[n]-displayWidth(s), 0))
			if s != "" {
				s = style(i).Render(s)
			}
			last := n == len(idx)-1
			switch {
			case t.Cols[i].Align == AlignRight:
				b.WriteString(gap + s)
			case last:
				b.WriteString(s)
			default:
				b.WriteString(s + gap)
			}
			if !last {
				b.WriteString(strings.Repeat(" ", gutter))
			}
		}
		return strings.TrimRight(b.String(), " ")
	}
	lines := []string{line(
		func(i int) string { return strings.ToUpper(t.Cols[i].Title) },
		func(int) lipgloss.Style { return p.Styles.Dim })}
	for _, r := range t.Rows {
		lines = append(lines, line(
			func(i int) string { return r[i].Text },
			func(i int) lipgloss.Style { return p.Styles.Tone(r[i].Tone) }))
		var extra []string
		for _, i := range below {
			if r[i].Text != "" {
				extra = append(extra, r[i].Text)
			}
		}
		if len(extra) > 0 {
			hang := indent + strings.Repeat(" ", widths[0]+gutter)
			text := Truncate(strings.Join(extra, "  "), max(p.Width-displayWidth(hang), 1))
			lines = append(lines, hang+p.Styles.Dim.Render(text))
		}
	}
	return strings.Join(lines, "\n")
}

func (p *Printer) textChecks(c Checks) string {
	nameW := 0
	for _, r := range c.Rows {
		nameW = max(nameW, displayWidth(r.Name))
	}
	detailCol := bodyIndent + 2 + nameW + gutter
	detailW := max(p.Width-detailCol, 10)
	pad := strings.Repeat(" ", detailCol)
	var lines []string
	for _, r := range c.Rows {
		g, style := p.checkGlyph(r.Status), p.Styles.Muted
		switch r.Status {
		case "pass":
			style = p.Styles.OK
		case "warn":
			style = p.Styles.Warn
		case "fail":
			style = p.Styles.Err
		}
		head := strings.Repeat(" ", bodyIndent) + style.Render(g) + " " + padRight(r.Name, nameW) + strings.Repeat(" ", gutter)
		for i, part := range strings.Split(ansi.Wrap(r.Detail, detailW, ""), "\n") {
			if i == 0 {
				lines = append(lines, head+part)
			} else {
				lines = append(lines, pad+part)
			}
		}
		if r.Hint != "" {
			lines = append(lines, pad+p.Styles.Accent.Render(p.Glyphs.Hint+" "+r.Hint))
		}
	}
	return strings.Join(lines, "\n")
}
