package cli

import (
	"time"

	"github.com/tristanbietsch/rex/internal/daemonctl"
)

// DefaultSocket returns the default UDS path.
func DefaultSocket() string { return daemonctl.DefaultSocket() }

func deadlineSeconds(n int) time.Time {
	return time.Now().Add(time.Duration(n) * time.Second)
}

var _ = deadlineSeconds
