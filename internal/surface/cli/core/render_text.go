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
	var blocks []string
	for _, b := range v {
		switch b := b.(type) {
		case Header:
			s := p.Styles.Title.Render(b.Title)
			if b.Meta != "" {
				s += "  " + p.Styles.Dim.Render(b.Meta)
			}
			blocks = append(blocks, s)
		case Section:
			blocks = append(blocks, p.Styles.Bold.Render(b.Title+":"))
		case KV:
			blocks = append(blocks, p.textKV(b))
		case Table:
			blocks = append(blocks, p.textTable(b))
		case Checks:
			blocks = append(blocks, p.textChecks(b))
		case Note:
			style := p.Styles.Dim
			if b.Tone == ToneWarn || b.Tone == ToneOK {
				style = p.Styles.Tone(b.Tone)
			}
			blocks = append(blocks, style.Render(b.Text))
		case Hint:
			s := p.Styles.Accent.Render(p.Glyphs.Hint + " " + b.Cmd)
			if b.Text != "" {
				s += "  " + p.Styles.Dim.Render(b.Text)
			}
			blocks = append(blocks, s)
		case Line:
			s := b.Text
			if b.Glyph != "" {
				s = b.Glyph + " " + s
			}
			blocks = append(blocks, p.Styles.Tone(b.Tone).Render(s))
		}
	}
	return joinBlocks(blocks)
}

func (p *Printer) textKV(kv KV) string {
	if kv.Inline {
		items := make([]string, len(kv.Pairs))
		for i, pr := range kv.Pairs {
			s := pr.Val.Text + " " + pairLabel(pr)
			if pr.Glyph != "" {
				s = pr.Glyph + " " + s
			}
			items[i] = p.Styles.Tone(pr.Val.Tone).Render(s)
		}
		return strings.Join(items, "   ")
	}
	w := 0
	for _, pr := range kv.Pairs {
		w = max(w, displayWidth(pairLabel(pr)))
	}
	valW := max(p.Width-bodyIndent-w-gutter, 10)
	pad := strings.Repeat(" ", bodyIndent+w+gutter)
	var lines []string
	for _, pr := range kv.Pairs {
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
	idx := visibleCols(t.Cols, FormatText)
	indent := strings.Repeat(" ", bodyIndent)
	if len(t.Rows) == 0 && t.Empty != "" {
		return indent + p.Styles.Dim.Render(t.Empty)
	}
	widths := make([]int, len(idx))
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
	total := bodyIndent + gutter*max(len(idx)-1, 0)
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
