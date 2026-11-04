package rexlog

import (
	"fmt"
	"os"
)

// Logs carry session names, working directories, commands and error text,
// so rex keeps them readable by the owning user only.
const (
	privateDirMode  os.FileMode = 0o700
	privateFileMode os.FileMode = 0o600
)

// PrivateDir creates dir (and any missing parents) with mode 0700 and
// clears the group and other permission bits of dir if it already exists.
// Use it only for directories rex owns; never for a user-chosen path that
// may be shared.
func PrivateDir(dir string) error {
	if err := os.MkdirAll(dir, privateDirMode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("stat %s: %w", dir, err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		if err := os.Chmod(dir, perm&^0o077); err != nil {
			return fmt.Errorf("tighten %s: %w", dir, err)
		}
	}
	return nil
}

// OpenPrivate opens path for appending, creating it with mode 0600, and
// tightens an existing file to 0600 (older rex versions created logs 0644).
// The parent directory must exist. On Windows the mode only controls the
// read-only attribute.
func OpenPrivate(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, privateFileMode)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if err := f.Chmod(privateFileMode); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("tighten %s: %w", path, err)
	}
	return f, nil
}
