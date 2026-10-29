package daemonctl

import (
	"fmt"
	"runtime"
)

// CheckSocketPath reports a socket path too long for a Unix domain socket
// address. sun_path also holds the trailing NUL, so the limit is one byte
// under its size. Binding such a path otherwise fails with a bare "invalid
// argument" that names neither the path nor the limit.
func CheckSocketPath(path string) error {
	if len(path) >= sunPathLen {
		return fmt.Errorf("socket path %s is %d bytes; %s allows at most %d: use a shorter -socket path or XDG_RUNTIME_DIR",
			path, len(path), runtime.GOOS, sunPathLen-1)
	}
	return nil
}
