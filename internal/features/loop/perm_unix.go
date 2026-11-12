//go:build !windows

package loop

import (
	"errors"
	"io/fs"
	"os"
	"syscall"
)

// checkOwnerAndMode refuses a file that another user owns or that group or
// others can write: either could change the command the daemon runs.
func checkOwnerAndMode(fi fs.FileInfo) error {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("cannot read file owner")
	}
	if int(st.Uid) != os.Getuid() {
		return errors.New("not owned by you")
	}
	if fi.Mode().Perm()&0o022 != 0 {
		return errors.New("group- or world-writable (run chmod 600)")
	}
	return nil
}
