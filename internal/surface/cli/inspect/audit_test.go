package inspect

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/features/audit"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/workspace"
)

var auditNow = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

type auditRig struct {
	home string
	out  *bytes.Buffer
}

// newAuditRig isolates HOME, writes config.yaml with cfg lines and captures
// the printer's output with a pinned clock.
func newAuditRig(t *testing.T, cfg string) *auditRig {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if cfg != "" {
		path := filepath.Join(home, ".config", "rex", "config.yaml")
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte(cfg), 0o600))
	}
	return &auditRig{home: home, out: &bytes.Buffer{}}
}

func (r *auditRig) printer(t *testing.T, format string) {
	t.Helper()
	r.out.Reset()
	p, err := core.NewPrinter(core.FormatFlags{Output: format},
		core.Env{Getenv: func(string) string { return "" }, Width: 120, GOOS: runtime.GOOS}, r.out, io.Discard)
	require.NoError(t, err)
	p.Now = func() time.Time { return auditNow }
	core.SetOut(p)
	t.Cleanup(func() { core.SetOut(nil) })
}

func (r *auditRig) run(t *testing.T, format string, args ...string) error {
	t.Helper()
	r.printer(t, format)
	return RunAudit(args)
}

func (r *auditRig) global(t *testing.T) string {
	t.Helper()
	p, err := paths.AuditFile()
	require.NoError(t, err)
	return p
}

func arec(id string, ended time.Time, mut ...func(*audit.Record)) audit.Record {
	r := audit.Record{
		TS: ended, SessionID: id, ShortID: id[:4], Name: "job-" + id[:4], Tool: "claude", Model: "opus",
		PromptFirstLine: "do the thing " + id[:4], State: "done", StartedAt: ended.Add(-5 * time.Minute),
		EndedAt: ended, DurationS: 300,
	}
	for _, m := range mut {
		m(&r)
	}
	return r
}

func appendAll(t *testing.T, path string, rs ...audit.Record) {
	t.Helper()
	for _, r := range rs {
		require.NoError(t, audit.Append(path, r))
	}
}

func decodeAudit(t *testing.T, r *auditRig) []audit.Record {
	t.Helper()
	var env struct {
		Data []audit.Record `json:"data"`
	}
	require.NoError(t, json.Unmarshal(r.out.Bytes(), &env), r.out.String())
	return env.Data
}

func ids(rs []audit.Record) []string {
	out := []string{}
	for _, r := range rs {
		out = append(out, r.SessionID)
	}
	return out
}

func TestAudit_OffRefusesWithExit5(t *testing.T) {
	r := newAuditRig(t, "audit_trail: false\n")
	appendAll(t, r.global(t), arec("aaaa1111", auditNow))
	err := r.run(t, "txt")
	var ec core.ExitCoder
	require.ErrorAs(t, err, &ec)
	require.Equal(t, core.ExitInvalidArgs, ec.ExitCode())
	require.Contains(t, err.Error(), "off: enable in Settings → Features (rex config set audit_trail true)")
	require.Empty(t, r.out.String())
}

func TestAudit_GlobalDefaultFiltersAndEveryFormat(t *testing.T) {
	r := newAuditRig(t, "")
	appendAll(t, r.global(t),
		arec("aaaa1111", auditNow.Add(-10*24*time.Hour)),
		arec("bbbb2222", auditNow.Add(-2*time.Hour), func(x *audit.Record) { x.Tool = "codex"; x.State = "failed" }),
		arec("cccc3333", auditNow.Add(-time.Hour)),
	)

	require.NoError(t, r.run(t, "json"))
	require.Equal(t, []string{"cccc3333", "bbbb2222", "aaaa1111"}, ids(decodeAudit(t, r)), "newest first")

	require.NoError(t, r.run(t, "json", "--since", "7d"))
	require.Equal(t, []string{"cccc3333", "bbbb2222"}, ids(decodeAudit(t, r)))
	require.NoError(t, r.run(t, "json", "--tool", "codex"))
	require.Equal(t, []string{"bbbb2222"}, ids(decodeAudit(t, r)))
	require.NoError(t, r.run(t, "json", "--state", "failed"))
	require.Equal(t, []string{"bbbb2222"}, ids(decodeAudit(t, r)))
	require.NoError(t, r.run(t, "json", "--tool", "codex", "--state", "done"))
	require.Empty(t, decodeAudit(t, r))

	for _, format := range []string{"text", "txt", "md", "agent"} {
		require.NoError(t, r.run(t, format), format)
		out := r.out.String()
		require.Contains(t, out, "job-cccc", format)
		require.Contains(t, out, "do the thing", format)
		if format == "agent" {
			require.Contains(t, out, "session_id=cccc3333")
		}
	}
}

func TestAudit_EmptyState(t *testing.T) {
	r := newAuditRig(t, "")
	require.NoError(t, r.run(t, "text"))
	require.Contains(t, r.out.String(), "no audit records in this scope")
	require.NoError(t, r.run(t, "json"))
	require.Empty(t, decodeAudit(t, r))
}

func TestAudit_ArgumentErrors(t *testing.T) {
	r := newAuditRig(t, "")
	for _, args := range [][]string{
		{"extra"}, {"--ws", "x", "--all"}, {"--state", "working"}, {"--since", "nope"},
	} {
		err := r.run(t, "txt", args...)
		var ec core.ExitCoder
		require.ErrorAs(t, err, &ec, "%v", args)
		require.Equal(t, core.ExitInvalidArgs, ec.ExitCode(), "%v", args)
	}
}

