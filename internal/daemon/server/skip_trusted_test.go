package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
)

func TestTrustDecisionFor_Trusted(t *testing.T) {
	trusted := t.TempDir()
	other := t.TempDir()
	sub := filepath.Join(trusted, "sub")
	require.NoError(t, os.Mkdir(sub, 0o700))
	echo := registry.Tool{ID: "echo"} // no trust prompt or args: Trusted must not depend on them

	off := &Server{}
	require.False(t, off.trustDecisionFor(echo, trusted).Trusted, "no auto_trust_dirs")

	on := &Server{cfg: Config{AutoTrustDirs: func() []string { return []string{trusted} }}}
	require.True(t, on.trustDecisionFor(echo, trusted).Trusted)
	require.True(t, on.trustDecisionFor(echo, sub).Trusted)
	require.False(t, on.trustDecisionFor(echo, other).Trusted)
}

// The setting alone is not enough: the flag reaches argv only when the
// folder is trusted, for a resumed session too.
func TestResume_SkipPermissionsOnlyInTrustedFolder(t *testing.T) {
	const tools = `tools:
  - id: echo
    skip_permissions_args: ["--skip-it"]
    resume:
      args: ["--resume", "{id}"]
    models:
      - id: argv
        name: "Prints its argv"
        args: ['echo "ARGV: $0 $*"; sleep 30']
`
	for _, tc := range []struct {
		name         string
		setting      bool
		trustCWD     bool
		wantSkipFlag bool
	}{
		{"on and trusted", true, true, true},
		{"on and untrusted", true, false, false},
		{"off and trusted", false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := startResumeServer(t, tools, 0)
			e.srv.cfg.SkipPermissions = func() bool { return tc.setting }
			if tc.trustCWD {
				e.srv.cfg.AutoTrustDirs = func() []string { return []string{e.dir} }
			}
			require.NoError(t, e.store.Add(exited(func() string { return e.dir }, "a", "done", "argv", testResumeID)))
			conn, w, r := e.dial(t)
			code, msg := call(t, conn, w, r, "Resume", map[string]string{"session_id": "a"})
			require.Empty(t, code, msg)
			require.Eventually(t, func() bool { return strings.Contains(e.transcript("a"), "ARGV:") }, 10e9, 20e6)
			tr := e.transcript("a")
			require.Equal(t, tc.wantSkipFlag, strings.Contains(tr, "--skip-it"), tr)
			require.Contains(t, tr, "--resume "+testResumeID)
			require.NoError(t, e.srv.deleteSession("a", "user"))
		})
	}
}
