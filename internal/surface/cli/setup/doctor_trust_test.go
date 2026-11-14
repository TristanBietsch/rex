package setup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

func writeTrustConfig(t *testing.T, value string) {
	t.Helper()
	cfg, err := paths.ConfigFile()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o700))
	require.NoError(t, os.WriteFile(cfg, []byte("auto_trust_dirs: "+value+"\n"), 0o600))
}

func TestCheckAutoTrustDirs(t *testing.T) {
	home, project := trustHome(t)

	require.Empty(t, checkAutoTrustDirs(), "unset: nothing to check")

	writeTrustConfig(t, project)
	rows := checkAutoTrustDirs()
	require.Len(t, rows, 1)
	require.Equal(t, "auto_trust_dirs", rows[0].Name)
	require.Equal(t, checkInfo, rows[0].Status)
	require.Contains(t, rows[0].Detail, project)

	missing := filepath.Join(home, "dev", "gone")
	for name, tc := range map[string]struct{ value, want string }{
		"missing":  {missing, "does not exist"},
		"home":     {home, "home folder"},
		"ancestor": {filepath.Dir(home), "holds your home"},
		"relative": {"dev/proj", "absolute"},
	} {
		t.Run(name, func(t *testing.T) {
			writeTrustConfig(t, tc.value)
			rows := checkAutoTrustDirs()
			require.NotEmpty(t, rows)
			var warns []checkResult
			for _, r := range rows {
				if r.Status == checkWarn {
					warns = append(warns, r)
				}
			}
			require.NotEmpty(t, warns, "%v", rows)
			require.Equal(t, "auto_trust_dirs", warns[0].Name)
			require.Contains(t, warns[0].Detail, tc.want)
		})
	}
}

func TestRunAllChecks_IncludesAutoTrustDirs(t *testing.T) {
	_, project := trustHome(t)
	writeTrustConfig(t, project)
	var names []string
	for _, r := range runAllChecks(&core.SocketFlag{}) {
		names = append(names, r.Name)
	}
	require.Contains(t, names, "auto_trust_dirs")
}
