package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// palette is one named color scheme. All renderers read from the package-level
// theme/style vars below, which are rebuilt on applyTheme().
type palette struct {
	BgElev, BgModal              lipgloss.Color
	FgPrimary, FgDim, FgMuted    lipgloss.Color
	Border                       lipgloss.Color
	Working, Needs, Done, Failed lipgloss.Color
	Crashed                      lipgloss.Color
}

var palettes = map[string]palette{
	"default": {
		BgElev:    "#171922",
		BgModal:   "#1B1E29",
		FgPrimary: "#E6E6E6",
		FgDim:     "#7A7F8C",
		FgMuted:   "#4A4F5A",
		Border:    "#262A36",
		Working:   "#5B8DEF",
		Needs:     "#E5B341",
		Done:      "#4ADE80",
		Failed:    "#EF4444",
		Crashed:   "#7A7F8C",
	},
	"noir": {
		BgElev:    "#0A0D14",
		BgModal:   "#05080F",
		FgPrimary: "#D8DDE6",
		FgDim:     "#6A7280",
		FgMuted:   "#3A4150",
		Border:    "#1A1E28",
		Working:   "#7AA6F0",
		Needs:     "#D4B574",
		Done:      "#5BC487",
		Failed:    "#D85A5A",
		Crashed:   "#6A7280",
	},
	"paper": {
		BgElev:    "#F2F0EA",
		BgModal:   "#FFFFFF",
		FgPrimary: "#1A1A1A",
		FgDim:     "#5C5C5C",
		FgMuted:   "#999999",
		Border:    "#D0CFC9",
		Working:   "#2D5BB8",
		Needs:     "#B58800",
		Done:      "#2C7A48",
		Failed:    "#B83333",
		Crashed:   "#999999",
	},
}

// Live palette. Mutated by applyTheme.
var (
	colorBgElev    lipgloss.Color
	colorBgModal   lipgloss.Color
	colorFgPrimary lipgloss.Color
	colorFgDim     lipgloss.Color
	colorFgMuted   lipgloss.Color
	colorBorder    lipgloss.Color

	colorWorking lipgloss.Color
	colorNeeds   lipgloss.Color
	colorDone    lipgloss.Color
	colorFailed  lipgloss.Color
	colorCrashed lipgloss.Color
)

// Live styles. Rebuilt by rebuildStyles().
var (
	styleHeaderApp    lipgloss.Style
	styleHeaderMeta   lipgloss.Style
	styleSectionTitle lipgloss.Style
	styleSlug         lipgloss.Style
	stylePrimary      lipgloss.Style
	styleDim          lipgloss.Style
	styleMuted        lipgloss.Style
	styleBorderFg     lipgloss.Style
	styleSelected     lipgloss.Style

	styleStateFailed lipgloss.Style

	styleChipActive lipgloss.Style
	styleChipDim    lipgloss.Style
	styleChipSep    lipgloss.Style

	// Prompt glyphs (▸ λ : ✎) and the selected-row arrow.
	styleArrow       lipgloss.Style
	styleArrowYellow lipgloss.Style

	styleBootOK      lipgloss.Style
	styleBootFail    lipgloss.Style
	styleBootWarn    lipgloss.Style
	styleBootSkip    lipgloss.Style
	styleBootRun     lipgloss.Style
	styleBootBracket lipgloss.Style
	styleBootHeader  lipgloss.Style
	styleBootReady   lipgloss.Style
	styleBootCause   lipgloss.Style
)

func init() {
	applyTheme("default")
}

// applyTheme swaps the live palette and rebuilds all style vars. Unknown names
// fall back to "default" silently.
func applyTheme(name string) {
	p, ok := palettes[name]
	if !ok {
		p = palettes["default"]
	}
	colorBgElev = p.BgElev
	colorBgModal = p.BgModal
	colorFgPrimary = p.FgPrimary
	colorFgDim = p.FgDim
	colorFgMuted = p.FgMuted
	colorBorder = p.Border
	colorWorking = p.Working
	colorNeeds = p.Needs
	colorDone = p.Done
	colorFailed = p.Failed
	colorCrashed = p.Crashed
	rebuildStyles()
}

func rebuildStyles() {
	styleHeaderApp = lipgloss.NewStyle().Bold(true).Foreground(colorFgPrimary)
	styleHeaderMeta = lipgloss.NewStyle().Foreground(colorFgDim)
	styleSectionTitle = lipgloss.NewStyle().Bold(true).Foreground(colorFgPrimary)
	styleSlug = lipgloss.NewStyle().Bold(true).Foreground(colorFgPrimary)
	stylePrimary = lipgloss.NewStyle().Foreground(colorFgPrimary)
	styleDim = lipgloss.NewStyle().Foreground(colorFgDim)
	styleMuted = lipgloss.NewStyle().Foreground(colorFgMuted)
	styleBorderFg = lipgloss.NewStyle().Foreground(colorBorder)
	styleSelected = lipgloss.NewStyle().Background(colorBgElev).Foreground(colorFgPrimary)

	styleStateFailed = lipgloss.NewStyle().Bold(true).Foreground(colorFailed)

	styleChipActive = lipgloss.NewStyle().Foreground(colorFgPrimary)
	styleChipDim = lipgloss.NewStyle().Foreground(colorFgDim)
	styleChipSep = lipgloss.NewStyle().Foreground(colorFgMuted)

	styleArrow = lipgloss.NewStyle().Foreground(colorWorking)
	styleArrowYellow = lipgloss.NewStyle().Foreground(colorNeeds)

	styleBootOK = lipgloss.NewStyle().Bold(true).Foreground(colorDone)
	styleBootFail = lipgloss.NewStyle().Bold(true).Foreground(colorFailed)
	styleBootWarn = lipgloss.NewStyle().Foreground(colorNeeds)
	styleBootSkip = lipgloss.NewStyle().Foreground(colorFgMuted)
	styleBootRun = lipgloss.NewStyle().Foreground(colorWorking)
	styleBootBracket = lipgloss.NewStyle().Foreground(colorFgMuted)
	styleBootHeader = lipgloss.NewStyle().Bold(true).Foreground(colorFgPrimary)
	styleBootReady = lipgloss.NewStyle().Foreground(colorDone)
	styleBootCause = lipgloss.NewStyle().Foreground(colorFailed)
}

// renderHR renders a horizontal rule line of given width.
func renderHR(width int) string {
	if width <= 0 {
		return ""
	}
	return styleBorderFg.Render(strings.Repeat("─", width))
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
