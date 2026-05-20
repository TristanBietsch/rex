package adapter

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/tristanbietsch/rex/internal/protocol"
)

// ClaudeStructured parses claude-code's stream-json output line-by-line and
// classifies session state from message types.
type ClaudeStructured struct {
	mu       sync.Mutex
	last     protocol.State
	buffer   []byte
	lastSeen time.Time
}

// NewClaudeStructured returns a fresh adapter.
func NewClaudeStructured() *ClaudeStructured {
	return &ClaudeStructured{last: protocol.StateWorking, lastSeen: time.Now()}
}

// Detect implements Adapter.
func (a *ClaudeStructured) Detect(window []byte, idle time.Duration) protocol.State {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.buffer = append(a.buffer, window...)
	if len(a.buffer) > 64*1024 {
		a.buffer = a.buffer[32*1024:]
	}

	// Parse every complete line in the buffer.
	for {
		nl := indexNewline(a.buffer)
		if nl < 0 {
			break
		}
		line := a.buffer[:nl]
		a.buffer = a.buffer[nl+1:]
		if len(line) == 0 {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal(line, &obj); err != nil {
			continue
		}
		a.lastSeen = time.Now()
		switch obj["type"] {
		case "assistant", "user":
			a.last = protocol.StateWorking
		case "result":
			a.last = protocol.StateDone
		}
	}

	// If working and we've been idle for >30s without new structured events,
	// assume Claude is waiting on a permission prompt or human reply.
	if a.last == protocol.StateWorking && idle > 0 && time.Since(a.lastSeen) > 30*time.Second {
		return protocol.StateNeedsInput
	}
	return a.last
}

func indexNewline(b []byte) int {
	for i, c := range b {
		if c == '\n' {
			return i
		}
	}
	return -1
}
