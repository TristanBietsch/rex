package settings

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/theme"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
)

func installLayer(t *testing.T, rep LayerReport) *int {
	t.Helper()
	calls := new(int)
	SetLayer(func() LayerReport { *calls++; return rep })
	t.Cleanup(func() { SetLayer(nil) })
	return calls
}

func writeYAML(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	return p
}

func TestLoadLayered_PrecedenceDefaultYamlLua(t *testing.T) {
	path := writeYAML(t, "theme: paper\nspinner: moon\n")
	installLayer(t, LayerReport{
		Exists: true,
		Values: map[string]any{"theme": "noir"},
		SetAt:  map[string]string{"theme": "config.lua:2"},
	})
	s, rep, err := LoadLayered(path)
	require.NoError(t, err)
	require.True(t, rep.Exists)
	require.Equal(t, "noir", s.Get("theme"), "lua beats yaml")
	require.Equal(t, SourceLua, s.Source("theme"))
	require.Equal(t, "config.lua:2", s.SetAt("theme"))
	require.True(t, s.Locked("theme"))
	require.Equal(t, "moon", s.Get("spinner"), "yaml beats default")
	require.Equal(t, SourceFile, s.Source("spinner"))
	require.Equal(t, SourceDefault, s.Source("row_density"))
	require.Equal(t, "", s.SetAt("spinner"))
	require.False(t, s.Locked("spinner"))
}

func TestLoadLayered_NoLayerIsYAMLOnly(t *testing.T) {
	SetLayer(nil)
	path := writeYAML(t, "theme: paper\n")
	s, rep, err := LoadLayered(path)
	require.NoError(t, err)
	require.False(t, rep.Exists)
	require.Equal(t, "paper", s.Get("theme"))
}

func TestLoadLayered_YAMLErrorStillAppliesLayer(t *testing.T) {
	path := writeYAML(t, "theme: [unclosed")
	installLayer(t, LayerReport{Values: map[string]any{"spinner": "moon"}, SetAt: map[string]string{"spinner": "config.lua:1"}})
	s, _, err := LoadLayered(path)
	require.Error(t, err)
	require.Equal(t, "moon", s.Get("spinner"))
}

func TestLuaConfigOff_IsInert(t *testing.T) {
	path := writeYAML(t, "lua_config: false\n")
	calls := installLayer(t, LayerReport{Values: map[string]any{"theme": "noir"}, SetAt: map[string]string{"theme": "config.lua:1"}})
	s, rep, err := LoadLayered(path)
	require.NoError(t, err)
	require.Zero(t, *calls, "the layer is never evaluated")
	require.False(t, rep.Exists)
	require.Equal(t, "default", s.Get("theme"))
	require.False(t, s.Locked("theme"))
	require.NoError(t, s.Set("theme", "paper"), "rows are unlocked")
}

func TestLuaConfigDefaultsOn(t *testing.T) {
	calls := installLayer(t, LayerReport{})
	_, _, err := LoadLayered(filepath.Join(t.TempDir(), "missing.yaml"))
	require.NoError(t, err)
	require.Equal(t, 1, *calls)
	require.True(t, LuaConfigOn(NewStore()))
}

func TestOverlayNeverPersisted(t *testing.T) {
	path := writeYAML(t, "spinner: moon\n")
	installLayer(t, LayerReport{Values: map[string]any{"theme": "noir"}, SetAt: map[string]string{"theme": "config.lua:1"}})
	s, _, err := LoadLayered(path)
	require.NoError(t, err)
	require.NoError(t, s.Set("row_density", "compact"))
	require.NoError(t, s.Save(path))
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "theme")
	require.Contains(t, string(raw), "row_density: compact")
	require.Contains(t, string(raw), "spinner: moon")
}

func TestSetAndResetRefuseLuaRows(t *testing.T) {
	path := writeYAML(t, "")
	installLayer(t, LayerReport{Values: map[string]any{"theme": "noir"}, SetAt: map[string]string{"theme": "lua/ui.lua:7"}})
	s, _, err := LoadLayered(path)
	require.NoError(t, err)

	err = s.Set("theme", "paper")
	var managed *ErrManagedByLua
	require.True(t, errors.As(err, &managed))
	require.Equal(t, "theme", managed.ID)
	require.Equal(t, "lua/ui.lua:7", managed.At)
	require.EqualError(t, err, "theme is set in lua/ui.lua:7; edit it there (rex config edit -lua)")
	require.ErrorAs(t, s.Reset("theme"), &managed)
	require.Equal(t, "noir", s.Get("theme"))
	require.Empty(t, s.dirty)
}

