package daemonctl

import "time"

// Socket dial and daemon-start polling defaults (shared by CLI, TUI boot, tests).
const (
	SocketDialTimeout = 200 * time.Millisecond
	StartPollInterval = 20 * time.Millisecond
	StartPollAttempts = 400 // 400 × 20ms = 8s
)

// StartTimeout is the maximum wait for the UDS socket after spawning rex-daemon.
// A new daemon waits up to 6 s for a stopping one's state-dir lock before it
// binds (server stateLockWait, during `rex daemon restart`); this covers that
// plus startup. Start returns as soon as the daemon exits, so failures don't
// wait it out.
const StartTimeout = StartPollAttempts * StartPollInterval

// startTimeout is the wait Start uses: StartTimeout, in a var so tests of
// the early-exit path can widen it past a slow first exec under load.
var startTimeout = StartTimeout
