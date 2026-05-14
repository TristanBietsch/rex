package cli

import (
	"os"
	"path/filepath"
	"time"
)

// DefaultSocket returns the default UDS path, matching rex-daemon's logic.
func DefaultSocket() string {
	if r := os.Getenv("XDG_RUNTIME_DIR"); r != "" {
		return filepath.Join(r, "rex.sock")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "rex", "rex.sock")
}

// deadlineSeconds returns a time.Time n seconds in the future. Used by CLI commands
// that need a read deadline.
func deadlineSeconds(n int) time.Time {
	return time.Now().Add(time.Duration(n) * time.Second)
}

var _ = deadlineSeconds // referenced by other cli files in subsequent tasks
