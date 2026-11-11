// Package adapter classifies session state from PTY output.
package adapter

import (
	"fmt"
	"time"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Adapter classifies output chunks into states and also reports when the agent
// is ready to receive its first user input.
type Adapter interface {
	Detect(window []byte, idle time.Duration) protocol.State
	// IsReadyForInput reports whether the agent's CLI is currently parked at
	// its input prompt (or otherwise able to accept typed/pasted input).
	// Used by the supervisor's initial-prompt goroutine to decide WHEN to
	// paste the wizard's "describe the task" text.
	IsReadyForInput(window []byte, idle time.Duration) bool
}

// For builds an adapter for a tool's detection config. hookFile is the
// per-session file hook-based adapters read (ignored by heuristic tools).
func For(t registry.Tool, hookFile string) (Adapter, error) {
	switch t.Detect.Kind {
	case "heuristic":
		h, err := NewHeuristic(t.Detect.PromptRegex, t.Detect.DoneRegex, time.Duration(t.Detect.IdleMs)*time.Millisecond)
		if err != nil {
			return nil, err
		}
		if err := h.setBusy(t.Detect.BusyRegex); err != nil {
			return nil, err
		}
		switch t.Detect.Status {
		case "":
			return h, nil
		case registry.StatusOSC7501:
			return NewOSCStatus(h), nil
		default:
			return nil, fmt.Errorf("unsupported detect.status %q", t.Detect.Status)
		}
	case "hooks":
		switch t.Detect.Format {
		case "claude":
			return NewClaudeHooks(hookFile), nil
		default:
			return nil, fmt.Errorf("unsupported hooks format %q", t.Detect.Format)
		}
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownDetect, t.Detect.Kind)
	}
}
