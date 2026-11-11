package core

import (
	"strings"
)

var mdEscaper = strings.NewReplacer(
	`\`, `\\`, "|", `\|`, "`", "\\`", "<", "&lt;", ">", "&gt;",
	"[", `\[`, "]", `\]`, "!", `\!`, "*", `\*`, "_", `\_`, "~", `\~`,
)

// escapeMD makes s safe inside a Markdown table cell or paragraph: pipes,
// backticks, backslashes and angle brackets (so <script> stays text) are
// escaped, as are [ ] ! * _ ~ (no links, images, emphasis or strikethrough
// when pasted into an issue), and a leading # cannot become a heading.
func escapeMD(s string) string {
	s = mdEscaper.Replace(s)
	if strings.HasPrefix(s, "#") {
		s = `\` + s
	}
	return s
}

func mdRow(cells ...string) string { return "| " + strings.Join(cells, " | ") + " |" }

func mdTable(head []string, align []Align, rows [][]string) string {
	sep := make([]string, len(head))
	for i := range sep {
		sep[i] = "---"
		if align[i] == AlignRight {
			sep[i] = "---:"
		}
	}
	lines := []string{mdRow(head...), mdRow(sep...)}
	for _, r := range rows {
		lines = append(lines, mdRow(r...))
	}
	return strings.Join(lines, "\n")
}

// renderMD is GitHub-flavored Markdown for pasting into issues and PRs.
func (p *Printer) renderMD(v View) string {
	var blocks []string
	for _, b := range v {
		switch b := b.(type) {
		case Header:
			s := "### " + escapeMD(b.Title)
			if b.Meta != "" {
				s += "\n\n*" + escapeMD(b.Meta) + "*"
			}
			blocks = append(blocks, s)
		case Section:
			blocks = append(blocks, "#### "+escapeMD(b.Title))
		case KV:
			pairs := b.pairs(FormatMD)
			if len(pairs) == 0 {
				continue
			}
			rows := make([][]string, len(pairs))
			for i, pr := range pairs {
				rows[i] = []string{escapeMD(pairLabel(pr)), escapeMD(pr.Val.Text)}
			}
			blocks = append(blocks, mdTable([]string{"Key", "Value"}, []Align{AlignLeft, AlignLeft}, rows))
		case Table:
			idx := visibleCols(b.Cols, FormatMD)
			if len(idx) == 0 {
				continue
			}
			if len(b.Rows) == 0 && b.Empty != "" {
				blocks = append(blocks, escapeMD(b.Empty))
				continue
			}
			head := make([]string, len(idx))
			align := make([]Align, len(idx))
			for n, i := range idx {
				head[n], align[n] = escapeMD(b.Cols[i].Title), b.Cols[i].Align
			}
			rows := make([][]string, len(b.Rows))
			for r, row := range b.Rows {
				for _, i := range idx {
					rows[r] = append(rows[r], escapeMD(row[i].Text))
				}
			}
			blocks = append(blocks, mdTable(head, align, rows))
		case Checks:
			rows := make([][]string, len(b.Rows))
			for i, c := range b.Rows {
				detail := escapeMD(c.Detail)
				if c.Hint != "" {
					detail += " (`" + strings.ReplaceAll(c.Hint, "`", "'") + "`)"
				}
				rows[i] = []string{strings.ToUpper(c.Status), escapeMD(c.Name), detail}
			}
			blocks = append(blocks, mdTable([]string{"Status", "Check", "Detail"},
				[]Align{AlignLeft, AlignLeft, AlignLeft}, rows))
		case Note:
			blocks = append(blocks, "> "+escapeMD(b.Text))
		case Hint:
			blocks = append(blocks, "`"+strings.ReplaceAll(b.Cmd, "`", "'")+"` "+escapeMD(b.Text))
		case Line:
			blocks = append(blocks, escapeMD(b.Text))
		case Para:
			blocks = append(blocks, escapeMD(b.Text))
		case Pre:
			blocks = append(blocks, "```\n"+strings.ReplaceAll(b.Text, "```", "'''")+"\n```")
		case Defs:
			if len(b.Items) == 0 {
				continue
			}
			rows := make([][]string, len(b.Items))
			for i, it := range b.Items {
				rows[i] = []string{"`" + strings.ReplaceAll(it.Name, "`", "'") + "`", escapeMD(it.Desc)}
			}
			blocks = append(blocks, mdTable([]string{"Name", "Description"}, []Align{AlignLeft, AlignLeft}, rows))
		}
	}
	return joinBlocks(blocks)
}
