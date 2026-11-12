package workspace

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// ConfigFileName is the workspace's own Lua settings inside DirName (R06
// W3). rex evaluates it only for a workspace the user trusted (TrustStore).
const ConfigFileName = "config.lua"

// MaxConfigBytes caps config.lua; a larger file is refused unread. It
// matches the Lua evaluator's own cap (luacfg.MaxScript).
const MaxConfigBytes = 256 << 10

// ownedByMe reports whether fi belongs to the current user. Tests replace it
// to stand in for another user's file.
var ownedByMe = ownedByCurrentUser

// ConfigPath is root/.rex/config.lua.
func ConfigPath(root string) string { return filepath.Join(root, DirName, ConfigFileName) }

// HasConfig reports whether root/.rex/config.lua exists, as anything (a
// symlink too). It only Lstats: nothing is read or followed.
func HasConfig(root string) bool {
	_, err := os.Lstat(ConfigPath(root))
	return err == nil
}

// ReadConfig reads root/.rex/config.lua as hostile input. The .rex/ is
// checked as Find does (a real folder of yours that others cannot write);
// the file is opened once, without following a symlink and without blocking
// on a FIFO, and every check runs on that handle: a regular file, still the
// file the path names, no other hard link, owned by you, not group- or
// world-writable, at most MaxConfigBytes. A missing .rex/ or file wraps
// fs.ErrNotExist; a refusal wraps ErrUnsafe (or ErrInvalid for the size).
func ReadConfig(root string) ([]byte, error) {
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("workspace: root %q is not absolute", root)
	}
	rexDir := filepath.Join(filepath.Clean(root), DirName)
	if err := checkDir(rexDir); err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, errNotDir) {
			return nil, fmt.Errorf("workspace: no %s: %w", rexDir, fs.ErrNotExist)
		}
		return nil, err
	}
	return readPrivate(filepath.Join(rexDir, ConfigFileName), MaxConfigBytes)
}

// readPrivate reads path, refusing anything but a plain private file of
// the current user's, checked on the opened handle. A missing file wraps
// fs.ErrNotExist.
func readPrivate(path string, maxBytes int64) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|oNoFollow|oNonBlock, 0)
	if err != nil {
		if isLinkOpenErr(err) {
			return nil, fmt.Errorf("%w: %s is a symlink", ErrUnsafe, path)
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %s is not a regular file", ErrUnsafe, path)
	}
	// Windows opens follow symlinks: the path must still name this file.
	if lfi, err := os.Lstat(path); err != nil || !os.SameFile(lfi, fi) {
		return nil, fmt.Errorf("%w: %s is a link or changed while opening", ErrUnsafe, path)
	}
	if !skipModeChecks {
		switch {
		case hardLinked(fi):
			return nil, fmt.Errorf("%w: %s has other hard links", ErrUnsafe, path)
		case !ownedByMe(fi):
			return nil, fmt.Errorf("%w: %s is owned by another user", ErrUnsafe, path)
		case fi.Mode().Perm()&0o022 != 0:
			return nil, fmt.Errorf("%w: %s is group- or world-writable", ErrUnsafe, path)
		}
	}
	if fi.Size() > maxBytes {
		return nil, fmt.Errorf("%w: %s is larger than %d bytes", ErrInvalid, path, maxBytes)
	}
	b, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > maxBytes {
		return nil, fmt.Errorf("%w: %s is larger than %d bytes", ErrInvalid, path, maxBytes)
	}
	return b, nil
}
