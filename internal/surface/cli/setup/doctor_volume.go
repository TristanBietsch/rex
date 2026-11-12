package setup

import (
	"fmt"
	"strconv"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/workspace/wsconfig"
)

// checkLuaVolume warns when config.lua (or a trusted workspace
// .rex/config.lua) sets master_volume to a number above 0 and at most 1.
// Lua cannot tell 1 from 1.0, so an old `master_volume = 1.0` (full) now
// reads as 1 of 50, nearly silent; a fraction (0.8) still converts but is
// the old scale. 0 is left alone: it is mute on both scales. No row when
// Lua is off or the value is not set from Lua.
func checkLuaVolume() []checkResult {
	s, rep, _, _ := settings.LoadDefaultReport()
	if s == nil || !settings.LuaConfigOn(s) {
		return nil
	}
	rows := luaVolumeRows(rep)
	if info := wsconfig.Current(); info.State == wsconfig.StateApplied {
		rows = append(rows, luaVolumeRows(info.Report)...)
	}
	return rows
}

// luaVolumeRows is checkLuaVolume for one layer report: a warning naming
// the file:line (rep.SetAt) when the Lua number given for master_volume is
// in (0, 1].
func luaVolumeRows(rep settings.LayerReport) []checkResult {
	const id = "master_volume"
	g, ok := rep.Given[id]
	if _, set := rep.Values[id]; !ok || !set || g <= 0 || g > 1 {
		return nil
	}
	num := strconv.FormatFloat(g, 'g', -1, 64)
	detail := fmt.Sprintf("%s sets %s = %s: the scale is 0–%d now (%d = max)", rep.SetAt[id], id, num, settings.VolumeMax, settings.VolumeMax)
	if g == 1 {
		detail += fmt.Sprintf(", so this is 1/%d, nearly silent; write %d for full volume", settings.VolumeMax, settings.VolumeMax)
	} else {
		detail += fmt.Sprintf("; read as %v, write that instead", rep.Values[id])
	}
	return []checkResult{{id, checkWarn, detail}}
}
