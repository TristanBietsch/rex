package theme

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/testutil/testperm"
)

func writeTheme(t *testing.T, dir, file, src string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, file), []byte(src), 0o600))
}

func skipIfNoSymlinks(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
}

func issueFor(c *Catalog, file string) string {
	for _, i := range c.Issues {
		if i.File == file {
			return i.Msg
		}
	}
	return ""
}

func TestLoadDir_MissingAndEmptyPath(t *testing.T) {
	require.Empty(t, LoadDir("").UserNames())
	c := LoadDir(filepath.Join(t.TempDir(), "nope"))
	require.Empty(t, c.UserNames())
	require.Empty(t, c.Issues)
}

func TestLoadDir_LoadsYAMLOnly(t *testing.T) {
	dir := t.TempDir()
	writeTheme(t, dir, "b.yaml", `colors: {accent: "#EEEEEE"}`)
	writeTheme(t, dir, "a.yaml", "extends: paper")
	writeTheme(t, dir, "notes.txt", "hello")
	writeTheme(t, dir, "c.yml", "colors: {}")
	require.NoError(t, os.Mkdir(filepath.Join(dir, "sub"), 0o700))

	c := LoadDir(dir)
	require.Empty(t, c.Issues)
	require.Equal(t, []string{"a", "b"}, c.UserNames())
	require.True(t, c.IsUser("a"))
	require.False(t, c.IsUser("paper"))

	p, ok := c.Get("b")
	require.True(t, ok)
	require.Equal(t, "#EEEEEE", p.Accent)
	p.Fleet[0] = "mutated"
	q, _ := c.Get("b")
	require.NotEqual(t, "mutated", q.Fleet[0], "Get returns a copy")

	paper, ok := c.Get("paper")
	require.True(t, ok, "built-ins resolve through the catalog")
	require.Equal(t, Builtin["paper"].Bg, paper.Bg)
	d, ok := c.Get("ghost")
	require.False(t, ok)
	require.Equal(t, DefaultName, d.Name)
}

func TestNilCatalogIsBuiltinsOnly(t *testing.T) {
	var c *Catalog
	require.False(t, c.IsUser("x"))
	require.Nil(t, c.UserNames())
	require.Nil(t, c.Skipped())
	require.Empty(t, c.WhySkipped("x"))
	p, ok := c.Get("noir")
	require.True(t, ok)
	require.Equal(t, "noir", p.Name)
}

func TestLoadDir_BadFileDoesNotHideGoodOnes(t *testing.T) {
	dir := t.TempDir()
	writeTheme(t, dir, "good.yaml", `colors: {accent: "#EEEEEE"}`)
	writeTheme(t, dir, "bad.yaml", `colors: {accent: "nope"}`)
	writeTheme(t, dir, "noir.yaml", "extends: paper")
	writeTheme(t, dir, "Upper.yaml", "colors: {}")
	writeTheme(t, dir, "warn.yaml", "sparkle: 1")

	c := LoadDir(dir)
	require.Equal(t, []string{"good", "warn"}, c.UserNames())
	require.Contains(t, issueFor(c, "bad.yaml"), "colors.accent")
	require.Contains(t, issueFor(c, "noir.yaml"), "built-in")
	require.Contains(t, issueFor(c, "Upper.yaml"), "theme name")
	require.Contains(t, issueFor(c, "warn.yaml"), "unknown key")
	require.Len(t, c.Skipped(), 3, "a warning is not a skipped file")
	require.Contains(t, c.WhySkipped("bad"), "colors.accent")
	require.Empty(t, c.WhySkipped("good"))
	require.Empty(t, c.WhySkipped("warn"), "loaded with a warning")
	require.Empty(t, c.WhySkipped("ghost"))
	require.Equal(t, `"Upper.yaml": `+issueFor(c, "Upper.yaml"), c.Issues[0].String())
}

func TestLoadDir_FileLimits(t *testing.T) {
	dir := t.TempDir()
	writeTheme(t, dir, "big.yaml", "# "+strings.Repeat("x", MaxFileSize)+"\n")
	c := LoadDir(dir)
	require.Contains(t, issueFor(c, "big.yaml"), "larger than 64 KiB")

	many := t.TempDir()
	for i := 0; i <= MaxFiles; i++ {
		writeTheme(t, many, fmt.Sprintf("t%03d.yaml", i), "colors: {}")
	}
	c = LoadDir(many)
	require.Len(t, c.UserNames(), MaxFiles)
	require.Contains(t, issueFor(c, fmt.Sprintf("t%03d.yaml", MaxFiles)), "more than 64 theme files")
}

