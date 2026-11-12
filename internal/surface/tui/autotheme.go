package tui

import (
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/catalog/theme"
)

// hasDarkBackground asks the terminal for its background color (OSC 11, via
// termenv). It reads stdin, so it must only run before Bubble Tea owns it.
var hasDarkBackground = lipgloss.HasDarkBackground

// termLightFor reports whether the auto theme should pick its light theme.
// It looks at the terminal only when the theme setting is auto: first
// COLORFGBG, then the OSC 11 query. Any other theme, or no store, is the
// dark default without a query.
func termLightFor(store *settings.Store, getenv func(string) string, query func() bool) bool {
	if store == nil || store.String("theme") != theme.AutoName {
		return false
	}
	if dark, ok := colorFGBGDark(getenv("COLORFGBG")); ok {
		return !dark
	}
	return !query()
}

// bootTermLight is termLightFor with the real environment and terminal. Run
// calls it before tea.NewProgram.
func bootTermLight(store *settings.Store) bool {
	return termLightFor(store, os.Getenv, hasDarkBackground)
}

// colorFGBGDark reads COLORFGBG ("fg;bg" or "fg;default;bg", set by rxvt,
// Konsole and others). The background is its last field, an ANSI index:
// 7 (light gray) and 9-15 are light, 0-6 and 8 dark. ok is false when the
// variable is unset or says nothing usable ("default", out of range).
func colorFGBGDark(v string) (dark, ok bool) {
	i := strings.LastIndexByte(v, ';')
	if i < 0 {
		return false, false
	}
	bg, err := strconv.Atoi(v[i+1:])
	if err != nil || bg < 0 || bg > 15 {
		return false, false
	}
	return bg != 7 && bg < 9, true
}

// resolveThemeName is the theme to load for name: auto becomes default or
// paper by the boot-time background; any other name is unchanged.
func resolveThemeName(m Model, name string) string {
	if name == theme.AutoName {
		return theme.AutoPick(!m.termLight)
	}
	return name
}
