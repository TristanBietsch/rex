//go:build !windows

package workspace

import (
	"errors"
	"syscall"
)

// oNoFollow makes open fail on a symlink as the last path element.
const oNoFollow = syscall.O_NOFOLLOW

// oNonBlock keeps open from hanging on a FIFO.
const oNonBlock = syscall.O_NONBLOCK

// isLinkOpenErr reports whether err is open refusing a symlink (O_NOFOLLOW).
func isLinkOpenErr(err error) bool { return errors.Is(err, syscall.ELOOP) }
