package settings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCoveredBy(t *testing.T) {
	home, dev := trustHome(t)
	proj := filepath.Join(dev, "proj")
	require.NoError(t, os.MkdirAll(filepath.Join(proj, "sub"), 0o700))
	other := filepath.Join(dev, "other")
	require.NoError(t, os.MkdirAll(other, 0o700))

	e, ok := CoveredBy(filepath.Join(proj, "sub"), "~/dev/proj")
	require.True(t, ok)
	require.Equal(t, "~/dev/proj", e)
	_, ok = CoveredBy(proj, proj)
	require.True(t, ok, "the folder itself")
	_, ok = CoveredBy(other, "~/dev/proj")
	require.False(t, ok)
	_, ok = CoveredBy(other, "")
	require.False(t, ok, "nothing listed")
	_, ok = CoveredBy(proj, "~/dev/proj-extra")
	require.False(t, ok, "whole path elements")
	_, ok = CoveredBy(proj, home)
	require.False(t, ok, "an entry the daemon refuses covers nothing")
	_, ok = CoveredBy("relative/dir", "~/dev/proj")
	require.False(t, ok)
}
