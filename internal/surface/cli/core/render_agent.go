package core

import (
	"slices"
	"strings"
)

// renderAgent renders v as agent lines: one `kind key=value ...` line per
// record, then the trailer `ok cmd=<cmd> [count=N] [fields...]`.
//
//   - A Table with Kind set yields one line per row, with a key per column
//     shown in agent mode (the cell's Raw value, else its Text).
//   - A KV with Kind set yields one record line; a KV without Kind
//     contributes its pairs to the trailer.
//   - Checks yield `check` lines.
//   - count is the total number of Table rows, present when the view has a
//     record Table (so an empty list says count=0).
//   - meta entries follow, sorted by key.
//
// Headers, sections, notes, hints and lines carry no data and are skipped.
func renderAgent(cmd string, v View, meta map[string]any) string {
	var lines []string
	var trailer []Field
	count, tables := 0, false
	for _, b := range v {
		switch b := b.(type) {
		case Table:
			if b.Kind == "" {
				continue
			}
			tables = true
			idx := visibleCols(b.Cols, FormatAgent)
			for _, row := range b.Rows {
				fields := make([]Field, 0, len(idx))
				for _, i := range idx {
					fields = append(fields, Field{b.Cols[i].Key, cellValue(row[i])})
				}
				lines = append(lines, EncodeLine(b.Kind, fields...))
				count++
			}
		case KV:
			pairs := b.pairs(FormatAgent)
			fields := make([]Field, 0, len(pairs))
			for _, p := range pairs {
				fields = append(fields, Field{p.Key, cellValue(p.Val)})
			}
			if b.Kind == "" {
				trailer = append(trailer, fields...)
			} else {
				lines = append(lines, EncodeLine(b.Kind, fields...))
			}
		case Checks:
			for _, c := range b.Rows {
				fields := []Field{{"name", c.Name}, {"status", c.Status}, {"detail", c.Detail}}
				if c.Hint != "" {
					fields = append(fields, Field{"hint", c.Hint})
				}
				lines = append(lines, EncodeLine("check", fields...))
			}
		case Defs:
			if b.Kind == "" {
				continue
			}
			tables = true
			for _, it := range b.Items {
				name := it.Name
				if it.Key != "" {
					name = it.Key
				}
				fields := append([]Field{{"name", name}, {"desc", it.Desc}}, it.Extra...)
				lines = append(lines, EncodeLine(b.Kind, fields...))
				count++
			}
		case Header, Section, Note, Hint, Line, Para:
		}
	}
	head := []Field{{"cmd", cmd}}
	if tables {
		head = append(head, Field{"count", count})
	}
	keys := make([]string, 0, len(meta))
	for k := range meta {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	head = append(head, trailer...)
	for _, k := range keys {
		head = append(head, Field{k, meta[k]})
	}
	lines = append(lines, EncodeLine("ok", head...))
	return strings.Join(lines, "\n") + "\n"
}

func cellValue(c Cell) any {
	if c.Raw != nil {
		return c.Raw
	}
	return c.Text
}
