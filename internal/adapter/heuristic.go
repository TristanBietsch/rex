package adapter

import (
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/tristanbietsch/rex/internal/protocol"
)

// ErrUnknownDetect signals an unsupported detect.kind in the registry.
var ErrUnknownDetect = errors.New("unknown detect kind")

// HeuristicCLI is a regex+idle adapter for CLIs without structured output.
type HeuristicCLI struct {
	prompt *regexp.Regexp
	idle   time.Duration
}

// NewHeuristic builds a HeuristicCLI. Returns an error if the regex is invalid.
func NewHeuristic(promptRegex string, idle time.Duration) (*HeuristicCLI, error) {
	re, err := regexp.Compile("(?m)" + promptRegex)
	if err != nil {
		return nil, fmt.Errorf("compile prompt regex %q: %w", promptRegex, err)
	}
	return &HeuristicCLI{prompt: re, idle: idle}, nil
}

// Detect implements Adapter.
func (h *HeuristicCLI) Detect(window []byte, idle time.Duration) protocol.State {
	if idle < h.idle {
		return protocol.StateWorking
	}
	tail := window
	if len(tail) > 4096 {
		tail = tail[len(tail)-4096:]
	}
	if h.prompt.Match(tail) {
		return protocol.StateNeedsInput
	}
	return protocol.StateWorking
}
