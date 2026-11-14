package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/catalog/theme"
)

// themeOptions are the names the theme setting can cycle through: the
// built-in and bundled themes in registry order, then the user's other theme
// files by name. A file that overrides a bundled theme keeps its slot.
func themeOptions(m Model) []string {
	s, _ := settings.Find("theme")
	return append(slices.Clone(s.Options), m.Themes.UserNamesExcept(s.Options)...)
}

// applyThemeNamed applies the named theme with the stored accent_color. An
// unknown name uses the default palette and puts a notice on the status
// line.
func applyThemeNamed(m *Model, name string) {
	syncAccent(*m)
	if !applyThemeFrom(m.Themes, resolveThemeName(*m, name)) {
		m.Err = themeNotFound(m.Themes, name)
	}
}

// syncAccent makes the stored accent_color the one applyPalette uses.
func syncAccent(m Model) {
	accentChoice = ""
	if m.Store != nil {
		accentChoice = m.Store.String("accent_color")
	}
}

// reapplyTheme applies the current theme setting again with accentChoice as
// it is (the settings editor's live preview).
func reapplyTheme(m Model) {
	name := theme.DefaultName
	if m.Store != nil {
		name = m.Store.String("theme")
	}
	applyThemeFrom(m.Themes, resolveThemeName(m, name))
}

// accentNotice is what the last theme apply has to say about accent_color:
// a value that could not be used, a color adjusted for contrast, or one
// close to a state color. "" when there is nothing to say.
func accentNotice(themeName string) (msg string, isErr bool) {
	if accentErr != nil {
		return "accent_color: " + accentErr.Error() + " (using the theme's accent)", true
	}
	return accentLast.Note(themeName), false
}

// noteAccent puts accentNotice on the status line.
func noteAccent(m *Model) {
	name := theme.DefaultName
	if m.Store != nil {
		name = resolveThemeName(*m, m.Store.String("theme"))
	}
	if msg, isErr := accentNotice(name); isErr {
		m.Err = msg
	} else if msg != "" {
		m.Notice = msg
	}
}

// setAccentSetting stores, applies and saves an accent_color value.
func setAccentSetting(m Model, value string) Model {
	if settingLocked(&m, "accent_color") {
		return m
	}
	if err := m.Store.Set("accent_color", value); err != nil {
		m.Err = "accent: " + err.Error()
		return m
	}
	applyLive(&m, "accent_color", m.Store.Get("accent_color"))
	if err := m.Store.Save(m.StorePath); err != nil {
		m.Err = "accent: save: " + err.Error()
	}
	return m
}

// runAccent is `:accent [value]`: set accent_color (any form the setting
// takes, "theme" for the theme's own), or with no value step to the next
// named color.
func runAccent(m Model, c cmdCall) (Model, tea.Cmd) {
	if m.Store == nil {
		m.Err = "accent: no settings loaded"
		return m, nil
	}
	if len(c.Args) == 0 {
		s, _ := settings.Find("accent_color")
		return setAccentSetting(m, cycleOption(s.Options, m.Store.String("accent_color"), +1)), nil
	}
	return setAccentSetting(m, strings.Join(c.Args, " ")), nil
}

// themeNotFound is the notice for a theme that cannot be applied: its file
// was skipped (with the reason), or no such theme exists.
func themeNotFound(cat *theme.Catalog, name string) string {
	if why := cat.WhySkipped(name); why != "" {
		return fmt.Sprintf("theme %q not loaded: %s (using default)", name, why)
	}
	return fmt.Sprintf("theme %q not found (using default)", name)
}

// themesLoadedStep is the boot-log line for the theme files: how many loaded,
// and the first reason for any that were skipped.
func themesLoadedStep(msg themesLoadedMsg) bootStepMsg {
	step := bootStepMsg{Name: "themes.load", Dur: msg.Dur}
	names := msg.Themes.UserNames()
	skipped := msg.Themes.Skipped()
	switch {
	case len(skipped) > 0:
		step.Status = stepWarn
		step.Desc = fmt.Sprintf("%d loaded, %d skipped: %s", len(names), len(skipped), skipped[0])
	case len(names) > 0:
		step.Status = stepOK
		step.Desc = fmt.Sprintf("%d user theme(s): %s", len(names), strings.Join(names, ", "))
	default:
		step.Status = stepSkip
		step.Desc = "no user themes"
	}
	return step
}

// setThemeSetting stores, applies and saves a theme choice.
func setThemeSetting(m Model, name string) Model {
	if settingLocked(&m, "theme") {
		return m
	}
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
