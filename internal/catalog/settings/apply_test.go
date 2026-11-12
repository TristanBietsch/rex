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

func TestApplyModesMatchHowSettingsAreRead(t *testing.T) {
	for id, want := range map[string]ApplyMode{
		"color_scheme":            ApplyLive,
		"max_concurrent_sessions": ApplyLive,
		"summary_detail":          ApplyLive, // the daemon's settings watcher
		"turn_snapshots":          ApplyLive,
		"attach_banner":           ApplyNextAttach,
		"attach_scrollback_lines": ApplyNextAttach,
		"default_spawn_tool":      ApplyNextSpawn,
		"auto_trust_dirs":         ApplyNextSpawn,
		"skip_permissions":        ApplyNextSpawn,
		"summary_enabled":         ApplyDaemonRestart,
		"summary_model":           ApplyDaemonRestart,
	} {
		s, ok := Find(id)
		require.True(t, ok, id)
		require.Equal(t, want, s.Apply, id)
	}
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
	require.Contains(t, visibleIDs(), "color_scheme")

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
