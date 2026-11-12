package settings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMasterVolume_IsIntZeroToFifty(t *testing.T) {
	s, ok := Find("master_volume")
	require.True(t, ok)
	require.Equal(t, TypeInt, s.Type)
	require.Equal(t, 40, s.Default)
	require.Equal(t, 0.0, s.Min)
	require.Equal(t, 50.0, s.Max)
	require.Equal(t, "max", s.MaxText)
}

// A config.yaml from before the 0–50 scale holds a float literal from 0.0
// to 1.0; Load converts it to round(v*50). Whole numbers are the new scale.
func TestMasterVolume_LoadMigratesLegacyFloat(t *testing.T) {
	cases := []struct {
		yaml string
		want int
	}{
		{"0.8", 40},
		{"0.5", 25},
		{"0.25", 13},
		{"0.01", 1},
		{"1.0", 50},
		{"0.0", 0},
		{"1", 1},
		{"0", 0},
		{"40", 40},
		{"50", 50},
		{"40.0", 40},
	}
	for _, c := range cases {
		t.Run(c.yaml, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte("master_volume: "+c.yaml+"\n"), 0o600))
			s := NewStore()
			require.NoError(t, s.Load(path))
			require.Equal(t, c.want, s.Get("master_volume"))
			require.Equal(t, SourceFile, s.Source("master_volume"))
		})
	}
}

func TestMasterVolume_LoadSkipsInvalid(t *testing.T) {
	for _, v := range []string{"51", "1.5", "-0.2", "-1", ".nan", "loud"} {
		t.Run(v, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte("master_volume: "+v+"\n"), 0o600))
			s := NewStore()
			require.NoError(t, s.Load(path))
			require.Equal(t, 40, s.Get("master_volume"))
			require.Equal(t, SourceDefault, s.Source("master_volume"))
		})
	}
}

// Load converts in memory only: config.yaml keeps the old value until the
// volume is changed, and an unrelated Save does not rewrite it.
func TestMasterVolume_LoadDoesNotRewriteFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("master_volume: 0.8\n"), 0o600))
	s := NewStore()
	require.NoError(t, s.Load(path))
	require.NoError(t, s.Set("spinner", "moon"))
	require.NoError(t, s.Save(path))
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(raw), "master_volume: 0.8")

	require.NoError(t, s.Set("master_volume", 41))
	require.NoError(t, s.Save(path))
	raw, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(raw), "master_volume: 41")
}

// `rex config set` passes a string: a decimal literal from 0 to 1 is the old
// scale, a whole number the new one.
func TestMasterVolume_SetString(t *testing.T) {
	ok := map[string]int{"0.8": 40, "1.0": 50, "0.0": 0, "1": 1, "0": 0, "50": 50, "40.0": 40}
	for in, want := range ok {
		s := NewStore()
		require.NoError(t, s.Set("master_volume", in), in)
		require.Equal(t, want, s.Get("master_volume"), in)
	}
	for _, in := range []string{"51", "1.5", "12.5", "-0.2", "NaN", "Inf", "loud", ""} {
		require.Error(t, NewStore().Set("master_volume", in), in)
	}
}

func TestMasterVolume_SetNumbers(t *testing.T) {
	s := NewStore()
	require.NoError(t, s.Set("master_volume", 0.8))
	require.Equal(t, 40, s.Get("master_volume"))
	require.NoError(t, s.Set("master_volume", 1))
	require.Equal(t, 1, s.Get("master_volume"))
	require.Error(t, s.Set("master_volume", 1.5))
	require.Error(t, s.Set("master_volume", 51))
}

func TestSetting_Display(t *testing.T) {
	s, _ := Find("master_volume")
	require.Equal(t, "max", s.Display(50))
	require.Equal(t, "49", s.Display(49))
	require.Equal(t, "0", s.Display(0))
	require.Equal(t, "40", s.Display(s.Default))
	other, _ := Find("max_concurrent_sessions")
	require.Equal(t, "64", other.Display(64), "no MaxText: the number")
	spin, _ := Find("spinner")
	require.Equal(t, "moon", spin.Display("moon"))

	st := NewStore()
	require.Equal(t, "40", st.Display("master_volume"))
	require.NoError(t, st.Set("master_volume", 50))
	require.Equal(t, "max", st.Display("master_volume"))
	require.Equal(t, "50", st.String("master_volume"), "String stays numeric")
}

func TestVolumeGain(t *testing.T) {
	require.InDelta(t, 0.8, VolumeGain(40), 1e-9)
	require.InDelta(t, 1.0, VolumeGain(50), 1e-9)
	require.InDelta(t, 0.0, VolumeGain(0), 1e-9)
	require.InDelta(t, 0.02, VolumeGain(1), 1e-9)
	require.InDelta(t, 1.0, VolumeGain(99), 1e-9, "clamped")
	require.InDelta(t, 0.0, VolumeGain(-3), 1e-9, "clamped")
	require.InDelta(t, 0.8, VolumeGain(nil), 1e-9, "not an int: the default")
}
