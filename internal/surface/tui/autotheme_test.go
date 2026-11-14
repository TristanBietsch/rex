package tui

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/catalog/theme"
)

func TestColorFGBGDark(t *testing.T) {
	for _, tc := range []struct {
		v          string
		dark, sure bool
	}{
		{"15;0", true, true},
		{"0;15", false, true},
		{"0;7", false, true},
		{"7;8", true, true},
		{"12;default;0", true, true},
		{"0;default;15", false, true},
		{"15;4", true, true},
		{"0;11", false, true},
		{"", false, false},
		{"15", false, false},
		{"15;default", false, false},
		{"15;x", false, false},
		{"15;16", false, false},
		{"15;-1", false, false},
	} {
		dark, sure := colorFGBGDark(tc.v)
		require.Equal(t, tc.sure, sure, tc.v)
		if sure {
			require.Equal(t, tc.dark, dark, tc.v)
		}
	}
}

func TestTermLightFor(t *testing.T) {
	env := func(v string) func(string) string {
		return func(k string) string {
			if k == "COLORFGBG" {
				return v
			}
			return ""
		}
	}
	queries := 0
	query := func(dark bool) func() bool {
		return func() bool { queries++; return dark }
	}
	store := func(name string) *settings.Store {
		s := settings.NewStore()
		require.NoError(t, s.Set("theme", name))
		return s
	}

	// Not auto: the terminal is never asked, whatever it would say.
	require.False(t, termLightFor(store("paper"), env("0;15"), query(false)))
	require.False(t, termLightFor(settings.NewStore(), env(""), query(false)))
	require.False(t, termLightFor(nil, env(""), query(false)))
	require.Zero(t, queries)

	// auto: COLORFGBG first, and the OSC 11 query only when it says nothing.
	require.True(t, termLightFor(store(theme.AutoName), env("0;15"), query(true)))
	require.False(t, termLightFor(store(theme.AutoName), env("15;0"), query(false)))
	require.Zero(t, queries)
	require.True(t, termLightFor(store(theme.AutoName), env(""), query(false)))
	require.False(t, termLightFor(store(theme.AutoName), env("15;default"), query(true)))
	require.Equal(t, 2, queries)
}

func TestBootThemeApplyAutoNamesWhatItPicked(t *testing.T) {
	m := themeModel(t)
	m.Focus = FocusBoot
	m.termLight = true
	next, _ := m.Update(themeApplyMsg{Scheme: theme.AutoName})
	got := next.(Model)
	require.NotEmpty(t, got.BootLog)
	step := got.BootLog[len(got.BootLog)-1]
	require.Equal(t, stepOK, step.Status)
	require.Equal(t, "auto (paper)", step.Desc)
}
