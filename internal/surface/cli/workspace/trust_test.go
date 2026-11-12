package workspace

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

// tty makes the printer a text one at a terminal whose user answers ans.
func (r *rig) tty(t *testing.T, ans string) {
	t.Helper()
	r.out.Reset()
	env := core.Env{Getenv: func(string) string { return "" }, Width: 100, GOOS: runtime.GOOS, InTTY: true, ErrTTY: true}
	p, err := core.NewPrinter(core.FormatFlags{Output: "text"}, env, r.out, io.Discard)
	require.NoError(t, err)
	p.In = strings.NewReader(ans)
	core.SetOut(p)
	t.Cleanup(func() { core.SetOut(nil) })
}

func TestWsTrust_TrustAndRevoke(t *testing.T) {
	r := newRig(t, true)
	proj := r.project(t, "demo")
	t.Chdir(proj)
	require.NoError(t, RunWs([]string{"init"}))
	body := []byte("rex.set('theme', 'noir')\n")
	require.NoError(t, os.WriteFile(wsp.ConfigPath(proj), body, 0o600))
	w, err := wsp.Load(proj)
	require.NoError(t, err)
	require.False(t, trustStore(t).Trusted(w))

	// Refused without a terminal or in a data format: no flag skips it.
	for _, fl := range []core.FormatFlags{{Output: "text"}, {Output: "json"}, {Output: "agent"}} {
		r.printer(t, fl)
		require.Equal(t, core.ExitOperationRefused, exitCode(t, RunWs([]string{"trust"})), "%+v", fl)
	}
	r.tty(t, "n\n")
	require.Equal(t, core.ExitOperationRefused, exitCode(t, RunWs([]string{"trust"})), "answered no")
	require.False(t, trustStore(t).Trusted(w))

	r.tty(t, "y\n")
	require.NoError(t, RunWs([]string{"trust"}))
	out := r.out.String()
	require.Contains(t, out, "trusted "+w.ID)
	require.Contains(t, out, wsp.ConfigHash(body)[:12])
	require.Contains(t, out, "may only set")
	h, ok := trustStore(t).TrustedHash(w)
	require.True(t, ok)
	require.Equal(t, wsp.ConfigHash(body), h)

	// Unchanged: no question, nothing to confirm.
	r.printer(t, core.FormatFlags{Output: "json"})
	require.NoError(t, RunWs([]string{"trust", proj}))
	d := decode[trustInfo](t, r)
	require.False(t, d.Changed)
	require.True(t, d.Config)
	require.Equal(t, len(body), d.Size)

	// An id is refused: the registry could map it to a copy elsewhere.
	require.Equal(t, core.ExitInvalidArgs, exitCode(t, RunWs([]string{"trust", w.ID})))

	// Edited: asks again.
	require.NoError(t, os.WriteFile(wsp.ConfigPath(proj), []byte("rex.set('theme', 'paper')\n"), 0o600))
	r.printer(t, core.FormatFlags{Output: "json"})
	require.Equal(t, core.ExitOperationRefused, exitCode(t, RunWs([]string{"trust"})))

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
	r.tty(t, "y\n")
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
