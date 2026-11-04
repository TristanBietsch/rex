//go:build !linux && !darwin

package server

import "net"

// peerCredSupported reports whether verifyPeer actually checks the peer.
// Here it does not: Windows AF_UNIX sockets have no peer-credential option
// (the socket file inherits its directory's ACL, which is the guard), and
// the BSDs are not supported targets yet.
const peerCredSupported = false

// peerUID is a documented no-op on this platform; verifyPeer then accepts.
func peerUID(*net.UnixConn) (int, error) {
	return 0, errPeerCredUnsupported
}
