package testperm

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAndMkdirAll_setExactModes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b")
	MkdirAll(t, dir, 0o755)
	RequireMode(t, dir, 0o755)
	RequireMode(t, filepath.Dir(dir), 0o700)

	path := filepath.Join(dir, "f")
	WriteFile(t, path, []byte("x"), 0o644)
	RequireMode(t, path, 0o644)
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "x" {
		t.Fatalf("content %q, err %v", got, err)
	}

	Loosen(t, path, 0o640)
	RequireMode(t, path, 0o640)

	// Without the owner write bit (the read-only attribute on Windows).
	// Restored so the TempDir cleanup can remove the file on Windows.
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	Loosen(t, path, 0o400)
	RequireMode(t, path, 0o400)
}

// Windows keeps only the owner write bit, as the read-only attribute, and
// reports the execute bits on every directory.
func TestStatMode(t *testing.T) {
	for _, tc := range []struct {
		goos string
		mode os.FileMode
		dir  bool
		want os.FileMode
	}{
		{"linux", 0o600, false, 0o600},
		{"darwin", 0o700, true, 0o700},
		{"linux", os.ModeDir | 0o755, true, 0o755},
		{"windows", 0o600, false, 0o666},
		{"windows", 0o700, false, 0o666},
		{"windows", 0o400, false, 0o444},
		{"windows", 0o700, true, 0o777},
		{"windows", 0o555, true, 0o555},
	} {
		if got := statMode(tc.goos, tc.mode, tc.dir); got != tc.want {
			t.Errorf("statMode(%s, %#o, dir=%v) = %#o, want %#o", tc.goos, tc.mode, tc.dir, got, tc.want)
		}
	}
}
