package core

import (
	"time"

	"github.com/tristanbietsch/rex/internal/runtime/daemonctl"
)

// DefaultSocket returns the default UDS path.
func DefaultSocket() string { return daemonctl.DefaultSocket() }

func DeadlineSeconds(n int) time.Time {
	return time.Now().Add(time.Duration(n) * time.Second)
}

var _ = DeadlineSeconds
