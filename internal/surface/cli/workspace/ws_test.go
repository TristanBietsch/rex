package workspace

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/testutil/testenv"
	wsp "github.com/tristanbietsch/rex/internal/workspace"
)

func TestMain(m *testing.M) { testenv.Run(m) }

type rig struct {
	home string
	out  *bytes.Buffer
	err  *bytes.Buffer
}

// newRig isolates HOME, optionally turns the workspaces feature on, and
// captures the printer's output.
func newRig(t *testing.T, on bool) *rig {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if on {
		cfg := filepath.Join(home, ".config", "rex", "config.yaml")
		require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o700))
		require.NoError(t, os.WriteFile(cfg, []byte("workspaces: true\n"), 0o600))
	}
	r := &rig{home: home, out: &bytes.Buffer{}, err: &bytes.Buffer{}}
	r.printer(t, core.FormatFlags{})
	return r
}

func (r *rig) printer(t *testing.T, fl core.FormatFlags) {
	t.Helper()
	r.out.Reset()
	p, err := core.NewPrinter(fl, core.Env{Getenv: func(string) string { return "" }, Width: 100, GOOS: runtime.GOOS}, r.out, io.Discard)
	require.NoError(t, err)
	core.SetOut(p)
	t.Cleanup(func() { core.SetOut(nil) })
}

func (r *rig) project(t *testing.T, name string) string {
	t.Helper()
	d := filepath.Join(r.home, "dev", name)
	require.NoError(t, os.MkdirAll(d, 0o750))
	return d
}

func exitCode(t *testing.T, err error) int {
	t.Helper()
	var ec core.ExitCoder
	require.ErrorAs(t, err, &ec)
	return ec.ExitCode()
}

func TestWs_OffExits5AndCreatesNothing(t *testing.T) {
	r := newRig(t, false)
	proj := r.project(t, "p")
	t.Chdir(proj)
	for _, args := range [][]string{
		{"init"}, {"init", proj}, {"id"}, {"ls"}, {"rename", "7f3b89a2", "x"}, {"color", "7f3b89a2", "1"},
	} {
		err := RunWs(args)
		require.Errorf(t, err, "%v", args)
		require.Equalf(t, core.ExitInvalidArgs, exitCode(t, err), "%v", args)
		require.Contains(t, err.Error(), "off: enable in Settings → Features (rex config set workspaces true)")
	}
	require.NoDirExists(t, filepath.Join(proj, ".rex"))
	require.NoDirExists(t, filepath.Join(r.home, ".local"), "no registry written either")
	require.Empty(t, r.out.String())
}

func TestWs_UsageErrors(t *testing.T) {
	newRig(t, true)
	for _, args := range [][]string{
		{}, {"bogus"}, {"ls", "extra"}, {"rename", "only-one"}, {"color"}, {"init", "a", "b"}, {"id", "a", "b"},
	} {
		err := RunWs(args)
		require.Errorf(t, err, "%v", args)
		require.Equalf(t, core.ExitInvalidArgs, exitCode(t, err), "%v", args)
	}
	require.ErrorIs(t, RunWs([]string{"-h"}), core.ErrHelpShown)
}

func decode[T any](t *testing.T, r *rig) T {
	t.Helper()
	var env struct {
		Data T `json:"data"`
	}
	require.NoError(t, json.Unmarshal(r.out.Bytes(), &env), r.out.String())
	r.out.Reset() // each decode consumes one command's output
	return env.Data
}

