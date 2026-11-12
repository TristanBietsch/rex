package settings

import (
	"path/filepath"
	"slices"
	"strings"
)

// TrustSettingIDs are the settings that grant trust: they decide whether new
// sessions run without the agent's permission and folder-trust prompts. The
// daemon fails them closed when config.yaml does not parse, and rex tells the
// daemon at once when it changes one (so revoking takes effect immediately
// instead of at the next poll).
var TrustSettingIDs = []string{"skip_permissions", "auto_trust_dirs"}

// IsTrustSetting reports whether id is one of TrustSettingIDs.
func IsTrustSetting(id string) bool { return slices.Contains(TrustSettingIDs, id) }

// WorkspaceAllowIDs are the only settings a workspace's .rex/config.lua
// may set, even in a workspace the user trusted (rex ws trust): display
// settings that change how rex looks, never what runs, what is recorded,
// what is heard or what gets deleted. Everything else is denied, so a
// setting added later is denied until it is listed here on purpose.
var WorkspaceAllowIDs = []string{
	"theme", "spinner", "row_density", "prompt_glyph", "reduce_motion",
	"blinking_enabled", "show_help_bar", "header_style", "window_border",
	"window_border_color", "column_layout", "mobile_layout",
	"mouse_enabled", "attach_scrollback_lines",
	"screensaver_enabled", "screensaver_after_minutes",
	"board_filter", "search", "timestamps", "summary_preview",
	"waiting_section", "usage_meter",
	"summary_detail", "desc_animation", "attach_banner",
}

// WorkspaceDenyIDs are the settings a workspace's .rex/config.lua can never
// set: every registry setting not in WorkspaceAllowIDs. Among them the
// trust settings, which agent and model run, the audit trail, sounds, the
// done timer, the features that run commands or write files, and the
// switches that gate .rex/ and config.lua (workspaces, lua_config,
// live_reload). A denied rex.set is a warning and leaves the value alone
// (it does not fail closed).
var WorkspaceDenyIDs = func() []string {
	var out []string
	for _, s := range Registry {
		if !slices.Contains(WorkspaceAllowIDs, s.ID) {
			out = append(out, s.ID)
		}
	}
	return out
}()

// WorkspaceDenied reports whether id is one of WorkspaceDenyIDs.
func WorkspaceDenied(id string) bool { return slices.Contains(WorkspaceDenyIDs, id) }

// WorkspaceSetAt reports whether at, a config.lua position from
// Store.SetAt, is in a workspace's .rex/config.lua rather than the user's
// own config.lua or its modules.
func WorkspaceSetAt(at string) bool {
	sep := string(filepath.Separator)
	return strings.Contains(at, sep+".rex"+sep+"config.lua:")
}
