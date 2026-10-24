package daemonctl

import "errors"

// ErrNotRunning is returned when no rex-daemon process is found.
var ErrNotRunning = errors.New("no rex-daemon process found")
