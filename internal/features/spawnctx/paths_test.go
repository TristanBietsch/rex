package spawnctx

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// workspace is a fresh root with HOME pointed away from the real one, and
// the files given (name → content); a name ending in "/" is a folder.
func workspace(t *testing.T, files map[string]string) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	root := t.TempDir()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if strings.HasSuffix(name, "/") {
			require.NoError(t, os.MkdirAll(p, 0o700))
			continue
		}
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o700))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	}
	return root
}

func TestValidatePath_Inside(t *testing.T) {
	root := workspace(t, map[string]string{"internal/auth/auth_test.go": "package auth\n", "docs/": ""})
	info, err := ValidatePath(root, "internal/auth/auth_test.go")
	require.NoError(t, err)
	require.Equal(t, PathInfo{Rel: "internal/auth/auth_test.go", Size: 13}, info)

	info, err = ValidatePath(root, "docs")
	require.NoError(t, err)
	require.Equal(t, PathInfo{Rel: "docs", Dir: true}, info)

	info, err = ValidatePath(root, "docs/")
	require.NoError(t, err, "a folder's trailing slash is fine")
	require.Equal(t, "docs", info.Rel)

	// An absolute path inside the root is taken as its relative path.
	info, err = ValidatePath(root, filepath.Join(root, "docs"))
	require.NoError(t, err)
	require.Equal(t, "docs", info.Rel)

	_, err = ValidatePath(root, "missing.go")
	require.ErrorIs(t, err, os.ErrNotExist)

	_, err = ValidatePath(root, ".")
	require.ErrorIs(t, err, ErrBadName, "the workspace itself is not an item")
	_, err = ValidatePath(root, "")
	require.ErrorIs(t, err, ErrBadName)
}

func TestValidatePath_DotDot(t *testing.T) {
	root := workspace(t, map[string]string{"a/b.go": "x"})
	require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(root), "secret.txt"), []byte("s"), 0o600))
	for _, p := range []string{"../secret.txt", "a/../../secret.txt", "..", "a/../.."} {
		_, err := ValidatePath(root, p)
		require.ErrorIs(t, err, ErrOutside, p)
		var pe *PathError
		require.ErrorAs(t, err, &pe)
		require.Equal(t, p, pe.Path)
		require.Empty(t, pe.Target)
	}
	info, err := ValidatePath(root, "a/../a/b.go")
	require.NoError(t, err, "a .. that stays inside is fine")
	require.Equal(t, "a/b.go", info.Rel)
}

func TestValidatePath_AbsoluteOutside(t *testing.T) {
	root := workspace(t, nil)
	other := t.TempDir()
	f := filepath.Join(other, "key.pem")
	require.NoError(t, os.WriteFile(f, []byte("k"), 0o600))
	_, err := ValidatePath(root, f)
	require.ErrorIs(t, err, ErrOutside)
	require.Contains(t, err.Error(), "outside the workspace")
}

