//go:build !linux && !darwin

package server

import "errors"

// errProcTableUnsupported is what parentPID returns on platforms where rex
// does not read the process table.
var errProcTableUnsupported = errors.New("process table not supported")

// parentPID is not implemented on this platform. It is unreachable in
// practice: peerPID fails here first (no peer credentials), so no
// connection is ever classified by ancestry.
func parentPID(int) (int, error) {
	return 0, errProcTableUnsupported
}
