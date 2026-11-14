package workspace

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/surface/tui"
	wsp "github.com/tristanbietsch/rex/internal/workspace"
	"github.com/tristanbietsch/rex/internal/workspace/wsconfig"
)

// captureBoard replaces the TUI launch and records the scope it got.
func captureBoard(t *testing.T) *[]*tui.WorkspaceScope {
	t.Helper()
	// A board needs a terminal: tests run in text mode like one.
	p, err := core.NewPrinter(core.FormatFlags{Output: "text"}, core.Env{Getenv: func(string) string { return "" }, Width: 100, GOOS: runtime.GOOS}, io.Discard, io.Discard)
	require.NoError(t, err)
	core.SetOut(p)
	t.Cleanup(func() { core.SetOut(nil) })
	var got []*tui.WorkspaceScope
	prev := runBoard
	runBoard = func(s *tui.WorkspaceScope) error { got = append(got, s); return nil }
	t.Cleanup(func() { runBoard = prev })
	return &got
}

func TestHere_NoWorkspaceExits5(t *testing.T) {
	r := newRig(t, true)
	proj := r.project(t, "plain")
	t.Chdir(proj)
	got := captureBoard(t)

	for _, run := range []func() error{
		func() error { return RunHere(nil) },
		func() error { return RunWs([]string{"open"}) },
		func() error { return RunWs([]string{"open", proj}) },
	} {
		err := run()
		require.Error(t, err)
		require.Equal(t, core.ExitInvalidArgs, exitCode(t, err))
		require.Contains(t, err.Error(), "no .rex/ here — run `rex ws init`")
	}
	require.Empty(t, *got, "no board without a workspace")
}

func TestHere_OffRefusesWithTheStandardMessage(t *testing.T) {
	r := newRig(t, false)
	proj := r.project(t, "p")
	t.Chdir(proj)
	got := captureBoard(t)

	for _, run := range []func() error{
		func() error { return RunHere(nil) },
		func() error { return RunWs([]string{"open"}) },
	} {
		err := run()
		require.Error(t, err)
		require.Equal(t, core.ExitInvalidArgs, exitCode(t, err))
		require.Contains(t, err.Error(), "off: enable in Settings → Features (rex config set workspaces true)")
	}
	require.Empty(t, *got)
	require.NoDirExists(t, filepath.Join(r.home, ".local"), "no discovery side effects, no registry")
}

func TestHere_OpensTheBoardOnTheNearestWorkspace(t *testing.T) {
	r := newRig(t, true)
	proj := r.project(t, "demo")
	w, err := wsp.Init(proj, "demo", nil)
	require.NoError(t, err)
	nested := filepath.Join(proj, "src", "pkg")
	require.NoError(t, os.MkdirAll(nested, 0o750))
	got := captureBoard(t)

	t.Chdir(nested)
	require.NoError(t, RunHere(nil))
	require.Len(t, *got, 1)
	sc := (*got)[0]
	require.Equal(t, w.ID, sc.ID)
	require.Equal(t, "demo", sc.Name)
	require.Equal(t, w.Color, sc.Color)
	resolved, err := filepath.EvalSymlinks(proj)
	require.NoError(t, err)
	gotRoot, err := filepath.EvalSymlinks(sc.Root)
	require.NoError(t, err)
	require.Equal(t, resolved, gotRoot)

	// ws open <path> works from anywhere.
	t.Chdir(r.home)
	require.NoError(t, RunWs([]string{"open", nested}))
	require.Len(t, *got, 2)
	require.Equal(t, w.ID, (*got)[1].ID)
	require.Equal(t, w.ID, wsconfig.Current().ID, "the board applies that workspace's config, not the start folder's")
	_ = r
}

func TestHere_RefusesArgumentsAndDataFormats(t *testing.T) {
	r := newRig(t, true)
	proj := r.project(t, "demo")
	_, err := wsp.Init(proj, "demo", nil)
	require.NoError(t, err)
	t.Chdir(proj)
	got := captureBoard(t)

	err = RunHere([]string{"extra"})
	require.Error(t, err)
	require.Equal(t, core.ExitInvalidArgs, exitCode(t, err))

	err = RunWs([]string{"open", "a", "b"})
	require.Error(t, err)
	require.Equal(t, core.ExitInvalidArgs, exitCode(t, err))

	err = RunWs([]string{"open", "-o", "json"})
	require.Error(t, err, "a board has no data form")
	require.Equal(t, core.ExitInvalidArgs, exitCode(t, err))
	require.Empty(t, *got)
}

func TestHere_AgentModeRefuses(t *testing.T) {
	r := newRig(t, true)
	proj := r.project(t, "demo")
	_, err := wsp.Init(proj, "demo", nil)
	require.NoError(t, err)
	t.Chdir(proj)
	got := captureBoard(t)
	r.printer(t, core.FormatFlags{Output: "agent"})
	err = RunHere(nil)
	require.Error(t, err)
	require.Equal(t, core.ExitInvalidArgs, exitCode(t, err))
	require.Empty(t, *got)
}

func TestHere_UnsafeRexIsRefused(t *testing.T) {
	r := newRig(t, true)
	proj := r.project(t, "demo")
	_, err := wsp.Init(proj, "demo", nil)
	require.NoError(t, err)
	require.NoError(t, os.Chmod(filepath.Join(proj, ".rex"), 0o777)) //nolint:gosec // the test
	if _, serr := wsp.Find(proj, r.home); serr == nil {
		t.Skip("mode checks are off on this platform")
	}
	t.Chdir(proj)
	got := captureBoard(t)
	err = RunHere(nil)
	require.Error(t, err)
	require.Equal(t, core.ExitOperationRefused, exitCode(t, err))
	require.Empty(t, *got)
}
