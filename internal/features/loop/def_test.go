package loop

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/testutil/testperm"
)

const goodYAML = `name: fix
kind: repeat
tool: claude
model: sonnet
prompt: "fix the build"
until:
  check: ["make", "check"]
`

func TestParse_Defaults(t *testing.T) {
	d, err := Parse([]byte(goodYAML), "fix")
	require.NoError(t, err)
	require.Equal(t, "fix", d.Name)
	require.Equal(t, KindRepeat, d.Kind)
	require.Equal(t, "fix the build", d.Prompt)
	require.Equal(t, "fix the build", d.FollowupPrompt, "followup defaults to prompt")
	require.Equal(t, []string{"make", "check"}, d.Until.Check)
	require.Equal(t, 15*time.Minute, d.Until.CheckTimeout)
	require.Equal(t, 10, d.Until.MaxIterations)
	require.Equal(t, 120, d.Until.MaxMinutes)
	require.Empty(t, d.Until.Marker)
}

func TestParse_AllFields(t *testing.T) {
	d, err := Parse([]byte(`
name: full
kind: repeat
tool: claude
model: opus
effort: high
cwd: /work/app
prompt: "start"
followup_prompt: "keep going"
until:
  check: ["go", "test", "./..."]
  check_timeout: 2h
  marker: '<promise>DONE</promise>'
  max_iterations: 100
  max_minutes: 1440
`), "full")
	require.NoError(t, err)
	require.Equal(t, "high", d.Effort)
	require.Equal(t, "/work/app", d.CWD)
	require.Equal(t, "keep going", d.FollowupPrompt)
	require.Equal(t, 2*time.Hour, d.Until.CheckTimeout)
	require.Equal(t, 100, d.Until.MaxIterations)
	require.Equal(t, 1440, d.Until.MaxMinutes)
	require.Equal(t, "<promise>DONE</promise>", d.Until.Marker)
}

func TestParse_Table(t *testing.T) {
	head := "name: x\nkind: repeat\ntool: claude\nmodel: sonnet\nprompt: go\n"
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{"shell string check", head + "until:\n  check: make check\n", "list of arguments"},
		{"single arg with space", head + "until:\n  check: [\"make check\"]\n", "shell string"},
		{"empty argv", head + "until:\n  check: []\n  marker: x\n", "check must not be empty"},
		{"empty first arg", head + "until:\n  check: [\"\"]\n", "empty"},
		{"nested check", head + "until:\n  check: [[a]]\n", "text"},
		{"no stop condition", head + "until:\n  max_iterations: 3\n", "check or marker"},
		{"no until", head, "check or marker"},
		{"iterations over cap", head + "until:\n  check: [true]\n  max_iterations: 101\n", "max_iterations"},
		{"iterations zero", head + "until:\n  check: [true]\n  max_iterations: 0\n", "max_iterations"},
		{"minutes over cap", head + "until:\n  check: [true]\n  max_minutes: 1441\n", "max_minutes"},
		{"timeout over cap", head + "until:\n  check: [true]\n  check_timeout: 2h1m\n", "check_timeout"},
		{"timeout bad", head + "until:\n  check: [true]\n  check_timeout: soon\n", "check_timeout"},
		{"timeout zero", head + "until:\n  check: [true]\n  check_timeout: 0s\n", "check_timeout"},
		{"bad marker regex", head + "until:\n  marker: '('\n", "marker"},
		{"marker matches empty", head + "until:\n  marker: 'x*'\n", "empty"},
		{"marker in prompt line", "name: x\nkind: repeat\ntool: claude\nmodel: sonnet\nprompt: |\n  say it:\n  DONE\nuntil:\n  marker: DONE\n", "prompt"},
		{"chain kind", strings.Replace(head, "repeat", "chain", 1) + "until:\n  check: [true]\n", "chain"},
		{"unknown kind", strings.Replace(head, "repeat", "spin", 1) + "until:\n  check: [true]\n", "kind"},
		{"missing kind", "name: x\ntool: claude\nmodel: sonnet\nprompt: go\nuntil:\n  check: [true]\n", "kind"},
		{"name mismatch", strings.Replace(head, "name: x", "name: y", 1) + "until:\n  check: [true]\n", "file name"},
		{"bad name", "name: ../x\nkind: repeat\ntool: claude\nmodel: sonnet\nprompt: go\nuntil:\n  check: [true]\n", "name"},
		{"no tool", "name: x\nkind: repeat\nmodel: sonnet\nprompt: go\nuntil:\n  check: [true]\n", "tool"},
		{"no model", "name: x\nkind: repeat\ntool: claude\nprompt: go\nuntil:\n  check: [true]\n", "model"},
		{"no prompt", "name: x\nkind: repeat\ntool: claude\nmodel: sonnet\nuntil:\n  check: [true]\n", "prompt"},
		{"relative cwd", head + "cwd: work\nuntil:\n  check: [true]\n", "cwd"},
		{"dotdot cwd", head + "cwd: /a/../b\nuntil:\n  check: [true]\n", "cwd"},
		{"unknown field", head + "shell: true\nuntil:\n  check: [true]\n", "shell"},
		{"not yaml", "name: [", "yaml"},
		{"multi document", head + "until:\n  check: [true]\n---\nname: z\n", "one document"},
		{"too big", head + "until:\n  check: [true]\n#" + strings.Repeat("x", MaxFileBytes), "too large"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.yaml), "x")
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestParse_MarkerOnly(t *testing.T) {
	d, err := Parse([]byte("name: x\nkind: repeat\ntool: claude\nmodel: sonnet\nprompt: go\nuntil:\n  marker: 'ALL DONE'\n"), "x")
	require.NoError(t, err)
	require.Empty(t, d.Until.Check)
}

