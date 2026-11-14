//go:build !windows

package audio

import (
	"errors"
	"io/fs"
	"os"
	"syscall"
)

// oNoFollow makes open fail on a symlink as the last path element.
const oNoFollow = syscall.O_NOFOLLOW

// checkClaimDir refuses a claim folder another user owns or that group or
// others can open: either could forge or read claims.
func checkClaimDir(fi fs.FileInfo) error {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("cannot read the claim folder's owner")
	}
	if int(st.Uid) != os.Getuid() {
		return errors.New("claim folder is not owned by you")
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return errors.New("claim folder is open to group or others (chmod 700)")
	}
	return nil
}
