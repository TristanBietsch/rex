// Package privfile reads user-written files that rex trusts as code or
// configuration (theme files, config.lua and its modules).
//
// The file is opened once and every check runs on that handle, so what is
// checked is what is read: a link swapped in between cannot slip a different
// file past the checks. A symlink is followed (a stow link into a dotfiles
// repo is fine), but its target must be a regular file, owned by you, not
// writable by group or others, and within the size cap. On Windows a link
// must stay inside the directory (os.Root) and ownership is an ACL matter
// that SECURITY.md documents as out of scope.
package privfile

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// readAll is io.ReadAll; tests replace it to fail a read.
var readAll = io.ReadAll

// Read opens dir/name and returns its bytes, or an error saying why the file
// is refused. name may hold forward slashes for a file below dir. The open
// never blocks on a FIFO. A file larger than max is refused; one that grows
// past max while being read is returned with max+1 bytes, so the caller can
// refuse it too.
func Read(dir, name string, max int64) ([]byte, error) {
	f, err := open(dir, name)
	if err != nil {
		return nil, fmt.Errorf("cannot open file (a dangling or looping link, or one that is not allowed): %w", err)
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		return nil, errors.New("not a regular file (or a link to one); refused")
	}
	if fi.Size() > max {
		return nil, fmt.Errorf("file is larger than %d KiB; refused", max>>10)
	}
	if err := checkFile(fi); err != nil {
		return nil, err
	}
	data, err := readAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, fmt.Errorf("cannot read file: %w", err)
	}
	return data, nil
}

// CheckDir applies the same ownership and mode rule to a directory whose
// FileInfo the caller already holds.
func CheckDir(fi os.FileInfo, dir string) error { return checkDir(fi, dir) }
