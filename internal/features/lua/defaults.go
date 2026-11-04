package lua

import "time"

// DefaultSendTimeout is how long rex.send waits for the daemon input channel.
const DefaultSendTimeout = 2 * time.Second

// DefaultHandlerTimeout bounds one event handler call. It exceeds
// DefaultSendTimeout so a handler blocked in rex.send fails that send
// cleanly before the handler itself is cut off.
const DefaultHandlerTimeout = 3 * time.Second

// DefaultLoadTimeout bounds the top-level run of init.lua at daemon start.
const DefaultLoadTimeout = 5 * time.Second

// DefaultQueueSize is how many events a Dispatcher holds for Lua before it
// drops new ones. Updates to an already-queued session take no extra slot.
const DefaultQueueSize = 256

// dropWarnInterval rate-limits the Dispatcher's "queue full" warning.
const dropWarnInterval = 10 * time.Second