func writeLoop(t *testing.T, dir, name, body string, mode os.FileMode) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o700))
	p := filepath.Join(dir, name+".yaml")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	testperm.Loosen(t, p, mode)
	return p
}

func TestLoadFile_OK(t *testing.T) {
	dir := t.TempDir()
	writeLoop(t, dir, "fix", goodYAML, 0o600)
	d, err := LoadFile(dir, "fix")
	require.NoError(t, err)
	require.Equal(t, "fix", d.Name)
}

func TestLoadFile_RefusesUnsafe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mode and symlink checks are unix only")
	}
	dir := t.TempDir()
	writeLoop(t, dir, "wide", goodYAML, 0o666)
	writeLoop(t, dir, "grp", goodYAML, 0o620)
	_, err := LoadFile(dir, "wide")
	require.ErrorContains(t, err, "writable")
	_, err = LoadFile(dir, "grp")
	require.ErrorContains(t, err, "writable")

	real := writeLoop(t, t.TempDir(), "real", goodYAML, 0o600)
	require.NoError(t, os.Symlink(real, filepath.Join(dir, "link.yaml")))
	_, err = LoadFile(dir, "link")
	require.ErrorContains(t, err, "symlink")

	require.NoError(t, os.Mkdir(filepath.Join(dir, "adir.yaml"), 0o700))
	_, err = LoadFile(dir, "adir")
	require.ErrorContains(t, err, "regular file")

	_, err = LoadFile(dir, "missing")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestLoadFile_RefusesBadName(t *testing.T) {
	for _, n := range []string{"", "../x", "a/b", ".hidden", "A", "x y", strings.Repeat("a", 65), "x.yaml"} {
		_, err := LoadFile(t.TempDir(), n)
		require.Error(t, err, n)
	}
}

func TestList(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "loops")
	got, err := List(dir)
	require.NoError(t, err)
	require.Empty(t, got, "missing dir lists nothing")

	writeLoop(t, dir, "good", strings.Replace(goodYAML, "name: fix", "name: good", 1), 0o600)
	writeLoop(t, dir, "bad", "name: bad\n", 0o600)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o600))
	got, err = List(dir)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "bad", got[0].Name)
	require.Error(t, got[0].Err)
	require.Equal(t, "good", got[1].Name)
	require.NoError(t, got[1].Err)
	require.Equal(t, "good", got[1].Def.Name)
}

func TestStarter_Parses(t *testing.T) {
	d, err := Parse([]byte(Starter("my-loop")), "my-loop")
	require.NoError(t, err)
	require.Equal(t, "my-loop", d.Name)
}

func TestValidName(t *testing.T) {
	require.True(t, ValidName("fix-until.green_2"))
	require.False(t, ValidName("Fix"))
}
