package settings

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Every setting says when a change takes effect.
func TestEverySettingHasApplyMode(t *testing.T) {
	for _, s := range Registry {
		require.Contains(t, ApplyModes, s.Apply, "%s: Apply %q is not a known mode", s.ID, s.Apply)
	}
}

func TestApplyModeLabelsAreDistinct(t *testing.T) {
	seen := map[string]ApplyMode{}
	for _, m := range ApplyModes {
		l := m.Label()
		require.NotEmpty(t, l, m)
		require.NotContains(t, seen, l)
		seen[l] = m
	}
	require.Equal(t, "applies now", ApplyLive.Label())
	require.Equal(t, "next attach", ApplyNextAttach.Label())
	require.Equal(t, "next new session", ApplyNextSpawn.Label())
	require.Equal(t, "after a daemon restart", ApplyDaemonRestart.Label())
	require.Empty(t, ApplyMode("").Label())
}

// applyModeOf pins every setting's mode to how the setting is really read.
// A new setting must be added here, so its label is a deliberate choice.
var applyModeOf = map[string]ApplyMode{
	// Appearance and the TUI's own behavior: read on every frame or applied
	// by applyLive.
	"theme": ApplyLive, "spinner": ApplyLive, "waiting_spinner": ApplyLive, "row_density": ApplyLive, "prompt_glyph": ApplyLive,
	"reduce_motion": ApplyLive, "blinking_enabled": ApplyLive, "show_help_bar": ApplyLive,
	"header_style": ApplyLive, "window_border": ApplyLive, "window_border_color": ApplyLive, "column_layout": ApplyLive, "mobile_layout": ApplyLive,
	"row_icon_set": ApplyLive, "ascii_glyphs": ApplyLive, "row_icons": ApplyLive,
	"keybind_preset": ApplyLive, "mouse_enabled": ApplyLive, "retire_after_hours": ApplyLive,
	"screensaver_enabled": ApplyLive, "screensaver_after_minutes": ApplyLive, "screensaver_keep_awake": ApplyLive,
	"screensaver_counts": ApplyLive, "screensaver_clock": ApplyLive, "screensaver_drift": ApplyLive,
	"board_hidden_sections": ApplyLive, // applyLive re-reads it into the board
	"desc_animation":        ApplyLive,
	"sound_enabled":         ApplyLive, "soundset": ApplyLive, "master_volume": ApplyLive,
	"notify_on_state_change": ApplyLive, "boot_voice": ApplyLive,
	// The cap is set live by the TUI (SetMaxConcurrent); the daemon never
	// reads it from config.yaml, only from its -max-concurrent-sessions flag.
	"max_concurrent_sessions": ApplyFromTUI,
	// Read when an attach popup opens or resizes.
	"attach_banner": ApplyNextAttach, "attach_scrollback_lines": ApplyNextAttach,
	// Read at each spawn.
	"default_spawn_tool": ApplyNextSpawn, "default_spawn_model": ApplyNextSpawn, "default_spawn_effort": ApplyNextSpawn,
	"auto_trust_dirs": ApplyNextSpawn, "skip_permissions": ApplyNextSpawn,
	"resume": ApplyNextSpawn, "spawn_skills": ApplyNextSpawn, "usage_statusline_tap": ApplyNextSpawn,
	// Features the TUI or CLI reads live, or the daemon applies through its
	// SettingsHub subscribers (cmd/rex-daemon/main.go).
	"board_filter": ApplyLive, "search": ApplyLive, "undo_complete": ApplyLive, "summary_preview": ApplyLive, "summary_schedule": ApplyLive,
	"waiting_section": ApplyLive, "timestamps": ApplyLive, "cli_agent_auto": ApplyLive, "session_export": ApplyLive,
	"fullscreen_pages": ApplyLive,                                               // the TUI picks the renderer on every frame
	"new_agent_page":   ApplyLive,                                               // the TUI picks the page or the wizard on every n
	"turn_snapshots":   ApplyLive, "usage": ApplyLive, "usage_meter": ApplyLive, // the TUI reads usage_meter on every frame
	"live_reload": ApplyLive, "loops": ApplyLive, "auto_swap": ApplyLive, // the TUI reads it on every binary check
	"extra_agents": ApplyLive, "model_autodetect": ApplyLive, // catalog applier subscribes to both
	"workspaces": ApplyLive, "audit_trail": ApplyLive, "audit_full_prompt": ApplyLive, "lua_config": ApplyLive,
	"summary_detail": ApplyLive, "overview_enabled": ApplyLive, "cli_error_log": ApplyLive,
	// Read once when the daemon starts.
	"summary_enabled": ApplyDaemonRestart, "summary_model": ApplyDaemonRestart, "lua_config_path": ApplyDaemonRestart,
}

func TestApplyModesMatchHowSettingsAreRead(t *testing.T) {
	for _, s := range Registry {
		want, ok := applyModeOf[s.ID]
		require.True(t, ok, "%s: add it to applyModeOf with the mode that is true", s.ID)
		require.Equal(t, want, s.Apply, s.ID)
	}
	for id := range applyModeOf {
		_, ok := Find(id)
		require.True(t, ok, "applyModeOf names unknown setting %s", id)
	}
}

func TestMaxConcurrentHelpIsTruthful(t *testing.T) {
	s, _ := Find("max_concurrent_sessions")
	require.NotContains(t, s.Help, "next daemon start")
	require.Contains(t, s.Help, "-max-concurrent-sessions")
	require.Contains(t, s.Help, "TUI")
	require.Equal(t, "applies now (set from the TUI)", ApplyFromTUI.Label())
}

// keybind_preset has one option today, so the TUI hides it; it stays in the
// registry, the store and `rex config`.
func TestHiddenSettings(t *testing.T) {
	s, ok := Find("keybind_preset")
	require.True(t, ok, "Find still returns a hidden setting")
	require.True(t, s.Hidden)
	require.Equal(t, "default", NewStore().Get("keybind_preset"))

	for _, v := range Visible() {
		require.False(t, v.Hidden, v.ID)
	}
	require.Len(t, Visible(), len(Registry)-1)
	require.NotContains(t, visibleIDs(), "keybind_preset")
	require.Contains(t, visibleIDs(), "theme")

	for _, r := range Registry {
		if r.ID != "keybind_preset" {
			require.False(t, r.Hidden, "only keybind_preset is hidden: %s", r.ID)
		}
	}
}

func visibleIDs() []string {
	var ids []string
	for _, s := range Visible() {
		ids = append(ids, s.ID)
	}
	return ids
}

func TestLuaConfigHelpHasNoPlanLetter(t *testing.T) {
	s, _ := Find("lua_config_path")
	require.False(t, strings.Contains(s.Help, "Plan"), s.Help)
}
