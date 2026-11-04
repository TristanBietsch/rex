package testperm

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func requireMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		return // only the read-only attribute exists
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s: mode %#o, want %#o", path, got, want)
	}
}

func TestWriteFileAndMkdirAll_setExactModes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b")
	MkdirAll(t, dir, 0o755)
	requireMode(t, dir, 0o755)
	requireMode(t, filepath.Dir(dir), 0o700)

	path := filepath.Join(dir, "f")
	WriteFile(t, path, []byte("x"), 0o644)
	requireMode(t, path, 0o644)
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "x" {
		t.Fatalf("content %q, err %v", got, err)
	}

	Loosen(t, path, 0o640)
	requireMode(t, path, 0o640)
}
