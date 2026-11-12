package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/catalog/theme"
)

// settingsRow returns the rendered settings line that holds label.
func settingsRow(t *testing.T, view, label string) string {
	t.Helper()
	for _, l := range strings.Split(view, "\n") {
		if strings.Contains(ansi.Strip(l), label) {
			return l
		}
	}
	t.Fatalf("no row for %q", label)
	return ""
}

// valueEnd is the cell column just past the last non-space, non-border cell.
func valueEnd(line string) int {
	s := strings.TrimRight(ansi.Strip(line), " │")
	return ansi.StringWidth(s)
}

// Values sit flush right in the list, on and off alike, at every width and
// on the selected row. Under 40 columns a value has its own line, which ends
// at the same column for every row.
func TestSettingsValuesRightAligned(t *testing.T) {
	for _, w := range []int{30, 50, 80, 120} {
		for _, cur := range []string{"theme", "reduce_motion", "blinking_enabled"} {
			m := Model{Store: settings.NewStore(), Settings: &SettingsState{CursorID: cur}}
			lines := strings.Split(renderSettings(m, w), "\n")
			ends := map[string]int{}
			for i, l := range lines {
				for _, lbl := range []string{"Spinner type", "Reduce motion", "Done blink", "Row density", "Theme"} {
					if !strings.Contains(ansi.Strip(l), lbl) {
						continue
					}
					if w < 40 {
						l = lines[i+1]
					}
					ends[lbl] = valueEnd(l)
				}
			}
			require.Len(t, ends, 5, "w=%d", w)
			for lbl, e := range ends {
				require.Equal(t, ends["Spinner type"], e, "w=%d cur=%s %s", w, cur, lbl)
			}
		}
	}
}

// On is drawn in the primary tone and off in the dim tone, in every theme,
// both selected and not.
func TestSettingsOnOffColors(t *testing.T) {
	t.Cleanup(func() { applyTheme("default") })
	for _, name := range theme.Names() {
		applyTheme(name)
		withProfile(termenv.TrueColor, func() {
			m := Model{Store: settings.NewStore(), Settings: &SettingsState{CursorID: "theme"}}
			view := renderSettings(m, 100)
			require.Contains(t, settingsRow(t, view, "Done blink"), settingOnStyle(false).Render("on"), name)
			require.Contains(t, settingsRow(t, view, "Reduce motion"), settingOffStyle(false).Render("off"), name)
			require.NotEqual(t, settingOnStyle(false).Render("x"), settingOffStyle(false).Render("x"), name)

			m.Settings.CursorID = "blinking_enabled"
			view = renderSettings(m, 100)
			require.Contains(t, settingsRow(t, view, "Done blink"), settingOnStyle(true).Render("on"), name)
			m.Settings.CursorID = "reduce_motion"
			view = renderSettings(m, 100)
			require.Contains(t, settingsRow(t, view, "Reduce motion"), settingOffStyle(true).Render("off"), name)
		})
	}
}
