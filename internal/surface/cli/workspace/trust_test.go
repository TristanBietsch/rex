package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	wsp "github.com/tristanbietsch/rex/internal/workspace"
)

func trustStore(t *testing.T) *wsp.TrustStore {
	t.Helper()
	p, err := paths.WorkspaceTrustFile()
	require.NoError(t, err)
	s, err := wsp.OpenTrust(p)
	require.NoError(t, err)
	return s
}

func TestWsTrust_TrustAndRevoke(t *testing.T) {
	r := newRig(t, true)
	proj := r.project(t, "demo")
	t.Chdir(proj)
	require.NoError(t, RunWs([]string{"init"}))
	require.NoError(t, os.WriteFile(wsp.ConfigPath(proj), []byte("rex.set('theme', 'noir')\n"), 0o600))
	w, err := wsp.Load(proj)
	require.NoError(t, err)
	require.False(t, trustStore(t).Trusted(w))

	r.printer(t, core.FormatFlags{Output: "json"})
	require.NoError(t, RunWs([]string{"trust"}))
	d := decode[trustInfo](t, r)
	require.Equal(t, "trust", d.Action)
	require.True(t, d.Changed)
	require.True(t, d.Config)
	require.Equal(t, w.ID, d.ID)
	require.True(t, trustStore(t).Trusted(w))

	r.printer(t, core.FormatFlags{Output: "text"})
	require.NoError(t, RunWs([]string{"trust", w.ID}))
	out := r.out.String()
	require.Contains(t, out, "already trusted")
	require.Contains(t, out, "auto_trust_dirs", "says what a workspace can never set")

	r.printer(t, core.FormatFlags{Output: "json"})
	require.NoError(t, RunWs([]string{"trust", "-rm", proj}))
	d = decode[trustInfo](t, r)
	require.Equal(t, "revoke", d.Action)
	require.True(t, d.Changed)
	require.False(t, trustStore(t).Trusted(w))
	require.NoError(t, RunWs([]string{"trust", "-rm", w.ID}))
	require.False(t, decode[trustInfo](t, r).Changed)
}

// A workspace that moved away (or was deleted) can still be revoked by id.
func TestWsTrust_RevokeByIDWhenGone(t *testing.T) {
	r := newRig(t, true)
	proj := r.project(t, "demo")
	t.Chdir(proj)
	require.NoError(t, RunWs([]string{"init"}))
	require.NoError(t, RunWs([]string{"trust"}))
	w, err := wsp.Load(proj)
	require.NoError(t, err)
	t.Chdir(r.home)
	require.NoError(t, os.RemoveAll(filepath.Join(proj, ".rex")))
	r.printer(t, core.FormatFlags{Output: "json"})
	require.NoError(t, RunWs([]string{"trust", "-rm", w.ID}))
	require.True(t, decode[trustInfo](t, r).Changed)
	list, err := trustStore(t).List()
	require.NoError(t, err)
	require.Empty(t, list)
}

func TestWsTrust_OffAndErrors(t *testing.T) {
	r := newRig(t, false)
	proj := r.project(t, "p")
	t.Chdir(proj)
	err := RunWs([]string{"trust"})
	require.Equal(t, core.ExitInvalidArgs, exitCode(t, err))
	require.NoDirExists(t, filepath.Join(r.home, ".local"), "nothing written while off")

	r = newRig(t, true)
	t.Chdir(r.project(t, "q"))
	require.Equal(t, core.ExitSelectorNotFound, exitCode(t, RunWs([]string{"trust"})), "no workspace here")
	require.Equal(t, core.ExitInvalidArgs, exitCode(t, RunWs([]string{"trust", "a", "b"})))
	require.Equal(t, core.ExitSelectorNotFound, exitCode(t, RunWs([]string{"trust", "-rm", "nowhere"})))
}
