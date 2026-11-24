package boot

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/daemon/childenv"
	"github.com/tristanbietsch/rex/internal/daemon/server"
	"github.com/tristanbietsch/rex/internal/daemon/state"
)

func envServer(t *testing.T, o func() childenv.Options) *server.Server {
	t.Helper()
	srv, err := server.New(server.Config{Socket: filepath.Join(t.TempDir(), "s.sock"), Registry: &registry.Registry{}, Store: state.NewStore(), ChildEnv: o})
	require.NoError(t, err)
	return srv
}

func TestCatalogProbeEnv(t *testing.T) {
	t.Setenv("REX_E5_TOOLVAR", "t")
	t.Setenv("REX_E5_PASSVAR", "p")
	t.Setenv("REX_E5_SECRET", "s")
	tool := registry.Tool{EnvPass: []string{"REX_E5_TOOLVAR"}}

	// Policy from the server: env_passthrough for every tool plus the tool's own.
	cat := NewCatalog(envServer(t, func() childenv.Options {
		return childenv.Options{Mode: childenv.ModeAllowlist, Pass: []string{"REX_E5_PASSVAR"}}
	}), "")
	env := cat.probeEnv(tool)
	require.Contains(t, env, "REX_E5_TOOLVAR=t")
	require.Contains(t, env, "REX_E5_PASSVAR=p")
	require.NotContains(t, env, "REX_E5_SECRET=s")
	require.Equal(t, []string{"REX_E5_TOOLVAR"}, tool.EnvPass, "registry data untouched")

	// No policy configured: fails closed to the allowlist plus the tool's names.
	env = NewCatalog(envServer(t, nil), "").probeEnv(tool)
	require.Contains(t, env, "REX_E5_TOOLVAR=t")
	require.NotContains(t, env, "REX_E5_PASSVAR=p")
	require.NotContains(t, env, "REX_E5_SECRET=s")
}

func TestLoopsCheckEnv(t *testing.T) {
	t.Setenv("REX_E5_PASSVAR", "p")
	t.Setenv("REX_E5_SECRET", "s")
	l := NewLoops(t.Context(), t.TempDir(), t.TempDir(), state.NewStore(), func() bool { return true })

	// Before SetServer: fails closed.
	env := l.checkEnv()
	require.NotContains(t, env, "REX_E5_PASSVAR=p")
	require.NotContains(t, env, "REX_E5_SECRET=s")

	l.SetServer(envServer(t, func() childenv.Options {
		return childenv.Options{Mode: childenv.ModeAllowlist, Pass: []string{"REX_E5_PASSVAR"}}
	}))
	env = l.checkEnv()
	require.Contains(t, env, "REX_E5_PASSVAR=p")
	require.NotContains(t, env, "REX_E5_SECRET=s")
}
