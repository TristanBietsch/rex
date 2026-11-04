//go:build darwin

package server

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

// peerCredSupported reports whether verifyPeer actually checks the peer.
const peerCredSupported = true

// peerUID returns the effective uid of the process on the other end of c,
// as the kernel recorded it at connect time (LOCAL_PEERCRED).
func peerUID(c *net.UnixConn) (int, error) {
	var uid int
	err := controlFD(c, func(fd int) error {
		cred, err := unix.GetsockoptXucred(fd, unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		if err != nil {
			return fmt.Errorf("LOCAL_PEERCRED: %w", err)
		}
		uid = int(cred.Uid)
		return nil
	})
	return uid, err
}
