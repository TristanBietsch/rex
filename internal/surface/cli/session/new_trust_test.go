package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseNewArgs_TrustRefusesHomeAndAncestors(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	project := filepath.Join(home, "proj")
	require.NoError(t, os.MkdirAll(project, 0o700))

	require.NoError(t, checkNewTrust(false, home), "no -trust, nothing to check")
	require.NoError(t, checkNewTrust(true, project))
	for _, bad := range []string{home, filepath.Dir(home), string(filepath.Separator)} {
		requireInvalidArgs(t, checkNewTrust(true, bad))
	}
}
