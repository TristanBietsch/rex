package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/catalog/theme"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// The live colors and styles below are filled from a theme.Palette by
// applyTheme. Every renderer reads them; none builds an inline style or names
// a hex value, so a color_scheme change restyles the whole TUI.

// Live palette tokens. Mutated by applyTheme.
var (
	colorFgPrimary   lipgloss.Color
	colorFgDim       lipgloss.Color
	colorFgMuted     lipgloss.Color
	colorBorder      lipgloss.Color
	colorBorderFocus lipgloss.Color
	colorBgSel       lipgloss.Color
	colorAccent      lipgloss.Color
	colorWorking     lipgloss.Color
	colorQueued      lipgloss.Color
	colorNeeds       lipgloss.Color
	colorDone        lipgloss.Color
	colorFailed      lipgloss.Color
	colorCrashed     lipgloss.Color
	colorWarn        lipgloss.Color
	colorDanger      lipgloss.Color
	colorSuccess     lipgloss.Color
	colorInfo        lipgloss.Color
	colorFleet       []lipgloss.Color
	faintTones       bool // dim, muted and border tones are drawn faint (theme.Palette.Faint)
	selReverse       bool // the selection is reverse video (theme.Palette.SelReverse)
)

// Live styles. Rebuilt by rebuildStyles().
var (
	// Text.
	styleLogo         lipgloss.Style // "∴ REX"
	styleKey          lipgloss.Style // bold key caps and group titles
	styleKbd          lipgloss.Style // key caps inside the attach chrome
	styleCmd          lipgloss.Style // :command names in help
	styleBold         lipgloss.Style
	styleHeaderMeta   lipgloss.Style
	styleSectionTitle lipgloss.Style
	styleSlug         lipgloss.Style
	stylePrimary      lipgloss.Style
	styleDim          lipgloss.Style
	styleMuted        lipgloss.Style
	styleBorderFg     lipgloss.Style
	styleBorderFocus  lipgloss.Style // overlay panel frame
	styleSelected     lipgloss.Style
	styleCursor       lipgloss.Style // lit cursor: reverse video

	// Feedback.
	styleAlert       lipgloss.Style // bold Needs: an alert column title
	styleNeeds       lipgloss.Style
	styleDone        lipgloss.Style
	styleFailed      lipgloss.Style
	styleStateFailed lipgloss.Style
	styleWorking     lipgloss.Style

	// styleMarker is a state's bold marker style, by state; styleLongRun is
	// the long-running badge by tier (0 = most severe), see longRunTiers.
	styleMarker  map[protocol.State]lipgloss.Style
	styleLongRun []lipgloss.Style
	// styleFleet is the fleet bar's style by fleetColor index.
	styleFleet []lipgloss.Style

	styleChipActive lipgloss.Style
	styleChipDim    lipgloss.Style
	styleChipSep    lipgloss.Style

	styleBootOK      lipgloss.Style
	styleBootFail    lipgloss.Style
	styleBootWarn    lipgloss.Style
	styleBootSkip    lipgloss.Style
	styleBootRun     lipgloss.Style
	styleBootBracket lipgloss.Style
	styleBootHeader  lipgloss.Style
	styleBootReady   lipgloss.Style
	styleBootCause   lipgloss.Style

	styleArrow       lipgloss.Style // λ, ▸ and the wizard cursor
	styleArrowYellow lipgloss.Style // the ':' prompt and confirms
)

func init() {
	applyTheme(theme.DefaultName)
}

// applyTheme swaps the live palette and rebuilds all style vars. Unknown names
// fall back to the default palette silently. It writes package globals, so
// call it from Update, never from a tea.Cmd goroutine.
func applyTheme(name string) {
	p, _ := theme.Get(name)
	c := func(s string) lipgloss.Color { return lipgloss.Color(s) }
	colorFgPrimary = c(p.Fg)
	colorFgDim = c(p.FgDim)
	colorFgMuted = c(p.FgMuted)
	colorBorder = c(p.Border)
	colorBorderFocus = c(p.BorderFocus)
	colorBgSel = c(p.BgSel)
	selReverse = p.SelReverse
	faintTones = p.Faint
	colorAccent = c(p.Accent)
	colorWorking = c(p.Working)
	colorQueued = c(p.Queued)
	colorNeeds = c(p.Needs)
	colorDone = c(p.Done)
	colorFailed = c(p.Failed)
	colorCrashed = c(p.Crashed)
	colorWarn = c(p.Warn)
	colorDanger = c(p.Danger)
	colorSuccess = c(p.Success)
	colorInfo = c(p.Info)
	colorFleet = make([]lipgloss.Color, len(p.Fleet))
	for i, f := range p.Fleet {
		colorFleet[i] = c(f)
	}
	rebuildStyles()
}

