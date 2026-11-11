package boot

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSkipPermissions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	require.False(t, SkipPermissions(), "no config: off")

	cfg := filepath.Join(home, ".config", "rex", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o700))
	require.NoError(t, os.WriteFile(cfg, []byte("skip_permissions: true\n"), 0o600))
	require.True(t, SkipPermissions())

	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	require.False(t, SkipPermissions(), "no home: off")
}

func TestAutoTrustDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	require.Empty(t, AutoTrustDirs(), "no config: trust nothing")

	cfg := filepath.Join(home, ".config", "rex", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o700))
	work := filepath.Join(home, "work") // absolute on every OS
	sep := string(filepath.ListSeparator)
	writeSetting := func(v string) {
		t.Helper()
		// Single-quoted YAML keeps Windows backslashes literal.
		require.NoError(t, os.WriteFile(cfg, []byte("auto_trust_dirs: '"+v+"'\n"), 0o600))
	}
	writeSetting("~/dev" + sep + work)
	require.Equal(t, []string{"~/dev", work}, AutoTrustDirs())

	// A folder name holding the separator splits into a broader folder and
	// a relative piece; the whole setting is ignored instead.
	writeSetting(work + sep + "b")
	require.Nil(t, AutoTrustDirs())

	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	require.Nil(t, AutoTrustDirs(), "no home: trust nothing")
}

func TestResumeEnabled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	require.True(t, ResumeEnabled(), "no config: the default is on")

	cfg := filepath.Join(home, ".config", "rex", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o700))
	require.NoError(t, os.WriteFile(cfg, []byte("resume: false\n"), 0o600))
	require.False(t, ResumeEnabled())

	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	require.True(t, ResumeEnabled(), "no home: defaults")
}
