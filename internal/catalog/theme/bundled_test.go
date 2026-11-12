package theme

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var bundledNames = []string{"dracula", "pantone_safe", "tokyo_night"}

func TestBundledLoad(t *testing.T) {
	require.Len(t, Bundled, len(bundledNames))
	for _, name := range bundledNames {
		p, ok := Bundled[name]
		require.True(t, ok, name)
		require.Equal(t, name, p.Name)
		require.True(t, p.Dark, name)
		_, clash := Builtin[name]
		require.False(t, clash, "%s is bundled, not a Go built-in", name)
	}
}

// A bundled file is held to more than a user file: it must parse with no
// warning at all (no unknown key, no low-contrast advice).
func TestBundledParseClean(t *testing.T) {
	files, err := bundledFS.ReadDir("builtin")
	require.NoError(t, err)
	require.Len(t, files, len(bundledNames))
	for _, f := range files {
		data, err := bundledFS.ReadFile("builtin/" + f.Name())
		require.NoError(t, err)
		name := strings.TrimSuffix(f.Name(), ".yaml")
		p, warns, err := Parse(name, data)
		require.NoError(t, err, name)
		require.Empty(t, warns, name)
		require.Equal(t, Bundled[name], p, name)
	}
}

func TestGetBundled(t *testing.T) {
	def, _ := Get(DefaultName)
	for _, name := range bundledNames {
		p, ok := Get(name)
		require.True(t, ok, name)
		require.Equal(t, name, p.Name)
		require.NotEqual(t, def.Accent, p.Accent, name)
	}
	p, _ := Get("dracula")
	require.NotEqual(t, def.Bg, p.Bg)
}

func TestGetBundledReturnsIndependentFleet(t *testing.T) {
	p, _ := Get("dracula")
	p.Fleet[0] = "mutated"
	require.NotEqual(t, "mutated", Bundled["dracula"].Fleet[0])
}

// The bundled themes are not bases: extends and the built-in name guard
// still see only the Go built-ins.
func TestBundledIsNotABase(t *testing.T) {
	_, _, err := Parse("mine", []byte("extends: dracula\n"))
	require.ErrorContains(t, err, `"dracula" is not a built-in theme`)
	require.NotContains(t, err.Error(), "tokyo_night", "the list of bases names only the Go built-ins")
	_, _, err = Parse("dracula", []byte("colors: {accent: \"#123456\"}\n"))
	require.NoError(t, err, "a user file may take a bundled name")
}

func writeThemes(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, src := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600))
	}
	return dir
}

func TestUserOverridesBundled(t *testing.T) {
	c := LoadDir(writeThemes(t, map[string]string{"dracula.yaml": "colors: {accent: \"#123456\"}\n"}))
	require.Empty(t, c.Skipped())
	p, ok := c.Get("dracula")
	require.True(t, ok)
	require.Equal(t, "#123456", p.Accent)
	require.True(t, c.IsUser("dracula"))
	q, _ := c.Get("tokyo_night")
	require.Equal(t, Bundled["tokyo_night"].Accent, q.Accent, "the other bundled themes still come from rex")
}

func TestUserNamesExcept(t *testing.T) {
	c := LoadDir(writeThemes(t, map[string]string{
		"dracula.yaml": "colors: {accent: \"#123456\"}\n",
		"mine.yaml":    "",
	}))
	registry := []string{"default", "classic", "noir", "paper", "high-contrast", "ansi", "dracula", "tokyo_night", "pantone_safe"}
	require.Equal(t, []string{"mine"}, c.UserNamesExcept(registry))
	require.Equal(t, []string{"dracula", "mine"}, c.UserNamesExcept(nil))
	var none *Catalog
	require.Nil(t, none.UserNamesExcept(registry))
}

// pantone_safe keeps its state colors apart for people with protan, deutan
// and tritan color vision, the way the U4 edge pool does.
func TestPantoneSafeCVDSeparation(t *testing.T) {
	p := Bundled["pantone_safe"]
	colors := map[string]string{"Working": p.Working, "Needs": p.Needs, "Done": p.Done, "Failed": p.Failed, "Accent": p.Accent}
	names := make([]string, 0, len(colors))
	for n := range colors {
		names = append(names, n)
	}
	for vision := range cvdMatrices {
		for i := range names {
			for j := i + 1; j < len(names); j++ {
				a, b := names[i], names[j]
				require.GreaterOrEqual(t, deltaEOK(colors[a], colors[b], vision), 6.5, "%s vs %s under %s", a, b, vision)
			}
		}
	}
}
