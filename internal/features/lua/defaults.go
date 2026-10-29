package lua

import "time"

// DefaultSendTimeout is how long rex.send waits for the daemon input channel.
const DefaultSendTimeout = 2 * time.Second

// DefaultCallTimeout bounds one run of user Lua: loading the script or one
// handler call. A runaway script (an endless loop) is aborted at the limit.
const DefaultCallTimeout = 5 * time.Second
