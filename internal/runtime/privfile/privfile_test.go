package privfile

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/testutil/testperm"
)

func write(t *testing.T, dir, name, body string, mode os.FileMode) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o700))
	testperm.WriteFile(t, p, []byte(body), mode)
}

func TestRead_OKAndSubdir(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.txt", "hi", 0o600)
	write(t, dir, "sub/b.txt", "yo", 0o644)
	got, err := Read(dir, "a.txt", 1024)
	require.NoError(t, err)
	require.Equal(t, "hi", string(got))
	got, err = Read(dir, "sub/b.txt", 1024)
	require.NoError(t, err)
	require.Equal(t, "yo", string(got))
}

func TestRead_Missing(t *testing.T) {
	_, err := Read(t.TempDir(), "nope.txt", 10)
	require.ErrorContains(t, err, "cannot open file")
}

func TestRead_NotRegular(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "d"), 0o700))
	_, err := Read(dir, "d", 10)
	require.ErrorContains(t, err, "not a regular file")
}

func TestRead_TooBig(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "big", strings.Repeat("x", 2048), 0o600)
	_, err := Read(dir, "big", 1024)
	require.ErrorContains(t, err, "larger than 1 KiB")
}

func TestRead_ReadFailure(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a", "x", 0o600)
	prev := readAll
	t.Cleanup(func() { readAll = prev })
	readAll = func(io.Reader) ([]byte, error) { return nil, errors.New("disk on fire") }
	_, err := Read(dir, "a", 10)
	require.ErrorContains(t, err, "cannot read file: disk on fire")
}

func TestRead_RefusesWritable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows modes carry no group/other bits")
	}
	dir := t.TempDir()
	write(t, dir, "open", "x", 0o666)
	write(t, dir, "group", "x", 0o660)
	for _, n := range []string{"open", "group"} {
		_, err := Read(dir, n, 10)
		require.ErrorContains(t, err, "writable by group or others", n)
	}
}

func TestCheckDir(t *testing.T) {
	dir := t.TempDir()
	fi, err := os.Stat(dir)
	require.NoError(t, err)
	require.NoError(t, CheckDir(fi, dir))
	if runtime.GOOS == "windows" {
		return
	}
	loose := filepath.Join(t.TempDir(), "loose")
	require.NoError(t, os.Mkdir(loose, 0o700))
	testperm.Loosen(t, loose, 0o777)
	fi, err = os.Stat(loose)
	require.NoError(t, err)
	require.ErrorContains(t, CheckDir(fi, loose), "chmod go-w "+loose)
}
