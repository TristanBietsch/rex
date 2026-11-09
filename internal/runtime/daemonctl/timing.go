package daemonctl

import "time"

// Socket dial and daemon-start polling defaults (shared by CLI, TUI boot, tests).
const (
	SocketDialTimeout = 200 * time.Millisecond
	StartPollInterval = 20 * time.Millisecond
	StartPollAttempts = 100 // 100 × 20ms = 2s
)

// StartTimeout is the maximum wait for the UDS socket after spawning rex-daemon.
const StartTimeout = StartPollAttempts * StartPollInterval

// StopTimeout bounds how long Stop waits for the daemon to remove its
// socket after a Shutdown intent. Shutdown kills every live session, each
// bounded by the supervisor's 5s drain, so this leaves room for that.
const StopTimeout = 10 * time.Second

// ReloadTimeout bounds ReloadDaemon: the handshake plus the daemon
// re-reading tools.yaml, which is a small local file.
const ReloadTimeout = 5 * time.Second

// intentWriteTimeout bounds writing one intent to a connected daemon.
const intentWriteTimeout = 2 * time.Second

// pidLookupTimeout bounds the pgrep/tasklist run behind FindPID.
const pidLookupTimeout = 2 * time.Second
