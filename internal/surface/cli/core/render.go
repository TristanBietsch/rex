package core

import (
	"io"
	"strings"
)

// Emit renders r to stdout in the Printer's format. It writes once, so a
// failed render leaves stdout empty.
//
// With -json (Legacy) it calls Result.Legacy and nothing else; a command
// that did not supply one is an ExitInvalidArgs error.
func (p *Printer) Emit(r Result) error {
	cmd := r.Cmd
	if cmd == "" {
		cmd = p.Cmd
	}
	if p.Legacy {
		if r.Legacy == nil {
			return NewExitError(ExitInvalidArgs, cmd+": -json is not supported")
		}
		return r.Legacy(p.Out)
	}
	if r.Content != "" && p.Format != FormatJSON {
		s := strings.TrimSuffix(r.Content, "\n") + "\n"
		if p.Format == FormatMD {
			s = "```text\n" + s + "```\n"
		}
		_, err := io.WriteString(p.Out, s)
		return err
	}
	var s string
	switch p.Format {
	case FormatJSON:
		var err error
		if s, err = renderJSON(cmd, r); err != nil {
			return err
		}
	case FormatAgent:
		s = renderAgent(cmd, r.View, r.Meta, r.Failed)
	case FormatMD:
		s = p.renderMD(r.View.sanitized())
	case FormatTXT:
		s = p.renderTXT(r.View.sanitized())
	default:
		s = p.renderText(r.View.sanitized())
	}
	_, err := io.WriteString(p.Out, s)
	return err
}

// joinBlocks joins rendered blocks with one blank line and ends the output
// with a single newline. A block that rendered nothing (a KV whose pairs are
// all for another format) is skipped; no blocks means no output.
func joinBlocks(blocks []string) string {
	kept := make([]string, 0, len(blocks))
	for _, b := range blocks {
		if b != "" {
			kept = append(kept, b)
		}
	}
	if len(kept) == 0 {
		return ""
	}
	return strings.Join(kept, "\n\n") + "\n"
}

// blockList collects rendered blocks. A Section is attached to the block
// after it (no blank line between the title and its body); every other
// pair of blocks is separated by one blank line.
type blockList struct {
	blocks []string
	attach bool
}

func (l *blockList) add(s string) {
	if s == "" {
		return
	}
	if l.attach && len(l.blocks) > 0 {
		l.blocks[len(l.blocks)-1] += "\n" + s
	} else {
		l.blocks = append(l.blocks, s)
	}
	l.attach = false
}

func (l *blockList) section(s string) {
	l.add(s)
	l.attach = true
}

func (l *blockList) String() string { return joinBlocks(l.blocks) }

// visibleCols returns the indexes of the columns shown in f.
func visibleCols(cols []Col, f Format) []int {
	var idx []int
	for i, c := range cols {
		if c.shownIn(f) {
			idx = append(idx, i)
		}
	}
	return idx
}

// padLeft and padRight pad s with spaces to w display cells.
func padRight(s string, w int) string { return padCells(s, w) }

func padLeft(s string, w int) string {
	if gap := w - displayWidth(s); gap > 0 {
		return strings.Repeat(" ", gap) + s
	}
	return s
}
