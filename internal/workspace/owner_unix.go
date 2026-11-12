//go:build !windows

package workspace

import (
	"errors"
	"io/fs"
	"os"
	"syscall"
)

// skipModeChecks is false where Unix permission bits mean something.
const skipModeChecks = false

func ownedByCurrentUser(fi fs.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return true
	}
	return int(st.Uid) == os.Getuid()
}

func isNotDirErr(err error) bool { return errors.Is(err, syscall.ENOTDIR) }