func TestWs_InitJSONThenIDLsRenameColor(t *testing.T) {
	r := newRig(t, true)
	proj := r.project(t, "demo")
	nested := filepath.Join(proj, "src", "pkg")
	require.NoError(t, os.MkdirAll(nested, 0o750))
	t.Chdir(proj)

	r.printer(t, core.FormatFlags{Output: "json"})
	require.NoError(t, RunWs([]string{"init"}))
	created := decode[info](t, r)
	require.True(t, wsp.ValidID(created.ID))
	require.Equal(t, "demo", created.Name)
	require.True(t, created.Created)
	require.Equal(t, wsp.Swatches()[0], created.Color)

	// Text.
	r.printer(t, core.FormatFlags{Output: "text"})
	require.NoError(t, RunWs([]string{"init"}))
	require.Contains(t, r.out.String(), "already a workspace")
	require.Contains(t, r.out.String(), created.ID)

	// id from a nested folder, relative path and the cwd.
	r.printer(t, core.FormatFlags{Output: "json"})
	require.NoError(t, RunWs([]string{"id", filepath.Join("src", "pkg")}))
	require.Equal(t, created.ID, decode[info](t, r).ID)
	r.printer(t, core.FormatFlags{Output: "txt"})
	require.NoError(t, RunWs([]string{"id"}))
	require.Contains(t, r.out.String(), created.ID)

	// ls
	r.printer(t, core.FormatFlags{Output: "json"})
	require.NoError(t, RunWs([]string{"ls"}))
	rows := decode[[]lsRow](t, r)
	require.Len(t, rows, 1)
	require.Equal(t, created.ID, rows[0].ID)
	require.True(t, rows[0].Present)
	r.printer(t, core.FormatFlags{Output: "text"})
	require.NoError(t, RunWs([]string{"ls"}))
	require.Contains(t, r.out.String(), "demo")

	// rename by id and by path
	r.printer(t, core.FormatFlags{Output: "json"})
	require.NoError(t, RunWs([]string{"rename", created.ID, "Renamed"}))
	require.Equal(t, "Renamed", decode[info](t, r).Name)
	require.NoError(t, RunWs([]string{"rename", nested, "By Path"}))
	r.out.Reset()
	w, err := wsp.Find(proj, r.home)
	require.NoError(t, err)
	require.Equal(t, "By Path", w.Name)
	require.Equal(t, core.ExitInvalidArgs, exitCode(t, RunWs([]string{"rename", created.ID, "  \x1b "})))

	// color by swatch number and hex
	require.NoError(t, RunWs([]string{"color", created.ID, "3"}))
	require.Equal(t, wsp.Swatches()[2], decode[info](t, r).Color)
	require.NoError(t, RunWs([]string{"color", proj, "#abcdef"}))
	require.Equal(t, "#ABCDEF", decode[info](t, r).Color)
	for _, bad := range []string{"0", "8", "red", "#12345", ""} {
		require.Equalf(t, core.ExitInvalidArgs, exitCode(t, RunWs([]string{"color", created.ID, bad})), "%q", bad)
	}

	// The registry followed the edits.
	r.printer(t, core.FormatFlags{Output: "json"})
	require.NoError(t, RunWs([]string{"ls"}))
	rows = decode[[]lsRow](t, r)
	require.Equal(t, "By Path", rows[0].Name)
	require.Equal(t, "#ABCDEF", rows[0].Color)
}

func TestWs_InitNameFlagAndAgentFormat(t *testing.T) {
	r := newRig(t, true)
	proj := r.project(t, "p")
	t.Chdir(r.home)
	r.printer(t, core.FormatFlags{Output: "agent"})
	require.NoError(t, RunWs([]string{"init", "--name", "My Proj", proj}))
	out := r.out.String()
	require.Contains(t, out, "My Proj")
	w, err := wsp.Find(proj, r.home)
	require.NoError(t, err)
	require.Equal(t, "My Proj", w.Name)
}