func TestValidatePath_SymlinkOut(t *testing.T) {
	root := workspace(t, nil)
	other := t.TempDir()
	target := filepath.Join(other, "passwords.txt")
	require.NoError(t, os.WriteFile(target, []byte("p"), 0o600))
	if err := os.Symlink(target, filepath.Join(root, "notes.txt")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	_, err := ValidatePath(root, "notes.txt")
	require.ErrorIs(t, err, ErrOutside)
	var pe *PathError
	require.ErrorAs(t, err, &pe)
	real, _ := filepath.EvalSymlinks(target)
	require.Equal(t, real, pe.Target, "the error names the link's target")
	require.Contains(t, err.Error(), real)

	// A link to a folder outside, then a path through it.
	require.NoError(t, os.Symlink(other, filepath.Join(root, "out")))
	_, err = ValidatePath(root, "out/passwords.txt")
	require.ErrorIs(t, err, ErrOutside)
	_, _, err = Preview(root, "out/passwords.txt", 1<<10, 10)
	require.ErrorIs(t, err, ErrOutside, "the preview checks the path first")

	require.NoError(t, os.Symlink(filepath.Join(other, "gone"), filepath.Join(root, "dangling")))
	_, err = ValidatePath(root, "dangling")
	require.Error(t, err)
}

func TestValidatePath_SymlinkInside(t *testing.T) {
	root := workspace(t, map[string]string{"real/main.go": "package main\n", "keys/.env": "A=1\n"})
	if err := os.Symlink(filepath.Join(root, "real", "main.go"), filepath.Join(root, "link.go")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	info, err := ValidatePath(root, "link.go")
	require.NoError(t, err)
	require.Equal(t, PathInfo{Rel: "link.go", Size: 13}, info, "the item keeps the name picked")

	require.NoError(t, os.Symlink("real", filepath.Join(root, "alias")))
	info, err = ValidatePath(root, "alias")
	require.NoError(t, err)
	require.True(t, info.Dir)

	// A harmless name that links to a secret is still flagged.
	require.NoError(t, os.Symlink(filepath.Join(root, "keys", ".env"), filepath.Join(root, "config.txt")))
	info, err = ValidatePath(root, "config.txt")
	require.NoError(t, err)
	require.Equal(t, "looks like secrets", info.Deny)
}

func TestValidatePath_RootIsALink(t *testing.T) {
	real := workspace(t, map[string]string{"a.go": "x"})
	link := filepath.Join(t.TempDir(), "ws")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	info, err := ValidatePath(link, "a.go")
	require.NoError(t, err)
	require.Equal(t, "a.go", info.Rel)
	_, err = ValidatePath(filepath.Join(t.TempDir(), "nope"), "a.go")
	require.Error(t, err, "a root that does not exist")
	_, err = ValidatePath("relative/root", "a.go")
	require.ErrorIs(t, err, ErrOutside, "a relative root has no inside")
}

func TestValidatePath_ControlCharName(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows file names cannot hold control characters")
	}
	for _, name := range []string{"a\nb.go", "tab\there.go", "esc\x1b[31m.go", "line sep.go", "para sep.go", "rlo‮gpj.go", "bad\xff.go"} {
		root := workspace(t, nil)
		// Created when the file system allows it: the check is on the name,
		// before any file system call.
		_ = os.WriteFile(filepath.Join(root, name), []byte("x"), 0o600)
		_, err := ValidatePath(root, name)
		require.ErrorIs(t, err, ErrBadName, "%q", name)
		require.NotContains(t, err.Error(), "\n")
	}
}

func TestValidatePath_BigFileWarns(t *testing.T) {
	root := workspace(t, nil)
	f, err := os.Create(filepath.Join(root, "big.log"))
	require.NoError(t, err)
	require.NoError(t, f.Truncate(BigFile+1))
	require.NoError(t, f.Close())
	info, err := ValidatePath(root, "big.log")
	require.NoError(t, err)
	require.Equal(t, "the agent may not read all of it", info.Warn)

	require.NoError(t, os.Truncate(filepath.Join(root, "big.log"), BigFile))
	info, err = ValidatePath(root, "big.log")
	require.NoError(t, err)
	require.Empty(t, info.Warn, "exactly 1 MiB is fine")
}

func TestValidatePath_Denylist(t *testing.T) {
	deny := []string{
		".env", ".env.local", ".env.test", "config/.env.production",
		"server.pem", "tls/server.PEM", "private.key",
		"id_rsa", "id_rsa.pub", "home/.ssh/id_ed25519", "id_ed25519_sk",
		"cert.p12", ".netrc", ".npmrc", ".pypirc",
		"credentials", "credentials.json", "aws/credentials_backup",
		"vault.kdbx",
	}
	files := map[string]string{}
	for _, p := range deny {
		files[p] = "s"
	}
	root := workspace(t, files)
	for _, p := range deny {
		reason, ok := Denied(p)
		require.True(t, ok, p)
		require.Equal(t, "looks like secrets", reason)
		info, err := ValidatePath(root, p)
		require.NoError(t, err, "a denylisted path is asked about, not refused: %s", p)
		require.Equal(t, "looks like secrets", info.Deny, p)
	}
	for _, p := range []string{"env.go", "environment.md", "keys.go", "id.go", "my_credentials.go", "pem/", "docs/key.md", "netrc.go"} {
		_, ok := Denied(p)
		require.False(t, ok, p)
	}
	_, ok := Denied("secrets/.env/")
	require.True(t, ok, "a folder name counts too")
}

func TestPreview_BoundedRead(t *testing.T) {
	root := workspace(t, nil)
	var b strings.Builder
	for b.Len() < 5<<20 {
		b.WriteString("log line with some words in it\n")
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "huge.log"), []byte(b.String()), 0o600))
	lines, more, err := Preview(root, "huge.log", 64<<10, 200)
	require.NoError(t, err)
	require.Len(t, lines, 200)
	require.Positive(t, more)
	read := 0
	for _, l := range lines {
		read += len(l) + 1
	}
	require.LessOrEqual(t, read, 64<<10)

	// One long line cut by the byte cap: the cut line counts as more.
	require.NoError(t, os.WriteFile(filepath.Join(root, "one.txt"), []byte(strings.Repeat("a", 2000)), 0o600))
	lines, more, err = Preview(root, "one.txt", 100, 200)
	require.NoError(t, err)
	require.Empty(t, lines)
	require.Equal(t, 1, more)

	// Cut right after a newline: still at least one more line.
	require.NoError(t, os.WriteFile(filepath.Join(root, "two.txt"), []byte("first\nsecond\n"), 0o600))
	lines, more, err = Preview(root, "two.txt", 6, 200)
	require.NoError(t, err)
	require.Equal(t, []string{"first"}, lines)
	require.Equal(t, 1, more)

	// Small file: all of it, sanitized.
	require.NoError(t, os.WriteFile(filepath.Join(root, "small.go"), []byte("a\r\n\x1b[31mb\x1b[0m\n"), 0o600))
	lines, more, err = Preview(root, "small.go", 64<<10, 200)
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b"}, lines)
	require.Zero(t, more)

	require.NoError(t, os.WriteFile(filepath.Join(root, "empty.txt"), nil, 0o600))
	lines, more, err = Preview(root, "empty.txt", 64<<10, 200)
	require.NoError(t, err)
	require.Empty(t, lines)
	require.Zero(t, more)

	// A multi-byte rune cut by the byte cap is not binary.
	require.NoError(t, os.WriteFile(filepath.Join(root, "utf.txt"), []byte("ab界界界"), 0o600))
	lines, _, err = Preview(root, "utf.txt", 4, 200)
	require.NoError(t, err)
	require.Empty(t, lines, "the cut line is dropped")
}

