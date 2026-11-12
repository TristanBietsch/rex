package settings

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// A broken config.lua fails auto_trust_dirs closed: the effective value reads
// empty. The YAML layer's own value must still be what config.yaml holds, so
// a read-modify-write of the list keeps the file's entries.
func TestFileString_IgnoresFailClosedLayer(t *testing.T) {
	path := writeYAML(t, "auto_trust_dirs: ~/dev/a\ntheme: paper\n")
	installLayer(t, LayerReport{Exists: true, Err: errors.New("config.lua: boom")})
	s, _, err := LoadLayered(path)
	require.NoError(t, err)
	require.Empty(t, s.String("auto_trust_dirs"), "the effective value fails closed")
	require.Equal(t, "~/dev/a", s.FileString("auto_trust_dirs"), "the YAML layer keeps its value")
	require.Equal(t, "paper", s.FileString("theme"))
	require.Equal(t, "opus", s.FileString("default_spawn_model"), "unset in the file: the default")
}

func TestFileString_IgnoresLuaValueAndFollowsSet(t *testing.T) {
	path := writeYAML(t, "theme: paper\n")
	installLayer(t, LayerReport{Exists: true,
		Values: map[string]any{"theme": "noir"}, SetAt: map[string]string{"theme": "config.lua:1"}})
	s, _, err := LoadLayered(path)
	require.NoError(t, err)
	require.Equal(t, "noir", s.String("theme"))
	require.Equal(t, "paper", s.FileString("theme"))

	require.NoError(t, s.Set("spinner", "moon"))
	require.Equal(t, "moon", s.FileString("spinner"), "a Set is the file's next value")
	require.NoError(t, s.Reset("spinner"))
	require.Equal(t, "braille", s.FileString("spinner"), "a Reset is the default again")
	c := s.WithDefaults("theme")
	require.Equal(t, "paper", c.FileString("theme"), "WithDefaults keeps the file layer")
}
