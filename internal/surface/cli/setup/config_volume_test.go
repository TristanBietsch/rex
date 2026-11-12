package setup

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// master_volume at 50 reads "max" in the human formats; txt, agent and JSON
// keep the number for scripts.
func TestConfigVolume_MaxInHumanOutput(t *testing.T) {
	luaConfigHome(t, "", "master_volume: 1.0\n") // legacy float: 50

	text, _, err := runFmt(t, "text", func() error { return configGet([]string{"master_volume"}) })
	require.NoError(t, err)
	require.Equal(t, "max", strings.TrimSpace(text))
	txt, _, err := runFmt(t, "txt", func() error { return configGet([]string{"master_volume"}) })
	require.NoError(t, err)
	require.Equal(t, "50\n", txt)
	js, _, err := runFmt(t, "json", func() error { return configGet([]string{"master_volume"}) })
	require.NoError(t, err)
	require.Contains(t, js, `"value":"50"`)

	list, _, err := runFmt(t, "text", func() error { return configList(nil) })
	require.NoError(t, err)
	require.Regexp(t, `master_volume\s+max\b`, list)
	list, _, err = runFmt(t, "txt", func() error { return configList(nil) })
	require.NoError(t, err)
	require.Regexp(t, `(?m)^  master_volume +50$`, list)
	js, _, err = runFmt(t, "json", func() error { return configList(nil) })
	require.NoError(t, err)
	require.Contains(t, js, `"id":"master_volume","section":"Audio","value":50,"default":40,"source":"file"`)
	ag, _, err := runFmt(t, "agent", func() error { return configList(nil) })
	require.NoError(t, err)
	require.Contains(t, ag, "setting id=master_volume section=Audio value=50 default=40 source=file")

	set, _, err := runFmt(t, "text", func() error { return configSet([]string{"master_volume", "0.5"}) })
	require.NoError(t, err)
	require.Contains(t, set, "set master_volume = 25")
	set, _, err = runFmt(t, "text", func() error { return configSet([]string{"master_volume", "50"}) })
	require.NoError(t, err)
	require.Contains(t, set, "set master_volume = max")
}
