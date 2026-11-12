package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func sessionTrustTool() registry.Tool {
	return registry.Tool{
		ID:          "claude",
		TrustPrompt: &registry.TrustPrompt{Shown: "s", Selected: "t", Move: "\x1b[B", Confirm: "\r"},
		TrustArgs:   []string{"--trust", registry.TrustArgsCWDList},
	}
}

func TestTrustDecision_ExplicitSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	project := filepath.Join(home, "dev", "project")
	require.NoError(t, os.MkdirAll(project, 0o700))
	homeLink := filepath.Join(t.TempDir(), "home-link")
	require.NoError(t, os.Symlink(home, homeLink))
	tool := sessionTrustTool()
	srv := &Server{} // no auto_trust_dirs at all

	d := srv.trustDecisionFor(tool, project, true)
	require.True(t, d.Trusted)
	require.NotNil(t, d.Answer)
	require.NotEmpty(t, d.Args)
	require.Contains(t, d.Args[1], project)

	d = srv.trustDecisionFor(tool, project, false)
	require.False(t, d.Trusted, "not asked for, not listed")
	require.Nil(t, d.Answer)
	require.Nil(t, d.Args)

	sep := string(filepath.Separator)
	root := filepath.VolumeName(home) + sep
	for name, cwd := range map[string]string{
		"home":        home,
		"home link":   homeLink,
		"root":        root,
		"dot-dot":     project + sep + ".." + sep + "..",
		"relative":    "dev/project",
		"tilde":       "~/dev/project",
		"blank":       "",
		"through ..":  filepath.Join(project, "x") + sep + ".." + sep + "y",
		"dot-dot end": project + sep + "..",
	} {
		d := srv.trustDecisionFor(tool, cwd, true)
		require.False(t, d.Trusted, name)
		require.Nil(t, d.Answer, name)
		require.Nil(t, d.Args, name)
	}
}

// A listed folder keeps its own reason; the explicit flag adds nothing there.
func TestTrustDecision_ExplicitInListedFolder(t *testing.T) {
	dir := t.TempDir()
	srv := &Server{cfg: Config{AutoTrustDirs: func() []string { return []string{dir} }}}
	d := srv.trustDecisionFor(sessionTrustTool(), dir, true)
	require.True(t, d.Trusted)
	require.NotNil(t, d.Answer)
	require.NotEmpty(t, d.Args)
}

func TestValidateNewSession_TrustFolder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	project := filepath.Join(home, "dev", "project")
	require.NoError(t, os.MkdirAll(project, 0o700))
	sep := string(filepath.Separator)
	base := protocol.NewSession{ToolID: "echo", ModelID: "short", Slug: "x"}

	ok := base
	ok.CWD, ok.TrustFolder = project, true
	require.NoError(t, validateNewSession(ok))

	untrusted := base
	untrusted.CWD = home
	require.NoError(t, validateNewSession(untrusted), "no trust requested: the folder is not checked")

	for name, cwd := range map[string]string{
		"home":     home,
		"root":     filepath.VolumeName(home) + sep,
		"dot-dot":  project + sep + ".." + sep + "x",
		"relative": "dev/project",
		"tilde":    "~/dev",
	} {
		p := base
		p.CWD, p.TrustFolder = cwd, true
		err := validateNewSession(p)
		require.Error(t, err, name)
		// Relative and ~ paths fail the absolute-cwd check first (H5); the
		// rest reach the trust check.
		require.Regexp(t, `trust_folder|absolute path`, err.Error(), name)
	}
}

const sessionTrustTools = `tools:
  - id: echo
    skip_permissions_args: ["--skip-it"]
    resume:
      args: ["--resume", "{id}"]
    models:
      - id: argv
        name: "Prints its argv"
        args: ['echo "ARGV: $0 $*"; sleep 30']
`

