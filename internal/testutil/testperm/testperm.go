// Package testperm gives test fixtures the loose permission bits that older
// rex versions, other programs or a user's own chmod leave behind, so tests
// can check that rex tightens them (its own state, logs and sockets) or
// keeps them (files the user owns, such as config.yaml or a shell profile).
//
// Fixtures are created owner-only like everything else (0700 dirs, 0600
// files) and then loosened here on purpose. This is the one place tests
// widen modes, which keeps the intent visible and keeps gosec's G301, G302
// and G306 meaningful everywhere else.
//
// RequireMode checks the result in a form that holds on every supported OS.
package testperm

import (
	"os"
	"runtime"
	"testing"
)

// Loosen sets path's permission bits to mode, typically a group- or
// world-readable one such as 0o644 or 0o755. Chmod is used rather than a
// create mode so the result does not depend on the process umask. Windows
// only honors the owner write bit.
func Loosen(t testing.TB, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode.Perm()); err != nil {
		t.Fatalf("testperm: loosen %s to %#o: %v", path, mode.Perm(), err)
	}
}

// WriteFile writes data to path as a fixture with mode, regardless of the
// umask. It replaces the os.WriteFile(path, data, 0o644) a test would
// otherwise write to create a world-readable fixture.
func WriteFile(t testing.TB, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("testperm: write %s: %v", path, err)
	}
	Loosen(t, path, mode)
}

// MkdirAll creates dir and any missing parents owner-only, then sets dir
// itself to mode.
func MkdirAll(t testing.TB, dir string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("testperm: mkdir %s: %v", dir, err)
	}
	Loosen(t, dir, mode)
}

// RequireMode fails t unless path's permission bits are the ones os.Stat
// reports on this OS for a path whose mode was set to mode, when it was
// created or by chmod.
//
// On Unix that is exactly mode. Windows keeps only the owner write bit, as
// the read-only attribute, so os.Stat reports 0666 or 0444 for a file and
// 0777 or 0555 for a directory (it ignores the mode a directory is created
// with; only chmod sets the attribute). Owner-only access there comes from
// the ACL a path inherits from the user's profile, which mode bits do not
// show.
func RequireMode(t testing.TB, path string, mode os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("testperm: %v", err)
	}
	want := statMode(runtime.GOOS, mode, info.IsDir())
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("testperm: %s: mode %#o, want %#o (set to %#o on %s)", path, got, want, mode.Perm(), runtime.GOOS)
	}
}

// statMode returns the permission bits os.Stat reports on goos for a file,
// or a directory when dir is true, whose mode was set to mode.
func statMode(goos string, mode os.FileMode, dir bool) os.FileMode {
	if goos != "windows" {
		return mode.Perm()
	}
	got := os.FileMode(0o444) // read-only attribute set
	if mode&0o200 != 0 {
		got = 0o666
	}
	if dir {
		got |= 0o111
	}
	return got
}
