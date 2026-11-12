// Package theme holds the semantic color palettes shared by the TUI and the
// CLI.
//
// A Palette names a color by meaning (text, state, feedback) rather than by
// widget. Values are strings so they work with lipgloss.Color unchanged:
// "#RRGGBB", an ANSI index "0"-"255", or "" for the terminal default.
package theme

import (
	"slices"
	"sort"
)

// Palette is one named color scheme.
type Palette struct {
	Name string
	Dark bool
	// Bg is the assumed terminal background. It is never painted; it exists
	// for caret and contrast decisions. Bg, Fg and BgSel are empty in a
	// scheme that leaves them to the terminal ("ansi").
	Bg, Fg, FgDim, FgMuted string
	BgSel, Border          string
	BorderFocus            string
	// Accent marks the prompt arrow, the focused input, the logo and command
	// names in help.
	Accent string
	// Per-state colors, matching the TUI legend.
	Working, Queued, Needs, Done, Failed, Crashed string
	// Feedback colors; they default to the state color of the same meaning.
	Warn, Danger, Success, Info string
	Fleet                       []string
	// SelReverse draws the selection in reverse video instead of BgSel.
	SelReverse bool
}

// DefaultName is the palette used when a name is unknown or unset.
const DefaultName = "default"

// Builtin maps scheme names to palettes. Treat it as read-only; use Get for a
// copy that is safe to modify.
var Builtin = map[string]Palette{
	"default":       modern,
	"classic":       classic,
	"noir":          noir,
	"paper":         paper,
	"high-contrast": highContrast,
	"ansi":          ansi,
}

// Names returns the built-in scheme names in sorted order.
func Names() []string {
	names := make([]string, 0, len(Builtin))
	for n := range Builtin {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Get returns a copy of the named palette. An unknown name returns the
// default palette and false.
func Get(name string) (Palette, bool) {
	p, ok := Builtin[name]
	if !ok {
		p = Builtin[DefaultName]
	}
	p.Fleet = slices.Clone(p.Fleet)
	return p, ok
}