// A session trusted for itself keeps that trust across Resume, with no
// auto_trust_dirs: skip_permissions reaches argv for it and for nothing else.
func TestResume_SessionTrustedFolderKeepsTrust(t *testing.T) {
	for _, tc := range []struct {
		name         string
		trustFolder  bool
		wantSkipFlag bool
	}{
		{"trusted for the session", true, true},
		{"not trusted", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := startResumeServer(t, sessionTrustTools, 0)
			e.srv.cfg.SkipPermissions = func() bool { return true }
			s := exited(func() string { return e.dir }, "a", "done", "argv", testResumeID)
			s.TrustFolder = tc.trustFolder
			require.NoError(t, e.store.Add(s))
			conn, w, r := e.dial(t)
			code, msg := call(t, conn, w, r, "Resume", map[string]string{"session_id": "a"})
			require.Empty(t, code, msg)
			require.Eventually(t, func() bool { return strings.Contains(e.transcript("a"), "ARGV:") }, 10e9, 20e6)
			tr := e.transcript("a")
			require.Equal(t, tc.wantSkipFlag, strings.Contains(tr, "--skip-it"), tr)
			require.NoError(t, e.srv.deleteSession("a", "user"))
		})
	}
}

// The stored folder is checked again on resume: a meta.json edited to trust
// the home folder does not get skip_permissions.
func TestResume_SessionTrustRevalidatesStoredFolder(t *testing.T) {
	e := startResumeServer(t, sessionTrustTools, 0)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	e.srv.cfg.SkipPermissions = func() bool { return true }
	s := exited(func() string { return home }, "a", "done", "argv", testResumeID)
	s.TrustFolder = true
	require.NoError(t, e.store.Add(s))
	conn, w, r := e.dial(t)
	code, msg := call(t, conn, w, r, "Resume", map[string]string{"session_id": "a"})
	require.Empty(t, code, msg)
	require.Eventually(t, func() bool { return strings.Contains(e.transcript("a"), "ARGV:") }, 10e9, 20e6)
	require.NotContains(t, e.transcript("a"), "--skip-it")
	require.NoError(t, e.srv.deleteSession("a", "user"))
}

// NewSession with trust_folder: the session is trusted without
// auto_trust_dirs, and the choice is recorded for Resume.
func TestNewSession_TrustFolderPersistsAndTrusts(t *testing.T) {
	e := startResumeServer(t, sessionTrustTools, 0)
	e.srv.cfg.SkipPermissions = func() bool { return true }
	conn, w, r := e.dial(t)
	code, msg := call(t, conn, w, r, "NewSession", protocol.NewSession{
		ToolID: "echo", ModelID: "argv", Slug: "tf", CWD: e.dir, TrustFolder: true,
	})
	require.Empty(t, code, msg)
	var id string
	for _, sum := range e.store.Snapshot() {
		if sum.Slug == "tf" {
			require.True(t, sum.TrustFolder)
			id = sum.ID
		}
	}
	require.NotEmpty(t, id)
	require.Eventually(t, func() bool { return strings.Contains(e.transcript(id), "ARGV:") }, 10e9, 20e6)
	require.Contains(t, e.transcript(id), "--skip-it")
	require.NoError(t, e.srv.deleteSession(id, "user"))

	code, msg = call(t, conn, w, r, "NewSession", protocol.NewSession{
		ToolID: "echo", ModelID: "argv", Slug: "bad", CWD: "relative/dir", TrustFolder: true,
	})
	require.Equal(t, protocol.ErrCodeBadIntent, code)
	require.Contains(t, msg, "absolute path", "a relative cwd is refused before the trust check")
}

// Over the wire, trust_folder for the home folder is a bad intent and starts
// nothing.
func TestNewSession_TrustFolderHomeIsBadIntent(t *testing.T) {
	e := startResumeServer(t, sessionTrustTools, 0)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	conn, w, r := e.dial(t)
	code, msg := call(t, conn, w, r, "NewSession", protocol.NewSession{
		ToolID: "echo", ModelID: "argv", Slug: "home", CWD: home, TrustFolder: true,
	})
	require.Equal(t, protocol.ErrCodeBadIntent, code, msg)
	require.Contains(t, msg, "trust_folder")
	for _, sum := range e.store.Snapshot() {
		require.NotEqual(t, "home", sum.Slug, "nothing was started")
	}
}
