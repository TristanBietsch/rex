// Package testperm gives test fixtures the loose permission bits that older
// rex versions, other programs or a user's own chmod leave behind, so tests
// can check that rex tightens them (its own state, logs and sockets) or
// keeps them (files the user owns, such as config.yaml or a shell profile).
//
// Fixtures are created owner-only like everything else (0700 dirs, 0600
// files) and then loosened here on purpose. This is the one place tests
// widen modes, which keeps the intent visible and keeps gosec's G301, G302
// and G306 meaningful everywhere else.
package testperm

import (
	"os"
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
