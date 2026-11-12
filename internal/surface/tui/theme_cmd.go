package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
)

// themeOptions are the names the theme setting can cycle through: the
// built-ins in registry order, then the user's theme files by name.
func themeOptions(m Model) []string {
	s, _ := settings.Find("theme")
	return append(slices.Clone(s.Options), m.Themes.UserNames()...)
}

// applyThemeNamed applies the named theme. An unknown name uses the default
// palette and puts a notice on the status line.
func applyThemeNamed(m *Model, name string) {
	if !applyThemeFrom(m.Themes, name) {
		m.Err = fmt.Sprintf("theme %q not found (using default)", name)
		if why := m.Themes.WhySkipped(name); why != "" {
			m.Err = fmt.Sprintf("theme %q not loaded: %s (using default)", name, why)
		}
	}
}

// setThemeSetting stores, applies and saves a theme choice.
func setThemeSetting(m Model, name string) Model {
	if err := m.Store.Set("theme", name); err != nil {
		m.Err = "theme: " + err.Error()
		return m
	}
	applyLive(&m, "theme", m.Store.Get("theme"))
	if err := m.Store.Save(m.StorePath); err != nil {
		m.Err = "theme: save: " + err.Error()
	}
	return m
}

// runTheme is `:theme [name]`: set the theme (previewed at once and saved),
// or with no name cycle to the next one.
func runTheme(m Model, c cmdCall) (Model, tea.Cmd) {
	opts := themeOptions(m)
	var name string
	switch len(c.Args) {
	case 0:
		cur, _ := m.Store.Get("theme").(string)
		name = cycleOption(opts, cur, +1)
	case 1:
		name = c.Args[0]
		if !slices.Contains(opts, name) {
			m.Err = fmt.Sprintf("theme: unknown %q (have: %s)", name, strings.Join(opts, ", "))
			return m, nil
		}
	default:
		m.Err = "theme: usage :theme [name]"
		return m, nil
	}
	return setThemeSetting(m, name), nil
}
