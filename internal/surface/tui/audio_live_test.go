package tui

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// audioCalls records every call the TUI makes on its player.
type audioCalls struct{ calls []string }

func (a *audioCalls) Play(ev string)          { a.calls = append(a.calls, "play "+ev) }
func (a *audioCalls) SetVolume(v float64)     { a.calls = append(a.calls, fmt.Sprintf("volume %.2f", v)) }
func (a *audioCalls) SetEnabled(b bool)       { a.calls = append(a.calls, fmt.Sprintf("enabled %t", b)) }
func (a *audioCalls) SetSoundset(name string) { a.calls = append(a.calls, "soundset "+name) }
func (a *audioCalls) SetBootVoice(b bool)     { a.calls = append(a.calls, fmt.Sprintf("voice %t", b)) }

// An edit to config.yaml from outside this TUI (rex config set, another
// board, an editor) reaches the player on the next poll.
func TestConfigReloadAppliesAudioSettings(t *testing.T) {
	m, cfg, dir := reloadModel(t)
	a := &audioCalls{}
	m.Audio = a

	// The value an older build wrote (0.0–1.0 float) reads on the 0–50 scale.
	writeFile(t, cfg, "sound_enabled: false\nsoundset: factorio\nmaster_volume: 0.7999999999999998\nboot_voice: false\nnotify_on_state_change: false\n")
	m, _ = runConfigCmd(t, m, loadConfig(cfg, dir, m.configStamp, false))
	require.Empty(t, m.Err)
	require.Equal(t, []string{"enabled false", "voice false"}, a.calls, "volume 40 is the default: unchanged")

	a.calls = nil
	writeFile(t, cfg, "sound_enabled: true\nsoundset: chiptune\nmaster_volume: 10\nboot_voice: true\nnotify_on_state_change: true\n")
	m, _ = runConfigCmd(t, m, loadConfig(cfg, dir, m.configStamp, false))
	require.Empty(t, m.Err)
	require.Equal(t, []string{
		"enabled true",
		"enabled true", "soundset chiptune",
		"volume 0.20",
		"voice true",
	}, a.calls)
}

// notify_on_state_change turned off in config.yaml silences the next
// transition without a restart.
func TestConfigReloadNotifyOnStateChange(t *testing.T) {
	m, cfg, dir := reloadModel(t)
	a := &audioCalls{}
	m.Audio = a
	m.Sessions = []protocol.SessionSummary{{ID: "s1", Slug: "one", State: protocol.StateWorking}}

	writeFile(t, cfg, "notify_on_state_change: false\n")
	m, _ = runConfigCmd(t, m, loadConfig(cfg, dir, m.configStamp, false))
	m = m.applyEvent(stateUpdate(t, "s1", protocol.StateNeedsInput))
	require.Empty(t, a.calls)

	m = m.applyEvent(stateUpdate(t, "s1", protocol.StateWorking))
	writeFile(t, cfg, "notify_on_state_change: true\n")
	m, _ = runConfigCmd(t, m, loadConfig(cfg, dir, m.configStamp, false))
	m = m.applyEvent(stateUpdate(t, "s1", protocol.StateNeedsInput))
	require.Contains(t, a.calls, "play attention")
}

// The settings page applies each audio row to the player as it changes.
func TestSettingsPageAppliesAudioSettings(t *testing.T) {
	m, _, _ := reloadModel(t)
	a := &audioCalls{}
	m.Audio = a
	m.Settings = &SettingsState{}

	steps := []struct {
		id   string
		step int
	}{{"sound_enabled", -1}, {"soundset", +1}, {"master_volume", +1}, {"boot_voice", -1}}
	for _, s := range steps {
		m.Settings.CursorID = s.id
		m = applySettingsAction(m, s.step)
	}
	require.Equal(t, []string{
		"enabled false",
		"enabled false", "soundset evangelion",
		"volume 0.82",
		"voice false",
	}, a.calls)
}
