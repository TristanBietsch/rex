package adapter

import (
	"time"

	"github.com/tristanbietsch/rex/internal/protocol"
)

// ClaudeStructured is a placeholder. Real implementation lands in Task B18.
type ClaudeStructured struct{}

// NewClaudeStructured returns the placeholder. Replace in Task B18.
func NewClaudeStructured() *ClaudeStructured { return &ClaudeStructured{} }

// Detect implements Adapter with placeholder behavior (always working).
// Task B18 replaces this with real structured parsing.
func (c *ClaudeStructured) Detect(window []byte, idle time.Duration) protocol.State {
	_ = window
	_ = idle
	return protocol.StateWorking
}
