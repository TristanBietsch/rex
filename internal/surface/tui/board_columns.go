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

// columnSizer sizes the row columns for rows at a content width.
type columnSizer func(rows []protocol.SessionSummary, now time.Time, width int, detailed, resume bool) rowCols

// columnLayouts picks the column sizer of each class.
var columnLayouts = [...]columnSizer{
	classWide:   layoutFit,
	classNormal: layoutFit,
	classNarrow: layoutFit,
	classMobile: layoutFit,
}

// layoutRows sizes the row columns for a content width.
func layoutRows(rows []protocol.SessionSummary, now time.Time, width int, detailed, resume bool) rowCols {
	return columnLayouts[classForWidth(width)](rows, now, width, detailed, resume)
}