func TestLoadDir_SymlinkRules(t *testing.T) {
	skipIfNoSymlinks(t)
	if runtime.GOOS == "windows" {
		t.Skip("windows keeps links inside the directory")
	}
	base := t.TempDir()
	dir := filepath.Join(base, "themes")
	writeTheme(t, dir, "real.yaml", `colors: {accent: "#EEEEEE"}`)
	repo := filepath.Join(base, "dotfiles")
	writeTheme(t, repo, "stowed.yaml", `extends: paper`)
	require.NoError(t, os.Mkdir(filepath.Join(dir, "sub"), 0o700))

	require.NoError(t, os.Symlink("real.yaml", filepath.Join(dir, "inside.yaml")))
	require.NoError(t, os.Symlink("../dotfiles/stowed.yaml", filepath.Join(dir, "relative-out.yaml")))
	require.NoError(t, os.Symlink(filepath.Join(repo, "stowed.yaml"), filepath.Join(dir, "absolute-out.yaml")))
	require.NoError(t, os.Symlink("gone.yaml", filepath.Join(dir, "dangling.yaml")))
	require.NoError(t, os.Symlink("sub", filepath.Join(dir, "dirlink.yaml")))
	require.NoError(t, os.Symlink("loop.yaml", filepath.Join(dir, "loop.yaml")))
	writeTheme(t, repo, "open.yaml", "colors: {}")
	testperm.Loosen(t, filepath.Join(repo, "open.yaml"), 0o666)
	require.NoError(t, os.Symlink("../dotfiles/open.yaml", filepath.Join(dir, "writable-target.yaml")))
	writeTheme(t, repo, "group.yaml", "colors: {}")
	testperm.Loosen(t, filepath.Join(repo, "group.yaml"), 0o660)
	require.NoError(t, os.Symlink("../dotfiles/group.yaml", filepath.Join(dir, "group-target.yaml")))

	c := LoadDir(dir)
	require.Equal(t, []string{"absolute-out", "inside", "real", "relative-out"}, c.UserNames(),
		"a link loads when its target is a safe regular file, wherever it is")
	paper, _ := c.Get("relative-out")
	require.Equal(t, Builtin["paper"].Bg, paper.Bg, "the target's content is what loads")
	require.Contains(t, issueFor(c, "dangling.yaml"), "cannot open file")
	require.Contains(t, issueFor(c, "loop.yaml"), "cannot open file")
	require.Contains(t, issueFor(c, "dirlink.yaml"), "not a regular file")
	require.Contains(t, issueFor(c, "writable-target.yaml"), "writable by group or others")
	require.Contains(t, issueFor(c, "group-target.yaml"), "writable by group or others")
}

func TestLoadDir_LinkedTargetOverTheSizeCapIsSkipped(t *testing.T) {
	skipIfNoSymlinks(t)
	if runtime.GOOS == "windows" {
		t.Skip("windows keeps links inside the directory")
	}
	base := t.TempDir()
	dir := filepath.Join(base, "themes")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	writeTheme(t, base, "big.yaml", "# "+strings.Repeat("x", MaxFileSize)+"\n")
	require.NoError(t, os.Symlink("../big.yaml", filepath.Join(dir, "big.yaml")))
	c := LoadDir(dir)
	require.Contains(t, issueFor(c, "big.yaml"), "larger than 64 KiB")
}

func TestLoadDir_ReadFailureIsReported(t *testing.T) {
	dir := t.TempDir()
	writeTheme(t, dir, "a.yaml", "colors: {}")
	prev := readAll
	t.Cleanup(func() { readAll = prev })
	readAll = func(io.Reader) ([]byte, error) { return nil, errors.New("disk on fire") }
	c := LoadDir(dir)
	require.Contains(t, issueFor(c, "a.yaml"), "cannot read file: disk on fire")
}

func TestLoadDir_NotADirectory(t *testing.T) {
	f := filepath.Join(t.TempDir(), "themes")
	require.NoError(t, os.WriteFile(f, nil, 0o600))
	c := LoadDir(f)
	require.Equal(t, "not a directory", issueFor(c, "themes"))
}

func TestLoadDir_StatError(t *testing.T) {
	skipIfNoSymlinks(t)
	loop := filepath.Join(t.TempDir(), "loop")
	require.NoError(t, os.Symlink("loop", loop))
	c := LoadDir(loop)
	require.Contains(t, issueFor(c, "loop"), "cannot read the themes directory")
}

