package tui

// attachChrome is the frame around the attached agent's terminal, the single
// source of the popup's geometry: attachChromeOf(m).innerSize sizes the PTY,
// boxPad shapes the box renderAttach is drawn in, and origin finds the
// terminal's top-left cell for the mouse. Reading all three from one value
// keeps them from drifting apart.
//
// The popup is a bordered box with padding (padX columns, padY rows) and a
// margin to the screen edge. In the mobile layout it is a sheet instead: the
// border only, no padding, filling the screen.
type attachChrome struct {
	sheet  bool // mobile: no margin, no padding
	banner int  // banner rows (0 or 1) between the title bar and the rule
}

// Popup box constants: one border cell each side, padding inside it, and the
// title, two rules and footer that renderAttach adds around the terminal.
const (
	attachBorder    = 1
	attachPopupPadX = 2
	attachPopupPadY = 1
	attachBarRows   = 4 // title, rule, rule, footer
	// attachMinCols is the narrowest the agent's terminal is made on a
	// screen that has room; below that it follows the screen.
	attachMinCols = 40
	attachMinRows = 8
)

// attachChromeOf is the frame for the current window: a sheet in the mobile
// layout. The banner rows are the ones the popup shows now.
func attachChromeOf(m Model) attachChrome {
	c := attachChrome{sheet: mobileLayout(m)}
	c.banner = attachBannerRowsFor(m, c)
	return c
}

// withBanner is c with an explicit banner row count (the rows a live popup
// was sized with).
func (c attachChrome) withBanner(rows int) attachChrome {
	c.banner = rows
	return c
}

func (c attachChrome) padX() int {
	if c.sheet {
		return 0
	}
	return attachPopupPadX
}

func (c attachChrome) padY() int {
	if c.sheet {
		return 0
	}
	return attachPopupPadY
}

// innerSize is the agent's terminal size on a termW x termH screen.
func (c attachChrome) innerSize(termW, termH int) (cols, rows int) {
	if termW <= 0 {
		termW = 100
	}
	if termH <= 0 {
		termH = 32
	}
	if c.sheet {
		// The border is the only chrome and the box fills the screen.
		cols = max(termW-2*attachBorder, 1)
		rows = max(termH-2*attachBorder-attachBarRows-c.banner, attachMinRows)
		return cols, rows
	}
	// 4 columns of margin each side, then border and padding inside it; 2
	// rows of margin top and bottom.
	cols = termW - 8 - 2*(attachBorder+attachPopupPadX)
	rows = termH - 4 - 2*(attachBorder+attachPopupPadY) - attachBarRows - c.banner
	// Never wider than the screen: the popup is cols + border + padding wide.
	cols = max(cols, min(attachMinCols, termW-2*(attachBorder+attachPopupPadX)), 1)
	rows = max(rows, attachMinRows)
	return cols, rows
}

// boxSize is the popup's outer size around a cols x rows terminal.
func (c attachChrome) boxSize(cols, rows int) (w, h int) {
	w = cols + 2*(attachBorder+c.padX())
	h = rows + attachBarRows + c.banner + 2*(attachBorder+c.padY())
	return w, h
}

// origin is the 0-based screen cell of the terminal's top-left cell when the
// popup is centered on a termW x termH screen (as compositeOverlay does).
func (c attachChrome) origin(termW, termH, cols, rows int) (x, y int) {
	if termW <= 0 {
		termW = 100
	}
	if termH <= 0 {
		termH = 32
	}
	boxW, boxH := c.boxSize(cols, rows)
	ox := max((termW-boxW)/2, 0)
	oy := max((termH-boxH)/2, 0)
	// Inside the box: border, padding, then the title bar, the banner and
	// the rule above the terminal.
	return ox + attachBorder + c.padX(), oy + attachBorder + c.padY() + 2 + c.banner
}
