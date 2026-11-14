package boot

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
)

// storeWith returns the defaults overlaid with a config.yaml holding body.
func storeWith(t *testing.T, body string) *settings.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	st := settings.NewStore()
	require.NoError(t, st.Load(path))
	return st
}

func TestSkipPermissions(t *testing.T) {
	require.False(t, SkipPermissions(settings.NewStore()), "default: off")
	require.True(t, SkipPermissions(storeWith(t, "skip_permissions: true\n")))
}

func TestAutoTrustDirs(t *testing.T) {
	require.Empty(t, AutoTrustDirs(settings.NewStore()), "default: trust nothing")

	work := filepath.Join(t.TempDir(), "work") // absolute on every OS
	sep := string(filepath.ListSeparator)
	// Single-quoted YAML keeps Windows backslashes literal.
	set := func(v string) *settings.Store { return storeWith(t, "auto_trust_dirs: '"+v+"'\n") }
	require.Equal(t, []string{"~/dev", work}, AutoTrustDirs(set("~/dev"+sep+work)))

	// A folder name holding the separator splits into a broader folder and
	// a relative piece; the whole setting is ignored instead.
	require.Nil(t, AutoTrustDirs(set(work+sep+"b")))
}

func TestSpawnSkillsEnabled(t *testing.T) {
	require.True(t, SpawnSkillsEnabled(settings.NewStore()), "default: on")
	require.False(t, SpawnSkillsEnabled(storeWith(t, "spawn_skills: false\n")))
}

func TestResumeEnabled(t *testing.T) {
	require.True(t, ResumeEnabled(settings.NewStore()), "default: on")
	require.False(t, ResumeEnabled(storeWith(t, "resume: false\n")))
}