func TestLoadDir_UnreadableDirectory(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a non-root unix user")
	}
	dir := filepath.Join(t.TempDir(), "themes")
	writeTheme(t, dir, "a.yaml", "colors: {}")
	testperm.Loosen(t, dir, 0o300)
	t.Cleanup(func() { testperm.Loosen(t, dir, 0o700) })
	c := LoadDir(dir)
	require.Contains(t, issueFor(c, "themes"), "cannot read the themes directory")
	require.Empty(t, c.UserNames())
}

func TestLoadDir_UnreadableFile(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a non-root unix user")
	}
	dir := t.TempDir()
	writeTheme(t, dir, "a.yaml", "colors: {}")
	require.NoError(t, os.Chmod(filepath.Join(dir, "a.yaml"), 0))
	c := LoadDir(dir)
	require.Contains(t, issueFor(c, "a.yaml"), "cannot open file")
}

func TestLoadDir_RefusesWritableFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows modes carry no group/other bits")
	}
	dir := t.TempDir()
	testperm.WriteFile(t, filepath.Join(dir, "open.yaml"), []byte("colors: {}"), 0o666)
	testperm.WriteFile(t, filepath.Join(dir, "group.yaml"), []byte("colors: {}"), 0o660)
	testperm.WriteFile(t, filepath.Join(dir, "ok.yaml"), []byte("colors: {}"), 0o644)
	c := LoadDir(dir)
	require.Equal(t, []string{"ok"}, c.UserNames())
	require.Contains(t, issueFor(c, "open.yaml"), "writable by group or others")
	require.Contains(t, issueFor(c, "open.yaml"), "chmod go-w")
	require.Contains(t, issueFor(c, "group.yaml"), "writable by group or others")
}

func TestLoadDir_RefusesWritableDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows modes carry no group/other bits")
	}
	dir := filepath.Join(t.TempDir(), "themes")
	writeTheme(t, dir, "a.yaml", "colors: {}")
	testperm.Loosen(t, dir, 0o777)
	c := LoadDir(dir)
	require.Empty(t, c.UserNames())
	require.Contains(t, issueFor(c, "themes"), "themes directory: writable by group or others")
	require.Contains(t, issueFor(c, "themes"), "no user themes loaded")
}

func TestStampChangesWithTheFiles(t *testing.T) {
	dir := t.TempDir()
	require.Equal(t, "", Stamp(filepath.Join(dir, "none")))
	require.Equal(t, "", Stamp(dir))
	writeTheme(t, dir, "a.yaml", "colors: {}")
	writeTheme(t, dir, "ignored.txt", "x")
	s1 := Stamp(dir)
	require.NotEmpty(t, s1)
	require.Equal(t, s1, Stamp(dir))

	later := time.Now().Add(time.Hour)
	require.NoError(t, os.Chtimes(filepath.Join(dir, "a.yaml"), later, later))
	s2 := Stamp(dir)
	require.NotEqual(t, s1, s2, "an edit")
	writeTheme(t, dir, "b.yaml", "colors: {}")
	require.NotEqual(t, s2, Stamp(dir), "an added file")
}

func TestStampDanglingLink(t *testing.T) {
	skipIfNoSymlinks(t)
	dir := t.TempDir()
	require.NoError(t, os.Symlink("gone.yaml", filepath.Join(dir, "x.yaml")))
	require.Equal(t, "x.yaml:?;", Stamp(dir))
}

func TestLoadDir_IssuesCarryNoControlCharacters(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows file names cannot hold control characters")
	}
	dir := t.TempDir()
	writeTheme(t, dir, "a\x1b[2Jb.yaml", "colors: {}")
	writeTheme(t, dir, "ok.yaml", "colors: {fleet: [\"\\u001b[2J\"], accent: \"\\u001b]0;x\\u0007\"}")
	writeTheme(t, dir, "v.yaml", "colors: {accent: [1]}")
	writeTheme(t, dir, "z.yaml", "sparkle\u202e: 1")
	c := LoadDir(dir)
	require.GreaterOrEqual(t, len(c.Issues), 4)
	for _, i := range c.Issues {
		for _, bad := range []string{"\x1b", "\x07", "\u202e"} {
			require.NotContains(t, i.String(), bad, "%q", i.String())
		}
	}
	require.Contains(t, c.Issues[0].String(), `"a\x1b[2Jb.yaml"`, "the file name is quoted")
	require.Contains(t, issueFor(c, "v.yaml"), "[]interface {} is not a color", "the type, not the value")
	require.Equal(t, "a?b?c d?", printable("a\x1bb\u202ec d\x00"))
}