func TestWs_InitAddsGitExclude(t *testing.T) {
	r := newRig(t, true)
	proj := r.project(t, "repo")
	require.NoError(t, os.MkdirAll(filepath.Join(proj, ".git"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644)) //nolint:gosec // test: the mode is what is under test
	t.Chdir(proj)
	r.printer(t, core.FormatFlags{Output: "json"})
	require.NoError(t, RunWs([]string{"init"}))
	require.True(t, decode[info](t, r).GitExcluded)
	b, err := os.ReadFile(filepath.Join(proj, ".git", "info", "exclude"))
	require.NoError(t, err)
	require.Equal(t, "/.rex/\n/.rex/audit.jsonl\n", string(b))

	require.NoError(t, RunWs([]string{"init"}))
	require.False(t, decode[info](t, r).GitExcluded, "second init adds nothing")
}

func TestWs_InitRefusesHomeAndRoot(t *testing.T) {
	r := newRig(t, true)
	t.Chdir(r.home)
	for _, args := range [][]string{{"init"}, {"init", r.home}, {"init", string(filepath.Separator)}} {
		err := RunWs(args)
		require.Error(t, err, "%v", args)
		require.Equal(t, core.ExitInvalidArgs, exitCode(t, err))
		require.Contains(t, err.Error(), "refusing")
	}
	require.NoDirExists(t, filepath.Join(r.home, ".rex"))
}

func TestWs_NotFoundErrors(t *testing.T) {
	r := newRig(t, true)
	proj := r.project(t, "p")
	t.Chdir(proj)

	err := RunWs([]string{"id"})
	require.Equal(t, core.ExitSelectorNotFound, exitCode(t, err))
	require.Contains(t, err.Error(), "rex ws init")

	require.Equal(t, core.ExitSelectorNotFound, exitCode(t, RunWs([]string{"rename", "deadbeef", "x"})))
	require.Equal(t, core.ExitSelectorNotFound, exitCode(t, RunWs([]string{"rename", proj, "x"})))

	r.printer(t, core.FormatFlags{Output: "text"})
	require.NoError(t, RunWs([]string{"ls"}))
	require.Contains(t, r.out.String(), "no workspaces yet")
}

func TestWs_MovedFolderIsReportedNotGuessed(t *testing.T) {
	r := newRig(t, true)
	proj := r.project(t, "p")
	t.Chdir(proj)
	r.printer(t, core.FormatFlags{Output: "json"})
	require.NoError(t, RunWs([]string{"init"}))
	id := decode[info](t, r).ID

	moved := filepath.Join(r.home, "dev", "moved")
	t.Chdir(r.home)
	require.NoError(t, os.Rename(proj, moved))
	err := RunWs([]string{"rename", id, "x"})
	require.Equal(t, core.ExitSelectorNotFound, exitCode(t, err))
	require.Contains(t, err.Error(), "not at")

	r.printer(t, core.FormatFlags{Output: "json"})
	require.NoError(t, RunWs([]string{"ls"}))
	require.False(t, decode[[]lsRow](t, r)[0].Present)
}

func TestWs_UnsafeRexRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("modes")
	}
	r := newRig(t, true)
	proj := r.project(t, "p")
	t.Chdir(proj)
	require.NoError(t, RunWs([]string{"init"}))
	require.NoError(t, os.Chmod(filepath.Join(proj, ".rex"), 0o770)) //nolint:gosec // test: the mode is what is under test
	for _, args := range [][]string{{"id"}, {"init"}} {
		err := RunWs(args)
		require.Equal(t, core.ExitOperationRefused, exitCode(t, err), "%v", args)
		require.True(t, strings.Contains(err.Error(), "unsafe"), err.Error())
	}
}

func TestWs_OversizedWorkspaceJSONRefused(t *testing.T) {
	r := newRig(t, true)
	proj := r.project(t, "p")
	require.NoError(t, os.MkdirAll(filepath.Join(proj, ".rex"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".rex", "workspace.json"),
		[]byte(`{"id":"7f3b89a2","name":"`+strings.Repeat("x", 70<<10)+`"}`), 0o600))
	t.Chdir(proj)
	err := RunWs([]string{"id"})
	require.Equal(t, core.ExitOperationRefused, exitCode(t, err))
}
