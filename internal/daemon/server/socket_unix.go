//go:build !windows

package server

import (
	"fmt"
	"os"
)

// restrictSocket makes the socket owner-only. Connecting to it is enough to
// type into every session, so its mode must not depend on the umask; the
// peer-uid check on accept (verifyPeer) is the second layer.
func restrictSocket(path string) error {
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod socket %s: %w", path, err)
	}
	return nil
}