func TestWithDefaultsCopiesLuaSources(t *testing.T) {
	path := writeYAML(t, "")
	installLayer(t, LayerReport{
		Values: map[string]any{"theme": "noir", "spinner": "moon"},
		SetAt:  map[string]string{"theme": "config.lua:1", "spinner": "config.lua:2"},
	})
	s, _, err := LoadLayered(path)
	require.NoError(t, err)
	c := s.WithDefaults("spinner")
	require.True(t, c.Locked("theme"))
	require.Equal(t, "config.lua:1", c.SetAt("theme"))
	require.False(t, c.Locked("spinner"))
	require.Equal(t, "braille", c.Get("spinner"))
	require.True(t, s.Locked("spinner"), "the original is unchanged")
}

func TestLuaErrorKeepsLastGoodButFailsTrustClosed(t *testing.T) {
	path := writeYAML(t, "auto_trust_dirs: ~/dev\nskip_permissions: true\nspinner: moon\n")
	installLayer(t, LayerReport{
		Exists: true, Err: errors.New("config.lua:3: boom"), LastGood: true,
		Values: map[string]any{"theme": "noir", "auto_trust_dirs": "~/lua"},
		SetAt:  map[string]string{"theme": "config.lua:1", "auto_trust_dirs": "config.lua:2"},
	})
	s, rep, err := LoadLayered(path)
	require.NoError(t, err)
	require.Error(t, rep.Err)
	require.Equal(t, "noir", s.Get("theme"), "last good values stay")
	require.Equal(t, "moon", s.Get("spinner"))
	require.Equal(t, "", s.Get("auto_trust_dirs"), "trust ids read their defaults, not yaml and not last good")
	require.Equal(t, false, s.Get("skip_permissions"))
	require.Equal(t, SourceDefault, s.Source("auto_trust_dirs"))
	require.False(t, s.Locked("auto_trust_dirs"))
}

func TestLuaErrorWithNoLastGoodStillFailsTrustClosed(t *testing.T) {
	path := writeYAML(t, "auto_trust_dirs: ~/dev\ntheme: paper\n")
	installLayer(t, LayerReport{Exists: true, Err: errors.New("syntax error")})
	s, _, err := LoadLayered(path)
	require.NoError(t, err)
	require.Equal(t, "paper", s.Get("theme"), "yaml and defaults apply")
	require.Equal(t, "", s.Get("auto_trust_dirs"))
}

func TestLoadDefaultReportUsesTheLayer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cfg, err := paths.ConfigFile()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o700))
	require.NoError(t, os.WriteFile(cfg, []byte("spinner: moon\n"), 0o600))
	installLayer(t, LayerReport{Exists: true, Values: map[string]any{"theme": "noir"}, SetAt: map[string]string{"theme": "config.lua:1"}})

	s, rep, path, err := LoadDefaultReport()
	require.NoError(t, err)
	require.Equal(t, cfg, path)
	require.True(t, rep.Exists)
	require.Equal(t, "noir", s.Get("theme"))
	s2, p2, err := LoadDefault()
	require.NoError(t, err)
	require.Equal(t, cfg, p2)
	require.Equal(t, "noir", s2.Get("theme"))

	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	s3, _, p3, err := LoadDefaultReport()
	require.Error(t, err)
	require.Empty(t, p3)
	require.NotNil(t, s3)
}

func TestSetLayerNilClears(t *testing.T) {
	calls := installLayer(t, LayerReport{})
	SetLayer(nil)
	_, _, err := LoadLayered(filepath.Join(t.TempDir(), "x.yaml"))
	require.NoError(t, err)
	require.Zero(t, *calls)
}

func TestLayerStamp(t *testing.T) {
	SetLayer(nil)
	require.Equal(t, "", LayerStamp(true), "no layer")
	SetLayer(func() LayerReport { return LayerReport{} })
	t.Cleanup(func() { SetLayer(nil) })
	require.Equal(t, "", LayerStamp(true), "no stamp")
	n := 0
	SetLayerStamped(func() LayerReport { return LayerReport{} }, func() string { n++; return "s1" })
	require.Equal(t, "s1", LayerStamp(true))
	require.Equal(t, "", LayerStamp(false))
	require.Equal(t, 1, n, "off: the stamp is never taken")
}

