//go:build !windows

package privfile

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// open opens dir/name, following a symlink wherever it points (the caller
// checks the target on the handle). O_NONBLOCK keeps the open of a FIFO from
// waiting for a writer; it does nothing for a regular file.
func open(dir, name string) (*os.File, error) {
	return os.OpenFile(filepath.Join(dir, filepath.FromSlash(name)), os.O_RDONLY|syscall.O_NONBLOCK, 0)
}

// checkFile refuses a file that someone else could have written: owned by
// another user, or writable by group or others. This mirrors what OpenSSH
// does for its config, so a shared or downloaded file that was left
// world-writable is not trusted without a deliberate chmod.
func checkFile(fi os.FileInfo) error {
	return check(fi, os.Geteuid(), "run: chmod go-w <file>")
}

func checkDir(fi os.FileInfo, dir string) error {
	return check(fi, os.Geteuid(), "run: chmod go-w "+dir)
}

// check is the rule, with the effective user id passed in.
func check(fi os.FileInfo, euid int, hint string) error {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) != euid {
		return fmt.Errorf("owned by another user (uid %d, you are %d); refused", st.Uid, euid)
	}
	if perm := fi.Mode().Perm(); perm&0o022 != 0 {
		return fmt.Errorf("writable by group or others (mode %04o); refused, %s", perm, hint)
	}
	return nil
}
