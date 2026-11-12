package setup

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
	"github.com/tristanbietsch/rex/internal/workspace/wsconfig"
)

// checkWorkspaceConfig reports the .rex/config.lua of the workspace around
// the current folder: not trusted (not read), failed (trust settings fail
// closed), refused calls, or which settings it set and where. No row
// outside a workspace or with workspaces (or lua_config) off. All text is
// flattened.
func checkWorkspaceConfig() []checkResult {
	const name = "workspace config.lua"
	if s, _, err := settings.LoadDefault(); err == nil && !settings.LuaConfigOn(s) {
		return nil
	}
	info := wsconfig.Current()
	file := protocol.FlattenDisplayText(info.File)
	rep := info.Report
	switch info.State {
	case wsconfig.StateOff:
		return nil
	case wsconfig.StateNone:
		if info.Root == "" {
			return nil
		}
		return []checkResult{{name, checkInfo, "none (" + file + ")"}}
	case wsconfig.StateUnsafe:
		return []checkResult{{name, checkWarn, info.Detail + "; not applied"}}
	case wsconfig.StateUntrusted:
		return []checkResult{{name, checkWarn, file + ": not trusted, not read (read it, then rex ws trust " + protocol.FlattenDisplayText(info.Root) + ")"}}
	case wsconfig.StateFailed:
		return []checkResult{{name, checkWarn, protocol.FlattenDisplayText(rep.Err.Error()) +
			"; trust settings read their defaults" + luaKeptSuffix(rep)}}
	}
	if len(rep.Warnings) > 0 {
		return []checkResult{{name, checkWarn, fmt.Sprintf("%s: %d warning(s), first: %s", file, len(rep.Warnings), rep.Warnings[0])}}
	}
	if len(rep.ThemeIssues) > 0 {
		return []checkResult{{name, checkWarn, fmt.Sprintf("%s: %d theme issue(s), first: %s", file, len(rep.ThemeIssues), rep.ThemeIssues[0])}}
	}
	ids := make([]string, 0, len(rep.Values))
	for id := range rep.Values {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, fmt.Sprintf("%s (%s)", id, rep.SetAt[id]))
	}
	detail := fmt.Sprintf("%s: %d setting(s), %d theme(s)", file, len(rep.Values), len(rep.Themes))
	if len(parts) > 0 {
		detail += ": " + strings.Join(parts, ", ")
	}
	return []checkResult{{name, checkPass, detail}}
}
