// Package adapter classifies session state from PTY output.
//
// Plan A ships HeuristicCLI (regex + idle). Plan B adds ClaudeStructured.
package adapter

import (
	"time"

	"github.com/tristanbietsch/rex/internal/protocol"
	"github.com/tristanbietsch/rex/internal/registry"
)

// Adapter classifies output chunks into states.
//
// The adapter is given a window of recent bytes plus the time since the last
// chunk arrived. It returns the state the session should be in (or an empty
// string to leave the state unchanged).
type Adapter interface {
	// Detect returns the next state given recent output and idle duration.
	// Returns "" to leave the state unchanged.
	Detect(window []byte, idle time.Duration) protocol.State
}

// For builds an adapter for a tool's detection config.
func For(t registry.Tool) (Adapter, error) {
	switch t.Detect.Kind {
	case "heuristic":
		return NewHeuristic(t.Detect.PromptRegex, time.Duration(t.Detect.IdleMs)*time.Millisecond), nil
	case "structured":
		// Plan B will return ClaudeStructured here.
		return nil, ErrStructuredUnsupported
	default:
		return nil, ErrUnknownDetect
	}
}
