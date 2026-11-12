package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

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
	times    bool // the timestamps feature is on: the span column exists
	fit      bool // column_layout: fit

	// icons is the icon strip's slots in layout order (row_icons on, not
	// mobile). nil is the off path: the text column, today's layout.
	icons []iconSlot
	// slotShown reports whether a row has a value for a slot (fit layout).
	slotShown func(protocol.SessionSummary, iconSlot) bool
}

func layoutOptsOf(m Model) layoutOpts {
	o := layoutOpts{
		detailed: m.Detailed,
		resume:   m.featureOn("resume"),
		times:    m.featureOn("timestamps"),
		fit:      m.Store != nil && m.Store.String("column_layout") == columnLayoutFit,
	}
	if rowIconsOn(m) {
		o.icons = iconSlots(m)
		if o.icons == nil {
			o.icons = []iconSlot{} // every slot removed: still the strip path
		}
		o.slotShown = func(s protocol.SessionSummary, slot iconSlot) bool {
			return strings.TrimSpace(ansi.Strip(slotCell(m, s, slot, slotWidth[slot], false))) != ""
		}
	}
	return o
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

// iconClassSpecs is classSpecs with row_icons on: the name widths of the
// icon layout and the same detail columns. Subagents and the long-run badge
// are strip slots there, not columns.
var iconClassSpecs = [...]classSpec{
	classWide: {
		simple:   colSet{slug: 32},
		detailed: colSet{slug: 24, id: colID, model: 24, tokens: colTokens, span: colSpan, resume: colResume},
	},
	classNormal: {
		simple:   colSet{slug: 24},
		detailed: colSet{slug: 18, id: colID, model: 16, tokens: colTokens},
	},
	classNarrow: {
		simple:   colSet{slug: 20},
		detailed: colSet{slug: 20},
	},
}

// layoutRows sizes the row columns for a content width.
func layoutRows(rows []protocol.SessionSummary, now time.Time, width int, o layoutOpts) rowCols {
	if o.icons != nil {
		return layoutIcons(rows, now, width, o)
	}
	if o.fit {
		return layoutFit(rows, now, width, o.detailed, o.resume, o.times)
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
	if !o.times {
		c.span = 0
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

// layoutIcons sizes the icon layout. fixed: the class table and the slot
// widths alone, no row content read. fit: every column and slot is at most
// its fixed width, sized to content, and a slot exists only while some row
// has a value for it. Then the squeeze, only when the spacer would go
// negative (see squeezeIcons).
func layoutIcons(rows []protocol.SessionSummary, now time.Time, width int, o layoutOpts) rowCols {
	class := classForWidth(width)
	if class == classMobile {
		class = classNarrow // mobile_layout: off keeps the narrow widths
	}
	set := iconClassSpecs[class].simple
	if o.detailed {
		set = iconClassSpecs[class].detailed
	}
	c := rowCols{icons: true, id: set.id, slug: set.slug, model: set.model,
		tokens: set.tokens, span: set.span, resume: set.resume}
	if !o.resume {
		c.resume = 0
	}
	if !o.times {
		c.span = 0
	}
	for _, s := range o.icons {
		c.slots[s] = slotWidth[s]
	}
	if o.fit {
		fitIcons(&c, rows, now, o)
	}
	squeezeIcons(&c, width)
	c.spacer = max(width-c.used(), 0)
	return c
}

// fitIcons shrinks the fixed icon layout to the rows' content.
func fitIcons(c *rowCols, rows []protocol.SessionSummary, now time.Time, o layoutOpts) {
	var slug, model, tokens, span, resume int
	var shown [len(slotWidth)]bool
	for _, s := range rows {
		slug = max(slug, ansi.StringWidth(displayName(s)))
		model = max(model, ansi.StringWidth(modelEffortLabel(s)))
		if s.Tokens > 0 {
			tokens = max(tokens, ansi.StringWidth(formatTokens(s.Tokens)))
		}
		span = max(span, ansi.StringWidth(spanText(now, s)))
		if resumeIDShown(s) != "" {
			resume = colResume
		}
		for _, slot := range o.icons {
			if !shown[slot] && o.slotShown != nil && o.slotShown(s, slot) {
				shown[slot] = true
			}
		}
	}
	c.slug = min(c.slug, slug)
	c.model = min(c.model, model)
	c.tokens = min(c.tokens, tokens)
	c.span = min(c.span, span)
	c.resume = min(c.resume, resume)
	for i := range c.slots {
		if !shown[i] {
			c.slots[i] = 0
		}
	}
}

// squeezeIcons runs only below the class minimum (the spacer would be
// negative): drop spend, long-run, loop, subagents in that order, then the
// why slot shrinks to its glyph, then the name to slugMin, then the unseen
// slot goes. A dropped slot takes its gap with it. Past that the name gives
// way to 0.
func squeezeIcons(c *rowCols, width int) {
	short := func() int { return c.used() - width }
	for _, s := range []iconSlot{slotSpend, slotLongRun, slotLoop, slotSubagents} {
		if short() <= 0 {
			return
		}
		c.slots[s] = 0
	}
	if short() > 0 && c.slots[slotReason] > 1 {
		c.slots[slotReason] = 1
	}
	if n := short(); n > 0 && c.slug > slugMin {
		c.slug = max(c.slug-n, slugMin)
	}
	if short() > 0 {
		c.slots[slotUnseen] = 0
	}
	if n := short(); n > 0 {
		c.slug = max(c.slug-n, 0)
	}
}

// usedIcons is used() on the icons path: everything but the spacer.
func (c rowCols) usedIcons() int {
	w := rowIndent + colMarker + colGap + c.slug + colGap + colTime
	for _, x := range []int{c.id, c.model, c.tokens, c.span, c.resume, c.agents, c.run} {
		if x > 0 {
			w += colGap + x
		}
	}
	for _, x := range c.slots {
		if x > 0 {
			w += colGap + x
		}
	}
	return w
}
