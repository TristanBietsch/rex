package tui

// boardLayout is the column layout renderBoard uses for m at width.
func boardLayout(m Model, width int) rowCols {
	return layoutSlots(boardSlots(m), m.now(), width)
}
