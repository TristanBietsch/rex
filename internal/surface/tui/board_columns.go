package tui

import (
	"time"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// layoutClass is the board's width class. The class is recomputed from the
// content width on every frame, so columns only change at its breakpoints.
type layoutClass int

const (
	classWide   layoutClass = iota // >= breakWide
	classNormal                    // breakNormal .. breakWide-1
	classNarrow                    // breakNarrow .. breakNormal-1
	classMobile                    // < breakNarrow
)

// Breakpoints: the lowest content width of each class.
const (
	breakWide   = 150
	breakNormal = 110
	breakNarrow = 61
)

// classForWidth maps a content width to its class.
func classForWidth(cw int) layoutClass {
	switch {
	case cw >= breakWide:
		return classWide
	case cw >= breakNormal:
		return classNormal
	case cw >= breakNarrow:
		return classNarrow
	default:
		return classMobile
	}
}

// mobileWidth is the widest content width that auto turns the mobile layout
// on for (the class below breakNarrow).
const mobileWidth = breakNarrow - 1

// mobileLayout reports whether the board uses the mobile layout: stacked
// two-line rows and a compact header and help line. mobile_layout: always
// forces it, off never uses it, auto (the default) uses it from mobileWidth
// columns down. Before the first size message (Width 0) auto is off.
func mobileLayout(m Model) bool {
	mode := "auto"
	if m.Store != nil {
		if v := m.Store.String("mobile_layout"); v != "" {
			mode = v
		}
	}
	switch mode {
	case "always":
		return true
	case "off":
		return false
	}
	return m.Width > 0 && contentWidth(m.Width) <= mobileWidth
}

// layoutClassOf is the board's class for the current window.
func layoutClassOf(m Model) layoutClass {
	if mobileLayout(m) {
		return classMobile
	}
	if c := classForWidth(contentWidth(m.Width)); c != classMobile {
		return c
	}
	return classNarrow // mobile_layout: off
}

// rowLines is how many lines one session takes on the board.
func rowLines(m Model) int {
	if mobileLayout(m) {
		return 2
	}
	return 1
}

// columnLayoutFit is the column_layout value that sizes columns to content.
const columnLayoutFit = "fit"

// layoutOpts is what column sizing needs besides the rows and the width.
type layoutOpts struct {
	detailed bool // the detailed view (v)
	resume   bool // the resume feature is on: the resume column exists
	fit      bool // column_layout: fit
}

func layoutOptsOf(m Model) layoutOpts {
	return layoutOpts{
		detailed: m.Detailed,
		resume:   m.featureOn("resume"),
		fit:      m.Store != nil && m.Store.String("column_layout") == columnLayoutFit,
	}
}

// colSet is the widths of one view in one class. 0 means no column (no
// cell, no gap). A column that is present is always reserved, empty or not.
type colSet struct {
	slug, id, model, tokens, span, resume, agents, run int
}

// classSpec is a class's fixed widths for each view.
type classSpec struct {
	simple, detailed colSet
}

// classSpecs holds the fixed column widths of every column class (mobile has
// no columns: its rows are stacked). The description takes whatever is left.
var classSpecs = [...]classSpec{
	classWide: {
		simple:   colSet{slug: 22, agents: colAgents, run: colRun},
		detailed: colSet{slug: 22, id: colID, model: 24, tokens: colTokens, span: colSpan, resume: colResume, agents: colAgents, run: colRun},
	},
	classNormal: {
		simple:   colSet{slug: 20, agents: colAgents, run: colRun},
		detailed: colSet{slug: 20, id: colID, model: 16, tokens: colTokens, agents: colAgents, run: colRun},
	},
	classNarrow: {
		simple:   colSet{slug: 16, agents: colAgents, run: colRun},
		detailed: colSet{slug: 16, agents: colAgents, run: colRun},
	},
}

// layoutRows sizes the row columns for a content width.
func layoutRows(rows []protocol.SessionSummary, now time.Time, width int, o layoutOpts) rowCols {
	if o.fit {
		return layoutFit(rows, now, width, o.detailed, o.resume)
	}
	return layoutFixed(width, o)
}

// layoutFixed sizes the columns from the class table alone: no row content
// is read, so a column never moves when a row changes. Only a terminal too
// narrow for the class (mobile_layout: off below breakNarrow) squeezes: the
// badge column, then the subagent column go, then the name gives way, so
// the description keeps descMinNarrow.
func layoutFixed(width int, o layoutOpts) rowCols {
	class := classForWidth(width)
	if class == classMobile {
		class = classNarrow // mobile_layout: off keeps the narrow widths
	}
	spec := classSpecs[class]
	set := spec.simple
	if o.detailed {
		set = spec.detailed
	}
	c := rowCols{id: set.id, slug: set.slug, model: set.model, tokens: set.tokens,
		span: set.span, resume: set.resume, agents: set.agents, run: set.run}
	if !o.resume {
		c.resume = 0
	}
	for _, drop := range []*int{&c.run, &c.agents} {
		if width-c.used() >= descMinNarrow {
			break
		}
		*drop = 0
	}
	if short := descMinNarrow - (width - c.used()); short > 0 {
		c.slug = max(c.slug-short, 0)
	}
	c.desc = max(width-c.used(), 0)
	return c
}
