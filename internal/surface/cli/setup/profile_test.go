package setup

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// testStamp is the fixed clock profile backups are named after in tests.
var testStamp = time.Unix(1700000000, 0)

func TestStripBlockFromFile_basic(t *testing.T) {
	input := `export PATH="$HOME/.local/bin:$PATH"
# BEGIN REX
export REX_SOCKET=/tmp/rex.sock
alias rex="rex --color"
# END REX
# some other config
export EDITOR=vim
`
	want := `export PATH="$HOME/.local/bin:$PATH"
# some other config
export EDITOR=vim
`
	modified, content := runStrip(t, input)
	if !modified {
		t.Fatal("expected modified=true")
	}
	if content != want {
		t.Fatalf("got:\n%q\nwant:\n%q", content, want)
	}
}

func TestStripBlockFromFile_caseInsensitive(t *testing.T) {
	input := "before\n# Begin Rex\nstuff\n# End Rex\nafter\n"
	want := "before\nafter\n"
	modified, content := runStrip(t, input)
	if !modified {
		t.Fatal("expected modified=true")
	}
	if content != want {
		t.Fatalf("got:\n%q\nwant:\n%q", content, want)
	}
}

func TestStripBlockFromFile_noBlock(t *testing.T) {
	input := "no rex block here\nexport FOO=bar\n"
	modified, content := runStrip(t, input)
	if modified {
		t.Fatal("expected modified=false when no block present")
	}
	require.Equal(t, input, content)
}

// Two sequential blocks (e.g. setup run, then install.sh --shell-init
// appended again) are both removed.
func TestStripBlockFromFile_multipleBlocks(t *testing.T) {
	input := "a\n# BEGIN REX\nb\n# END REX\nc\n# BEGIN REX\nd\n# END REX\ne\n"
	want := "a\nc\ne\n"
	modified, content := runStrip(t, input)
	if !modified {
		t.Fatal("expected modified=true")
	}
	if content != want {
		t.Fatalf("got:\n%q\nwant:\n%q", content, want)
	}
}

func TestStripBlockFromFile_nonexistent(t *testing.T) {
	backup, err := stripBlockFromFile(filepath.Join(t.TempDir(), "missing"), testStamp)
	require.NoError(t, err)
	require.Empty(t, backup, "a missing profile must not be reported as modified")
}

// P0-09: a BEGIN with no END used to drop every line to EOF and then
// atomically replace the profile with the truncated copy.
func TestStripBlockFromFile_unterminatedBlockLeavesFileUntouched(t *testing.T) {
	input := "export A=1\n# BEGIN REX\nexport PATH=x\nexport EDITOR=vim\nalias ll='ls -l'\n"
	p := writeProfile(t, input, 0o644)

	backup, err := stripBlockFromFile(p, testStamp)
	require.ErrorIs(t, err, errUnterminatedBlock)
	require.Contains(t, err.Error(), "line 2")
	require.Empty(t, backup)
	requireFileBytes(t, p, input)
	requireNoBackups(t, filepath.Dir(p))
}

func TestStripBlockFromFile_nestedBlockLeavesFileUntouched(t *testing.T) {
	input := "a\n# BEGIN REX\n# BEGIN REX\nb\n# END REX\n# END REX\nc\n"
	p := writeProfile(t, input, 0o644)

	_, err := stripBlockFromFile(p, testStamp)
	require.ErrorIs(t, err, errNestedBlock)
	requireFileBytes(t, p, input)
	requireNoBackups(t, filepath.Dir(p))
}

func TestStripBlockFromFile_unmatchedEndLeavesFileUntouched(t *testing.T) {
	input := "a\n# BEGIN REX\nb\n# END REX\n# END REX\nc\n"
	p := writeProfile(t, input, 0o644)

	_, err := stripBlockFromFile(p, testStamp)
	require.ErrorIs(t, err, errUnmatchedEnd)
	requireFileBytes(t, p, input)
	requireNoBackups(t, filepath.Dir(p))
}

// CRLF endings outside the block survive byte for byte.
func TestStripBlockFromFile_CRLF(t *testing.T) {
	input := "a\r\n# BEGIN REX\r\nb\r\n# END REX\r\nc\r\n"
	p := writeProfile(t, input, 0o644)

	_, err := stripBlockFromFile(p, testStamp)
	require.NoError(t, err)
	requireFileBytes(t, p, "a\r\nc\r\n")
}

// A profile whose last line has no newline keeps it that way.
func TestStripBlockFromFile_noTrailingNewline(t *testing.T) {
	p := writeProfile(t, "a\n# BEGIN REX\nb\n# END REX\nlast", 0o644)

	_, err := stripBlockFromFile(p, testStamp)
	require.NoError(t, err)
	requireFileBytes(t, p, "a\nlast")
}

// bufio.Scanner's default 64 KiB token limit made the old stripper fail on
// long lines (generated completion scripts, base64 blobs).
func TestStripBlockFromFile_lineOver64KiB(t *testing.T) {
	long := "export BLOB=" + strings.Repeat("x", 100*1024)
	input := long + "\n# BEGIN REX\nb\n# END REX\n" + long + "\n"
	p := writeProfile(t, input, 0o644)

	_, err := stripBlockFromFile(p, testStamp)
	require.NoError(t, err)
	requireFileBytes(t, p, long+"\n"+long+"\n")
}

func TestStripBlockFromFile_writesBackup(t *testing.T) {
	input := "a\n# BEGIN REX\nb\n# END REX\nc\n"
	p := writeProfile(t, input, 0o640)

	backup, err := stripBlockFromFile(p, testStamp)
	require.NoError(t, err)
	require.Equal(t, p+".rex-backup-1700000000", backup)
	requireFileBytes(t, backup, input)
	requireFileBytes(t, p, "a\nc\n")
	if runtime.GOOS != "windows" {
		requireMode(t, backup, 0o640)
		requireMode(t, p, 0o640)
	}
}

