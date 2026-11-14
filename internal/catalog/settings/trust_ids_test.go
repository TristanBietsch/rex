package settings

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsTrustSetting(t *testing.T) {
	require.True(t, IsTrustSetting("skip_permissions"))
	require.True(t, IsTrustSetting("auto_trust_dirs"))
	require.False(t, IsTrustSetting("usage"))
	for _, id := range TrustSettingIDs {
		_, ok := Find(id)
		require.True(t, ok, id)
	}
}

// Every trust setting is denied to a workspace .rex/config.lua: a repo's
// file can never grant or widen trust, whoever trusted the workspace.
func TestWorkspaceDenyIDs_CoverTrust(t *testing.T) {
	for _, id := range TrustSettingIDs {
		require.True(t, WorkspaceDenied(id), id)
	}
	for _, id := range WorkspaceDenyIDs {
		_, ok := Find(id)
		require.True(t, ok, "%s is a setting", id)
	}
	require.False(t, WorkspaceDenied("theme"))
	require.True(t, WorkspaceDenied("default_spawn_model"))
}

// The allow list is exact, each entry with why it is safe for a cloned
// repo to set. Adding a setting here is a security decision.
func TestWorkspaceAllowIDs_Exact(t *testing.T) {
	why := map[string]string{
		"theme": "colors", "accent_color": "colors", "spinner": "animation", "waiting_spinner": "animation", "row_density": "layout",
		"prompt_glyph": "a glyph", "reduce_motion": "animation",
		"selector_glyph": "a glyph", "selector_gap": "layout", "selector_bold": "layout",
		"selector_blink": "animation", "selector_blink_ms": "animation",
		"blinking_enabled": "animation", "show_help_bar": "layout",
		"header_style": "layout", "window_border": "layout",
		"window_border_color": "colors", "column_layout": "layout",
		"mobile_layout": "layout", "mouse_enabled": "input",
		"attach_scrollback_lines": "display buffer",
		"screensaver_enabled":     "display", "screensaver_after_minutes": "display",
		"screensaver_counts": "layout", "screensaver_clock": "display", "screensaver_drift": "animation",
		"board_filter": "display", "search": "display", "timestamps": "display",
		"summary_preview": "display", "waiting_section": "grouping only",
		"usage_meter": "display", "summary_detail": "description length",
		"desc_animation": "animation", "attach_banner": "display",
	}
	require.ElementsMatch(t, keys(why), WorkspaceAllowIDs)
	for _, id := range WorkspaceAllowIDs {
		_, ok := Find(id)
		require.True(t, ok, "%s is a setting", id)
		require.False(t, WorkspaceDenied(id), id)
	}
}

// Every registry setting is either allowed or denied, and the ones that
// must never be allowed are denied.
func TestWorkspaceDenyIDs_EverythingElse(t *testing.T) {
	require.Len(t, WorkspaceDenyIDs, len(Registry)-len(WorkspaceAllowIDs))
	for _, id := range []string{
		"skip_permissions", "auto_trust_dirs", "default_spawn_tool", "default_spawn_model",
		"default_spawn_effort", "extra_agents", "audit_trail", "audit_full_prompt",
		"workspaces", "lua_config", "live_reload", "board_hidden_sections",
		"sound_enabled", "soundset", "master_volume", "notify_on_state_change",
		"retire_after_hours", "turn_snapshots", "loops", "session_export", "usage",
		"cli_error_log", "summary_model", "screensaver_keep_awake",
	} {
		require.True(t, WorkspaceDenied(id), id)
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
