package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
)

func TestUnderAutoTrustDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dev := filepath.Join(home, "dev")
	project := filepath.Join(dev, "project")
	require.NoError(t, os.MkdirAll(project, 0o700))
	devil := filepath.Join(home, "devil")
	require.NoError(t, os.MkdirAll(devil, 0o700))
	link := filepath.Join(home, "link")
	require.NoError(t, os.Symlink(dev, link))
	// A link inside the trusted folder that points outside it.
	outside := filepath.Join(home, "outside", "y")
	require.NoError(t, os.MkdirAll(outside, 0o700))
	escape := filepath.Join(dev, "escape")
	require.NoError(t, os.Symlink(outside, escape))
	sep := string(filepath.Separator)

	cases := []struct {
		name string
		cwd  string
		dirs []string
		want bool
	}{
		{"exact folder", dev, []string{dev}, true},
		{"subfolder", project, []string{dev}, true},
		{"sibling with a shared prefix", devil, []string{dev}, false},
		{"tilde entry", project, []string{"~/dev"}, true},
		{"home itself is ignored", project, []string{"~/"}, false},
		{"bare tilde is ignored", project, []string{"~"}, false},
		{"home by absolute path is ignored", project, []string{home}, false},
		{"link out of the trusted folder", escape, []string{dev}, false},
		// Built by hand: filepath.Join would clean the ".." away.
		{"dot-dot after a link is refused", dev + sep + "escape" + sep + ".." + sep + "x", []string{dev}, false},
		{"dot-dot in an entry is refused", project, []string{dev + sep + "project" + sep + ".."}, false},
		{"cwd through a symlink", filepath.Join(link, "project"), []string{dev}, true},
		{"entry through a symlink", project, []string{link}, true},
		{"root never counts", project, []string{string(filepath.Separator)}, false},
		{"relative entry ignored", project, []string{"dev"}, false},
		{"relative cwd ignored", "dev/project", []string{dev}, false},
		{"blank entries ignored", project, []string{"", "  "}, false},
		{"no entries", project, nil, false},
		{"second entry matches", project, []string{devil, " " + dev + " "}, true},
		{"folder not created yet", filepath.Join(link, "new", "deeper"), []string{dev}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, got := underAutoTrustDir(tc.cwd, tc.dirs)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestTrustAnswerFor(t *testing.T) {
	dir := t.TempDir()
	tool := registry.Tool{ID: "claude", TrustPrompt: &registry.TrustPrompt{Shown: "s", Selected: "t", Move: "\x1b[B", Confirm: "\r"}}

	off := &Server{cfg: Config{}}
	require.Nil(t, off.trustAnswerFor(tool, dir), "no AutoTrustDirs: never")

	on := &Server{cfg: Config{AutoTrustDirs: func() []string { return []string{dir} }}}
	a := on.trustAnswerFor(tool, filepath.Join(dir, "x"))
	require.NotNil(t, a)
	require.Equal(t, "\x1b[B", string(a.Move))
	require.Nil(t, on.trustAnswerFor(tool, t.TempDir()), "folder not listed")

	noPrompt := registry.Tool{ID: "codex"}
	require.Nil(t, on.trustAnswerFor(noPrompt, dir), "tool without a trust_prompt")
}

func TestTrustArgsFor(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	project := filepath.Join(real, "project")
	require.NoError(t, os.MkdirAll(project, 0o700))
	link := filepath.Join(root, "link")
	require.NoError(t, os.Symlink(real, link))
	tool := registry.Tool{ID: "antigravity", TrustArgs: []string{"--settings", `{"trustedWorkspaces":{cwd_list}}`}}

	on := &Server{cfg: Config{AutoTrustDirs: func() []string { return []string{real} }}}
	args := on.trustArgsFor(tool, filepath.Join(link, "project"))
	require.Len(t, args, 2)
	require.Equal(t, "--settings", args[0])
	var settings struct {
		TrustedWorkspaces []string `json:"trustedWorkspaces"`
	}
	require.NoError(t, json.Unmarshal([]byte(args[1]), &settings), "valid session settings JSON")
	resolved, err := filepath.EvalSymlinks(project)
	require.NoError(t, err)
	require.Equal(t, []string{filepath.Join(link, "project"), resolved}, settings.TrustedWorkspaces,
		"the folder as given and resolved")

	require.Nil(t, on.trustArgsFor(tool, t.TempDir()), "folder not listed")
	require.Nil(t, (&Server{cfg: Config{}}).trustArgsFor(tool, project), "no setting")
	require.Nil(t, on.trustArgsFor(registry.Tool{ID: "codex"}, project), "tool without trust_args")

	// A folder whose name needs JSON escaping stays one valid entry.
	odd := filepath.Join(real, `quote"and\slash`)
	require.NoError(t, os.MkdirAll(odd, 0o700))
	args = on.trustArgsFor(tool, odd)
	require.NoError(t, json.Unmarshal([]byte(args[1]), &settings))
	require.Contains(t, settings.TrustedWorkspaces, odd)
}
