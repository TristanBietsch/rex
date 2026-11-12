package theme

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCatalog_WithLua(t *testing.T) {
	dir := t.TempDir()
	writeTheme(t, dir, "shared.yaml", "colors: {accent: '#B392F0'}")
	writeTheme(t, dir, "filey.yaml", "colors: {accent: '#B392F0'}")
	file := LoadDir(dir)

	lua := map[string]Palette{
		"shared": {Name: "shared", Accent: "#AAAAAA"},
		"mine":   {Name: "mine", Accent: "#BBBBBB"},
	}
	c := file.WithLua(lua, []Issue{{File: "config.lua:9", Msg: "bad \x1b[31m thing"}, {File: "config.lua:3", Msg: "note", Warn: true}})

	p, ok := c.Get("shared")
	require.True(t, ok)
	require.Equal(t, "#AAAAAA", p.Accent, "a Lua theme beats a file theme of the same name")
	require.True(t, c.IsLua("shared"))
	require.False(t, c.IsUser("shared"))
	require.True(t, c.IsUser("filey"))
	require.False(t, c.IsLua("filey"))
	require.Equal(t, []string{"filey", "mine", "shared"}, c.UserNames())
	require.Contains(t, issueFor(c, "shared.yaml"), "config.lua wins")
	require.Len(t, c.Skipped(), 1)
	require.NotContains(t, c.Skipped()[0].Msg, "\x1b")

	// The receiver is untouched.
	p, _ = file.Get("shared")
	require.Equal(t, "#B392F0", p.Accent)
	require.False(t, file.IsLua("shared"))
	require.Len(t, file.Issues, 0)
}

func TestCatalog_WithLuaOnNilCatalog(t *testing.T) {
	var c *Catalog
	got := c.WithLua(map[string]Palette{"x": {Name: "x"}}, nil)
	require.Equal(t, []string{"x"}, got.UserNames())
	require.True(t, got.IsLua("x"))
	require.False(t, c.IsLua("x"))
	require.Empty(t, got.Issues)
}