func TestAudit_WsFlagNeedsTheWorkspacesFeature(t *testing.T) {
	r := newAuditRig(t, "")
	err := r.run(t, "txt", "--ws", "7f3b89a2")
	var ec core.ExitCoder
	require.ErrorAs(t, err, &ec)
	require.Equal(t, core.ExitInvalidArgs, ec.ExitCode())
	require.Contains(t, err.Error(), "rex config set workspaces true")
}

// wsSetup makes a workspace at <home>/dev/proj with its own trail and the
// global trail beside it.
func wsSetup(t *testing.T, r *auditRig) (*workspace.Workspace, string) {
	t.Helper()
	root := filepath.Join(r.home, "dev", "proj")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "src"), 0o750))
	regPath, err := paths.WorkspacesFile()
	require.NoError(t, err)
	reg, err := workspace.OpenRegistry(regPath)
	require.NoError(t, err)
	w, err := workspace.Init(root, "proj", reg)
	require.NoError(t, err)
	appendAll(t, filepath.Join(root, ".rex", "audit.jsonl"),
		arec("aaaa1111", auditNow.Add(-time.Hour), func(x *audit.Record) { x.WorkspaceID = w.ID }))
	appendAll(t, r.global(t), arec("bbbb2222", auditNow.Add(-30*time.Minute)))
	return w, root
}

func TestAudit_ScopesWithWorkspacesOn(t *testing.T) {
	r := newAuditRig(t, "workspaces: true\n")
	w, root := wsSetup(t, r)

	// Default inside the workspace: its own trail.
	t.Chdir(filepath.Join(root, "src"))
	require.NoError(t, r.run(t, "json"))
	require.Equal(t, []string{"aaaa1111"}, ids(decodeAudit(t, r)))

	// Default outside any workspace: global.
	t.Chdir(r.home)
	require.NoError(t, r.run(t, "json"))
	require.Equal(t, []string{"bbbb2222"}, ids(decodeAudit(t, r)))

	// --ws by id and by path, from anywhere.
	require.NoError(t, r.run(t, "json", "--ws", w.ID))
	require.Equal(t, []string{"aaaa1111"}, ids(decodeAudit(t, r)))
	require.NoError(t, r.run(t, "json", "--ws", filepath.Join(root, "src")))
	require.Equal(t, []string{"aaaa1111"}, ids(decodeAudit(t, r)))

	// --all: both.
	require.NoError(t, r.run(t, "json", "--all"))
	require.Equal(t, []string{"bbbb2222", "aaaa1111"}, ids(decodeAudit(t, r)))

	// Unknown workspace.
	err := r.run(t, "txt", "--ws", "deadbeef")
	var ec core.ExitCoder
	require.ErrorAs(t, err, &ec)
	require.Equal(t, core.ExitSelectorNotFound, ec.ExitCode())
}

func TestAudit_WorkspacesOffIgnoresWorkspaceFiles(t *testing.T) {
	r := newAuditRig(t, "")
	_, root := wsSetup(t, r)
	t.Chdir(root)
	require.NoError(t, r.run(t, "json"))
	require.Equal(t, []string{"bbbb2222"}, ids(decodeAudit(t, r)), "off: no discovery, global only")
	require.NoError(t, r.run(t, "json", "--all"))
	require.Equal(t, []string{"bbbb2222"}, ids(decodeAudit(t, r)))
}

func TestAudit_AllSkipsUnsafeWorkspaceWithANote(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("modes")
	}
	r := newAuditRig(t, "workspaces: true\n")
	_, root := wsSetup(t, r)
	require.NoError(t, os.Chmod(filepath.Join(root, ".rex"), 0o777)) //nolint:gosec // test: the mode is what is under test
	t.Chdir(r.home)

	require.NoError(t, r.run(t, "text", "--all"))
	out := r.out.String()
	require.Contains(t, out, "skipped workspace")
	require.Contains(t, out, "job-bbbb")
	require.NotContains(t, out, "job-aaaa")

	// Inside the unsafe workspace the default falls back to global, with a note.
	t.Chdir(root)
	require.NoError(t, r.run(t, "text"))
	require.Contains(t, r.out.String(), "was not used")
}

func TestAudit_SymlinkedTrailIsRefusedNotFollowed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks")
	}
	r := newAuditRig(t, "")
	victim := filepath.Join(r.home, "victim.jsonl")
	appendAll(t, victim, arec("aaaa1111", auditNow))
	g := r.global(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(g), 0o700))
	require.NoError(t, os.Symlink(victim, g))

	require.NoError(t, r.run(t, "text"))
	out := r.out.String()
	require.Contains(t, out, "skipped")
	require.NotContains(t, out, "job-aaaa")
}

func TestAudit_FullPromptStaysOutOfTextButIsInJSON(t *testing.T) {
	r := newAuditRig(t, "")
	appendAll(t, r.global(t), arec("aaaa1111", auditNow, func(x *audit.Record) { x.Prompt = "line one\nsecret two" }))
	require.NoError(t, r.run(t, "text"))
	require.NotContains(t, r.out.String(), "secret two")
	require.NoError(t, r.run(t, "json"))
	require.Contains(t, strings.Join(func() []string {
		var ps []string
		for _, rec := range decodeAudit(t, r) {
			ps = append(ps, rec.Prompt)
		}
		return ps
	}(), "|"), "secret two")
}

func TestSpanText(t *testing.T) {
	for in, want := range map[int64]string{0: "0s", 59: "59s", 60: "1m", 3599: "59m", 3600: "1h00m", 11100: "3h05m"} {
		require.Equal(t, want, spanText(in))
	}
}
