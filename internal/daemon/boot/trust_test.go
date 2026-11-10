package boot

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAutoTrustDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	require.Empty(t, AutoTrustDirs(), "no config: trust nothing")

	cfg := filepath.Join(home, ".config", "rex", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o700))
	list := "~/dev" + string(filepath.ListSeparator) + "/srv/work"
	require.NoError(t, os.WriteFile(cfg, []byte("auto_trust_dirs: \""+list+"\"\n"), 0o600))
	require.Equal(t, []string{"~/dev", "/srv/work"}, AutoTrustDirs())

	// A folder name holding the separator splits into a broader folder and
	// a relative piece; the whole setting is ignored instead.
	sep := string(filepath.ListSeparator)
	require.NoError(t, os.WriteFile(cfg, []byte("auto_trust_dirs: \"/work/a"+sep+"b\"\n"), 0o600))
	require.Nil(t, AutoTrustDirs())

	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	require.Nil(t, AutoTrustDirs(), "no home: trust nothing")
}
