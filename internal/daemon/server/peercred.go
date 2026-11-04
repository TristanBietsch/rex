package server

import (
	"errors"
	"fmt"
	"net"
)

// errPeerCredUnsupported is what peerUID returns on platforms without a
// peer-credential socket option.
var errPeerCredUnsupported = errors.New("peer credentials not supported")

// verifyPeer rejects a connection whose peer runs as a different user than
// want. Anyone who can talk to the socket can type into every session's PTY,
// i.e. run commands as the daemon's user, so the socket's 0600 mode is
// backed by a check of the kernel-reported peer uid. On platforms without
// peer credentials (see peercred_other.go) it accepts every connection and
// the socket's file permissions are the only guard.
func verifyPeer(conn net.Conn, want int) error {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return fmt.Errorf("not a unix socket connection (%T)", conn)
	}
	uid, err := peerUID(uc)
	if errors.Is(err, errPeerCredUnsupported) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read peer credentials: %w", err)
	}
	if uid != want {
		return fmt.Errorf("peer uid %d is not the daemon's uid %d", uid, want)
	}
	return nil
}
