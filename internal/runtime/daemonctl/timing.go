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

// startTimeout is the wait Start uses: StartTimeout, in a var so tests of
// the early-exit path can widen it past a slow first exec under load.
var startTimeout = StartTimeout
