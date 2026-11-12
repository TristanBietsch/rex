package theme

import (
	"math"
	"strconv"
)

// Contrast is the WCAG 2.x contrast ratio (1 to 21) of two "#RRGGBB" colors.
// It is 0 when either is not a hex color (an ANSI index or "" has no
// measurable value: the terminal decides what it looks like).
func Contrast(fg, bg string) float64 {
	l1, ok1 := luminance(fg)
	l2, ok2 := luminance(bg)
	if !ok1 || !ok2 {
		return 0
	}
	if l1 < l2 {
		l1, l2 = l2, l1
	}
	return (l1 + 0.05) / (l2 + 0.05)
}

// luminance is the WCAG relative luminance of a "#RRGGBB" color.
func luminance(hex string) (float64, bool) {
	if len(hex) != 7 || hex[0] != '#' {
		return 0, false
	}
	v, err := strconv.ParseUint(hex[1:], 16, 32)
	if err != nil {
		return 0, false
	}
	lin := func(c uint64) float64 {
		f := float64(c) / 255
		if f <= 0.03928 {
			return f / 12.92
		}
		return math.Pow((f+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(v>>16&0xFF) + 0.7152*lin(v>>8&0xFF) + 0.0722*lin(v&0xFF), true
}
