package setup

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
)

// Lua cannot tell 1 from 1.0, so an old `master_volume = 1.0` (full) now
// reads as 1/50. rex doctor warns, with the file:line, for any number from
// just above 0 to 1; 0 (mute on both scales) and the new scale stay quiet.
func TestDoctor_WarnsOnLuaVolumeAtOrBelowOne(t *testing.T) {
	for _, in := range []string{"1", "1.0", "0.8", "0.01"} {
		luaConfigHome(t, "rex.set('theme', 'noir')\nrex.set('master_volume', "+in+")", "")
		rows := checkLuaVolume()
		require.Len(t, rows, 1, in)
		require.Equal(t, checkWarn, rows[0].Status, in)
		require.Equal(t, "master_volume", rows[0].Name, in)
		require.Contains(t, rows[0].Detail, "config.lua:2 sets master_volume = ", in)
		require.Contains(t, rows[0].Detail, "the scale is 0–50 now (50 = max)", in)
	}

	luaConfigHome(t, "rex.set('master_volume', 1.0)", "")
	require.Contains(t, checkLuaVolume()[0].Detail, "config.lua:1 sets master_volume = 1: the scale is 0–50 now (50 = max)")
	require.Contains(t, checkLuaVolume()[0].Detail, "1/50")

	luaConfigHome(t, "rex.set('master_volume', 0.8)", "")
	require.Contains(t, checkLuaVolume()[0].Detail, "sets master_volume = 0.8: the scale is 0–50 now (50 = max)")
	require.Contains(t, checkLuaVolume()[0].Detail, "read as 40")

	for _, in := range []string{"0", "0.0", "2", "40", "50"} {
		luaConfigHome(t, "rex.set('master_volume', "+in+")", "")
		require.Empty(t, checkLuaVolume(), in)
	}

	// Off, or not set from Lua: nothing.
	luaConfigHome(t, "rex.set('master_volume', 1)", "lua_config: false\n")
	require.Empty(t, checkLuaVolume())
	luaConfigHome(t, "rex.set('theme', 'noir')", "master_volume: 1\n")
	require.Empty(t, checkLuaVolume())
}

// The check reads the report's Given and SetAt, so a later layer (a
// workspace .rex/config.lua) is named by its own position, and a later
// whole number replaces an earlier layer's number.
func TestLuaVolumeRows_FromReport(t *testing.T) {
	rep := settings.LayerReport{
		Values: map[string]any{"master_volume": 1},
		SetAt:  map[string]string{"master_volume": "/w/.rex/config.lua:3"},
		Given:  map[string]float64{"master_volume": 1},
	}
	rows := luaVolumeRows(rep)
	require.Len(t, rows, 1)
	require.Contains(t, rows[0].Detail, "/w/.rex/config.lua:3 sets master_volume = 1")

	stacked := settings.StackLayers(
		func() settings.LayerReport { return rep },
		func() settings.LayerReport {
			return settings.LayerReport{
				Values: map[string]any{"master_volume": 40},
				SetAt:  map[string]string{"master_volume": "config.lua:9"},
				Given:  map[string]float64{"master_volume": 40},
			}
		},
	)()
	require.Empty(t, luaVolumeRows(stacked))
}
