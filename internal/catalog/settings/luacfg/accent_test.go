package luacfg

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
)

func TestLayer_AccentColor(t *testing.T) {
	homeWithConfig(t, "rex.set('accent_color', '#FF6F61')\n", "")
	s, rep, _, err := settings.LoadDefaultReport()
	require.NoError(t, err)
	require.NoError(t, rep.Err)
	require.Equal(t, "#ff6f61", s.Get("accent_color"))
	require.Equal(t, "config.lua:1", s.SetAt("accent_color"))
}

func TestLayer_AccentColorRefused(t *testing.T) {
	homeWithConfig(t, "rex.set('accent_color', 'rgb(300, 0, 0)')\n", "accent_color: mimosa\n")
	s, rep, _, err := settings.LoadDefaultReport()
	require.NoError(t, err)
	require.Equal(t, "mimosa", s.Get("accent_color"), "a refused Lua value leaves config.yaml's")
	all := ""
	if rep.Err != nil {
		all += rep.Err.Error()
	}
	for _, w := range rep.Warnings {
		all += w
	}
	require.Contains(t, all, "out of range 0-255")
}