// edgeStyle is the foreground-only style of the window frame, whose color
// (a per-launch-folder pick, not a palette token) is chosen at render time.
func edgeStyle(c lipgloss.Color) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(c)
}

func rebuildStyles() {
	fg := func(c lipgloss.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }
	bold := func(c lipgloss.Color) lipgloss.Style { return lipgloss.NewStyle().Bold(true).Foreground(c) }
	// quiet is fg for the dim, muted and border tones: a scheme with no
	// color of its own for them draws the terminal default faint.
	quiet := func(c lipgloss.Color) lipgloss.Style { return fg(c).Faint(faintTones) }

	styleLogo = bold(colorAccent)
	styleKey = bold(colorFgPrimary)
	styleKbd = bold(colorAccent)
	styleCmd = bold(colorAccent)
	styleBold = lipgloss.NewStyle().Bold(true)
	styleHeaderMeta = quiet(colorFgDim)
	styleSectionTitle = bold(colorFgPrimary)
	styleSlug = bold(colorFgPrimary)
	stylePrimary = fg(colorFgPrimary)
	styleDim = quiet(colorFgDim)
	styleMuted = quiet(colorFgMuted)
	styleBorderFg = quiet(colorBorder)
	styleBorderFocus = fg(colorBorderFocus)
	styleSelected = selectedStyle(fg(colorFgPrimary))
	styleCursor = lipgloss.NewStyle().Reverse(true)

	styleAlert = bold(colorNeeds)
	styleNeeds = fg(colorNeeds)
	styleDone = fg(colorDone)
	styleFailed = fg(colorFailed)
	styleStateFailed = bold(colorFailed)
	styleWorking = fg(colorWorking)

	styleMarker = map[protocol.State]lipgloss.Style{
		protocol.StateWorking:    bold(colorWorking),
		protocol.StateQueued:     bold(colorQueued),
		protocol.StateNeedsInput: bold(colorNeeds),
		protocol.StateDone:       bold(colorDone),
		protocol.StateFailed:     bold(colorFailed),
		protocol.StateCrashed:    bold(colorCrashed),
	}
	styleLongRun = []lipgloss.Style{
		bold(colorDanger).Reverse(true),
		bold(colorDanger),
		fg(colorWarn),
		quiet(colorFgDim),
	}
	styleFleet = make([]lipgloss.Style, len(colorFleet))
	for i, c := range colorFleet {
		styleFleet[i] = fg(c)
	}

	styleChipActive = fg(colorFgPrimary)
	styleChipDim = quiet(colorFgDim)
	styleChipSep = quiet(colorFgMuted)

	styleBootOK = bold(colorSuccess)
	styleBootFail = bold(colorDanger)
	styleBootWarn = fg(colorWarn)
	styleBootSkip = quiet(colorFgMuted)
	styleBootRun = fg(colorInfo)
	styleBootBracket = quiet(colorFgMuted)
	styleBootHeader = bold(colorFgPrimary)
	styleBootReady = fg(colorSuccess)
	styleBootCause = fg(colorDanger)

	// Rebuilt with the palette: package-level initializers run before
	// init(), when the colors are still empty.
	styleArrow = fg(colorAccent)
	styleArrowYellow = fg(colorWarn)
}

// brandStyle colors a tool's glyph with its registry brand color. That color
// is tool data (tools.yaml), not a theme token, so it is the one place a
// color outside the palette reaches a style.
func brandStyle(color string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color))
}

// selectedStyle marks style as the selection: the palette's selection
// background, or reverse video for a palette that leaves the colors to the
// terminal (ansi).
func selectedStyle(style lipgloss.Style) lipgloss.Style {
	if selReverse {
		return style.Reverse(true)
	}
	return style.Background(colorBgSel)
}

// renderHR renders a horizontal rule line of given width.
func renderHR(width int) string {
	if width <= 0 {
		return ""
	}
	line := ""
	for i := 0; i < width; i++ {
		line += "─"
	}
	return styleBorderFg.Render(line)
}

// padLine pads s to exactly width visible columns (right-padded with spaces).
// No background is applied — the terminal's native bg shows through. Lines
// wider than width are truncated with "…", never wrapped: a wrapped header or
// status line pushes the board down a row and breaks the fixed line math
// (boardTop, mouse hit-testing).
func padLine(s string, width int) string {
	if width <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		w := ansi.StringWidth(l)
		if w > width {
			l = ansi.Truncate(l, width, "…")
			w = ansi.StringWidth(l)
		}
		lines[i] = l + strings.Repeat(" ", width-w)
	}
	return strings.Join(lines, "\n")
}
