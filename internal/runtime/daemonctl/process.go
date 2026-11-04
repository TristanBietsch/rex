package daemonctl

import "errors"

// ErrNotRunning is returned when no rex-daemon answers on the socket, or
// FindPID finds no rex-daemon process for the current user.
var ErrNotRunning = errors.New("rex-daemon is not running")

// ErrMultipleDaemons is returned by FindPID when the current user runs
// more than one rex-daemon (for example on different sockets): a PID alone
// cannot say which one the caller means.
var ErrMultipleDaemons = errors.New("multiple rex-daemon processes are running for this user")
