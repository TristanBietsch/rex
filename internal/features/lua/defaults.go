package lua

import "time"

// DefaultSendTimeout is how long rex.send waits for the daemon input channel.
const DefaultSendTimeout = 2 * time.Second