func TestPreview_Binary(t *testing.T) {
	root := workspace(t, map[string]string{"bin/app": "MZ\x00\x00\x00\x00\x01\x02\xff\xfe\x00\x00"})
	_, _, err := Preview(root, "bin/app", 64<<10, 200)
	require.ErrorIs(t, err, ErrBinary)
}

func TestPreview_FolderAndMissing(t *testing.T) {
	root := workspace(t, map[string]string{"docs/": ""})
	_, _, err := Preview(root, "docs", 64<<10, 200)
	require.ErrorIs(t, err, ErrIsDir)
	_, _, err = Preview(root, "nope.txt", 64<<10, 200)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestPreview_RechecksTheOpenedFile(t *testing.T) {
	root := workspace(t, map[string]string{"a.txt": "a\n", "b.txt": "b\n"})
	orig := openRead
	t.Cleanup(func() { openRead = orig })

	// The path is swapped for another file between the check and the open.
	openRead = func(string) (*os.File, error) { return os.Open(filepath.Join(root, "b.txt")) }
	_, _, err := Preview(root, "a.txt", 64<<10, 200)
	require.ErrorIs(t, err, ErrSpecial)

	// A read that fails (a file opened write-only reads nothing).
	openRead = func(p string) (*os.File, error) { return os.OpenFile(p, os.O_WRONLY, 0) }
	_, _, err = Preview(root, "a.txt", 64<<10, 200)
	require.Error(t, err)

	openRead = func(string) (*os.File, error) { return nil, errors.New("open failed") }
	_, _, err = Preview(root, "a.txt", 64<<10, 200)
	require.EqualError(t, err, "open failed")
}

func TestPathError_Words(t *testing.T) {
	e := &PathError{Path: "x", Err: ErrSpecial}
	require.Equal(t, "x: not a regular file or folder", e.Error())
	e = &PathError{Path: "x", Target: "/etc/y", Err: ErrOutside}
	require.Equal(t, "x: links to /etc/y, outside the workspace", e.Error())
}
