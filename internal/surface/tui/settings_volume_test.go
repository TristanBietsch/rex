package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
)

type volumeAudio struct{ vols []float64 }

func (a *volumeAudio) Play(string)         {}
func (a *volumeAudio) SetVolume(v float64) { a.vols = append(a.vols, v) }
func (a *volumeAudio) SetEnabled(bool)     {}
func (a *volumeAudio) SetSoundset(string)  {}
func (a *volumeAudio) SetBootVoice(bool)   {}

func volumeModel(t *testing.T) (Model, *volumeAudio) {
	t.Helper()
	a := &volumeAudio{}
	return Model{
		Store:     settings.NewStore(),
		StorePath: filepath.Join(t.TempDir(), "config.yaml"),
		Settings:  &SettingsState{CursorID: "master_volume"},
		Audio:     a,
		Width:     100,
		Height:    60,
	}, a
}

func TestMasterVolume_StepsByOneAndSetsGain(t *testing.T) {
	m, a := volumeModel(t)
	m = applySettingsAction(m, +1)
	require.Equal(t, 41, m.Store.Get("master_volume"))
	m = applySettingsAction(m, -1)
	m = applySettingsAction(m, -1)
	require.Equal(t, 39, m.Store.Get("master_volume"))
	require.Len(t, a.vols, 3)
	require.InDelta(t, 0.82, a.vols[0], 1e-9)
	require.InDelta(t, 0.78, a.vols[2], 1e-9)
}

func TestMasterVolume_ClampsAndShowsMax(t *testing.T) {
	m, a := volumeModel(t)
	require.NoError(t, m.Store.Set("master_volume", 49))
	require.Equal(t, "49", settingValueText(m, mustSetting(t, "master_volume")))
	m = applySettingsAction(m, +1)
	m = applySettingsAction(m, +1)
	require.Equal(t, 50, m.Store.Get("master_volume"))
	require.InDelta(t, 1.0, a.vols[len(a.vols)-1], 1e-9)
	require.Equal(t, "max", settingValueText(m, mustSetting(t, "master_volume")))

	require.NoError(t, m.Store.Set("master_volume", 0))
	require.Equal(t, "0", settingValueText(m, mustSetting(t, "master_volume")))
	m = applySettingsAction(m, -1)
	require.Equal(t, 0, m.Store.Get("master_volume"))
}

func TestMasterVolume_RowRendersMax(t *testing.T) {
	m, _ := volumeModel(t)
	require.NoError(t, m.Store.Set("master_volume", 50))
	out := ansi.Strip(renderSettings(m, 100))
	var row string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "Master volume") {
			row = l
		}
	}
	require.Contains(t, row, "◂ max ▸")
	require.Contains(t, out, "range 0–50")
}

func mustSetting(t *testing.T, id string) settings.Setting {
	t.Helper()
	s, ok := settings.Find(id)
	require.True(t, ok)
	return s
}
