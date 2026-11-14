package protocol

// Terminal size bounds the daemon accepts. A client-chosen size feeds the
// daemon's screen mirror (one cell per column×row), so an unbounded uint16
// pair would allocate billions of cells.
const (
	MinTermCols uint16 = 20
	MaxTermCols uint16 = 1000
	MinTermRows uint16 = 5
	MaxTermRows uint16 = 500
)

// ClampTermSize clamps a requested terminal size to
// [MinTermCols..MaxTermCols]×[MinTermRows..MaxTermRows]. A zero dimension
// means "use the default" and stays zero.
func ClampTermSize(cols, rows uint16) (uint16, uint16) {
	return clampDim(cols, MinTermCols, MaxTermCols), clampDim(rows, MinTermRows, MaxTermRows)
}

func clampDim(v, lo, hi uint16) uint16 {
	switch {
	case v == 0:
		return 0
	case v < lo:
		return lo
	case v > hi:
		return hi
	}
	return v
}