func TestStackLayers_UpperWins(t *testing.T) {
	lower := func() LayerReport {
		return LayerReport{Exists: true,
			Values: map[string]any{"theme": "noir", "spinner": "moon"}, SetAt: map[string]string{"theme": "config.lua:1", "spinner": "config.lua:2"},
			Themes: map[string]theme.Palette{"a": {Name: "a"}, "b": {Name: "b", Accent: "#111111"}}, Warnings: []string{"w1"}}
	}
	upper := func() LayerReport {
		return LayerReport{Exists: true,
			Values: map[string]any{"theme": "paper"}, SetAt: map[string]string{"theme": ".rex/config.lua:4"},
			Themes: map[string]theme.Palette{"b": {Name: "b", Accent: "#222222"}}, Warnings: []string{"w2"},
			ThemeIssues: []theme.Issue{{File: ".rex/config.lua:5", Msg: "x"}}}
	}
	rep := StackLayers(lower, upper)()
	require.True(t, rep.Exists)
	require.Equal(t, "paper", rep.Values["theme"])
	require.Equal(t, ".rex/config.lua:4", rep.SetAt["theme"])
	require.Equal(t, "moon", rep.Values["spinner"])
	require.Equal(t, "config.lua:2", rep.SetAt["spinner"])
	require.Equal(t, "#222222", rep.Themes["b"].Accent)
	require.Contains(t, rep.Themes, "a")
	require.Equal(t, []string{"w1", "w2"}, rep.Warnings)
	require.Len(t, rep.ThemeIssues, 1)
	require.NoError(t, rep.Err)
	require.Equal(t, "noir", lower().Values["theme"], "inputs are not changed")

	broken := func() LayerReport { return LayerReport{Exists: true, Err: errors.New("boom"), LastGood: true} }
	rep = StackLayers(lower, broken)()
	require.ErrorContains(t, rep.Err, "boom", "any failure fails the stack: trust settings fail closed")
	require.True(t, rep.LastGood)
	require.Equal(t, "noir", rep.Values["theme"])

	require.Equal(t, LayerReport{}, StackLayers()())
}

func TestStackStamps(t *testing.T) {
	a := func() string { return "a" }
	b := func() string { return "b" }
	require.Equal(t, "a|b", StackStamps(a, b)())
	require.Equal(t, "a|", StackStamps(a, nil)())
}

// A layer that refused a trust value closes that setting: config.yaml's
// value does not survive the user's failed attempt to change it.
func TestLoadLayered_FailClosedIDsReadDefaults(t *testing.T) {
	path := writeYAML(t, "skip_permissions: true\nauto_trust_dirs: ~/dev\n")
	installLayer(t, LayerReport{Exists: true, FailClosed: []string{"skip_permissions"}, Warnings: []string{"w"}})
	s, _, err := LoadLayered(path)
	require.NoError(t, err)
	require.Equal(t, false, s.Get("skip_permissions"))
	require.Equal(t, SourceDefault, s.Source("skip_permissions"))
	require.Equal(t, "~/dev", s.Get("auto_trust_dirs"), "only the refused id closes")
}

func TestStackLayers_FailClosedFollowsLaterLayers(t *testing.T) {
	user := func() LayerReport {
		return LayerReport{Exists: true, Values: map[string]any{"auto_trust_dirs": "~/dev"}, SetAt: map[string]string{"auto_trust_dirs": "config.lua:1"},
			FailClosed: []string{"skip_permissions"}}
	}
	ws := func() LayerReport {
		return LayerReport{Exists: true, Values: map[string]any{"skip_permissions": false}, SetAt: map[string]string{"skip_permissions": ".rex/config.lua:1"},
			FailClosed: []string{"auto_trust_dirs"}}
	}
	rep := StackLayers(user, ws)()
	require.Equal(t, []string{"auto_trust_dirs"}, rep.FailClosed)
	require.NotContains(t, rep.Values, "auto_trust_dirs", "a later refusal closes an earlier layer's value")
	require.Equal(t, false, rep.Values["skip_permissions"], "a later valid value reopens")
}
