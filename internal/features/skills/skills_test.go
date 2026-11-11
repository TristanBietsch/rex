package skills

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeSkill(t *testing.T, lib, name, body string) {
	t.Helper()
	dir := filepath.Join(lib, name)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, SkillFile), []byte(body), 0o600))
}

func TestValidName(t *testing.T) {
	for _, ok := range []string{"a", "review", "go-test", "0day", strings.Repeat("a", 64)} {
		require.True(t, ValidName(ok), ok)
	}
	for _, bad := range []string{"", "-a", "A", "a_b", "a b", "a/b", "../x", ".", "..", "a.b", strings.Repeat("a", 65), "é", "a\n"} {
		require.False(t, ValidName(bad), bad)
	}
}

func FuzzValidName(f *testing.F) {
	for _, s := range []string{"a", "../x", "a/b", "", "go-test", "a\x00"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if !ValidName(s) {
			return
		}
		// A valid name is one plain path element.
		require.Equal(t, s, filepath.Base(s))
		require.NotContains(t, s, "/")
		require.NotContains(t, s, `\`)
		require.NotContains(t, s, "..")
		require.LessOrEqual(t, len(s), 64)
	})
}

func TestList(t *testing.T) {
	lib := t.TempDir()
	writeSkill(t, lib, "review", "---\nname: review\ndescription: Review a diff\n---\n# body\n")
	writeSkill(t, lib, "alpha", "no frontmatter\n")
	writeSkill(t, lib, "Bad_Name", "---\ndescription: x\n---\n")
	require.NoError(t, os.MkdirAll(filepath.Join(lib, "nofile"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(lib, "stray.txt"), []byte("x"), 0o600))
	writeSkill(t, lib, "esc", "---\ndescription: \"a\\u001b[31mred\\nline\"\n---\n")
	if runtime.GOOS != "windows" {
		target := t.TempDir()
		writeSkill(t, target, "linked", "---\ndescription: via link\n---\n")
		require.NoError(t, os.Symlink(filepath.Join(target, "linked"), filepath.Join(lib, "linked")))
	}

	got, errs := List(lib)
	names := make([]string, len(got))
	for i, s := range got {
		names[i] = s.Name
	}
	require.Equal(t, []string{"alpha", "esc", "review"}, names)
	require.Equal(t, "Review a diff", got[2].Description)
	require.Empty(t, got[0].Description)
	require.NotContains(t, got[1].Description, "\x1b")
	require.NotContains(t, got[1].Description, "\n")
	require.GreaterOrEqual(t, len(errs), 2, "bad name and missing SKILL.md are reported")
}

func TestList_MissingDirIsEmpty(t *testing.T) {
	got, errs := List(filepath.Join(t.TempDir(), "nope"))
	require.Empty(t, got)
	require.Empty(t, errs)
}

func TestParseDescription(t *testing.T) {
	require.Equal(t, "x", parseDescription([]byte("---\r\ndescription: x\r\n---\r\nbody")))
	require.Empty(t, parseDescription([]byte("---\ndescription: x\n")), "unterminated")
	require.Empty(t, parseDescription([]byte("---\n: : :\n---\n")), "bad yaml")
	long := parseDescription([]byte("---\ndescription: " + strings.Repeat("w", 500) + "\n---\n"))
	require.LessOrEqual(t, len([]rune(long)), maxDescription)
}
