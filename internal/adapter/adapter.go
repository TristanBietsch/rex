// Package adapter classifies session state from PTY output.
package adapter

import (
	"fmt"
	"time"

	"github.com/tristanbietsch/rex/internal/protocol"
	"github.com/tristanbietsch/rex/internal/registry"
)

// Adapter classifies output chunks into states.
type Adapter interface {
	Detect(window []byte, idle time.Duration) protocol.State
}

// For builds an adapter for a tool's detection config.
func For(t registry.Tool) (Adapter, error) {
	switch t.Detect.Kind {
	case "heuristic":
		return NewHeuristic(t.Detect.PromptRegex, t.Detect.DoneRegex, time.Duration(t.Detect.IdleMs)*time.Millisecond)
	case "structured":
		switch t.Detect.Format {
		case "claude_jsonl":
			return NewClaudeStructured(), nil
		default:
			return nil, fmt.Errorf("unsupported structured format %q", t.Detect.Format)
		}
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownDetect, t.Detect.Kind)
	}
}
