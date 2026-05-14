package adapter

import (
	"errors"
	"regexp"
	"time"

	"github.com/tristanbietsch/rex/internal/protocol"
)

// ErrUnknownDetect signals an unsupported detect.kind in the registry.
var ErrUnknownDetect = errors.New("unknown detect kind")

// ErrStructuredUnsupported is returned in Plan A; Plan B implements ClaudeStructured.
var ErrStructuredUnsupported = errors.New("structured adapter not implemented in Plan A")

// HeuristicCLI is a regex+idle adapter for CLIs without structured output.
type HeuristicCLI struct {
	prompt *regexp.Regexp
	idle   time.Duration
}

// NewHeuristic builds a HeuristicCLI.
func NewHeuristic(promptRegex string, idle time.Duration) *HeuristicCLI {
	// Use multiline mode so ^ matches start-of-line by default.
	re := regexp.MustCompile("(?m)" + promptRegex)
	return &HeuristicCLI{prompt: re, idle: idle}
}

// Detect implements Adapter.
func (h *HeuristicCLI) Detect(window []byte, idle time.Duration) protocol.State {
	if idle < h.idle {
		return protocol.StateWorking
	}
	// The window has been idle for longer than idle_ms. Now check whether the
	// trailing region matches the prompt pattern — if so, the session is waiting.
	tail := window
	if len(tail) > 4096 {
		tail = tail[len(tail)-4096:]
	}
	if h.prompt.Match(tail) {
		return protocol.StateNeedsInput
	}
	return protocol.StateWorking
}