// An existing backup with the same name is never overwritten; the profile
// is left alone instead.
func TestStripBlockFromFile_existingBackupAborts(t *testing.T) {
	input := "a\n# BEGIN REX\nb\n# END REX\n"
	p := writeProfile(t, input, 0o644)
	backup := p + ".rex-backup-1700000000"
	require.NoError(t, os.WriteFile(backup, []byte("older backup"), 0o600))

	_, err := stripBlockFromFile(p, testStamp)
	require.ErrorIs(t, err, os.ErrExist)
	requireFileBytes(t, p, input)
	requireFileBytes(t, backup, "older backup")
}

// stow and chezmoi link ~/.zshrc into a dotfiles checkout. The link must
// survive and the checkout's file must be the one rewritten.
func TestStripBlockFromFile_symlinkedProfile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks needs developer mode on Windows")
	}
	home := t.TempDir()
	dotfiles := filepath.Join(t.TempDir(), "dotfiles")
	require.NoError(t, os.MkdirAll(dotfiles, 0o700))
	target := filepath.Join(dotfiles, "zshrc")
	require.NoError(t, os.WriteFile(target, []byte("a\n# BEGIN REX\nb\n# END REX\nc\n"), 0o600))
	link := filepath.Join(home, ".zshrc")
	require.NoError(t, os.Symlink(target, link))

	backup, err := stripBlockFromFile(link, testStamp)
	require.NoError(t, err)

	info, err := os.Lstat(link)
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&os.ModeSymlink, "profile symlink was replaced by a regular file")
	dest, err := os.Readlink(link)
	require.NoError(t, err)
	require.Equal(t, target, dest)
	requireFileBytes(t, target, "a\nc\n")
	requireMode(t, target, 0o600)
	require.Equal(t, filepath.Join(home, ".zshrc.rex-backup-1700000000"), backup,
		"backup belongs next to the profile the user knows, not in the dotfiles checkout")
	requireNoBackups(t, dotfiles)
}

// A dangling profile symlink is skipped like a missing file.
func TestStripBlockFromFile_danglingSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks needs developer mode on Windows")
	}
	link := filepath.Join(t.TempDir(), ".zshrc")
	require.NoError(t, os.Symlink(filepath.Join(t.TempDir(), "gone"), link))

	backup, err := stripBlockFromFile(link, testStamp)
	require.NoError(t, err)
	require.Empty(t, backup)
}

func TestStripProfileBlocks_reportsPerFile(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, ".zshrc")
	bad := filepath.Join(dir, ".bashrc")
	clean := filepath.Join(dir, ".profile")
	require.NoError(t, os.WriteFile(good, []byte("# BEGIN REX\nx\n# END REX\n"), 0o600))
	require.NoError(t, os.WriteFile(bad, []byte("# BEGIN REX\nx\n"), 0o600))
	require.NoError(t, os.WriteFile(clean, []byte("plain\n"), 0o600))

	stripped, errs := stripProfileBlocks([]string{good, bad, clean, filepath.Join(dir, "missing")}, testStamp)
	require.Equal(t, []strippedProfile{{Path: good, Backup: good + ".rex-backup-1700000000"}}, stripped)
	require.Len(t, errs, 1)
	require.ErrorIs(t, errs[0], errUnterminatedBlock)
	require.Contains(t, errs[0].Error(), bad)
}

func TestHasProfileBlock(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, ".zshrc")
	_ = os.WriteFile(f, []byte("# BEGIN REX\nfoo\n# END REX\n"), 0o600)
	if !hasProfileBlock(f) {
		t.Fatal("expected block to be detected")
	}
	_ = os.WriteFile(f, []byte("no block\n"), 0o600)
	if hasProfileBlock(f) {
		t.Fatal("expected no block detected")
	}
	_ = os.WriteFile(f, []byte("echo '# BEGIN REX'\n"), 0o600)
	require.False(t, hasProfileBlock(f), "a marker inside another line is not a block")
}

// Every rc file `rex setup` can write the block into must be scanned.
func TestProfileFiles_coverSetupTargets(t *testing.T) {
	home := filepath.Join("h", "home")
	got := profileFiles(home)
	for _, want := range []string{
		filepath.Join(home, ".zshrc"),
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".bash_profile"),
		filepath.Join(home, ".profile"),
		filepath.Join(home, ".config", "fish", "config.fish"),
	} {
		require.Contains(t, got, want)
	}
}

// runStrip writes input to a temp file, calls stripBlockFromFile, and returns
// modified flag + resulting file content.
func runStrip(t *testing.T, input string) (bool, string) {
	t.Helper()
	p := writeProfile(t, input, 0o644)
	backup, err := stripBlockFromFile(p, testStamp)
	if err != nil {
		t.Fatalf("stripBlockFromFile: %v", err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	return backup != "", string(data)
}

// writeProfile creates a profile file with content and mode in a fresh dir.
func writeProfile(t *testing.T, content string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), ".zshrc")
	require.NoError(t, os.WriteFile(p, []byte(content), mode))
	require.NoError(t, os.Chmod(p, mode)) // WriteFile's mode is masked by umask
	return p
}

func requireFileBytes(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, want, string(got), "content of %s", path)
}

func requireMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, want, info.Mode().Perm(), "mode of %s", path)
}

func requireNoBackups(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*.rex-backup-*"))
	require.NoError(t, err)
	require.Empty(t, matches)
	temps, err := filepath.Glob(filepath.Join(dir, ".*.rex-*"))
	require.NoError(t, err)
	require.Empty(t, temps, "temp files left behind")
}
