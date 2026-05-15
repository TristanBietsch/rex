package adapter

import (
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/charmbracelet/x/ansi"
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

// cursorPositionRe matches CSI sequences that reposition the cursor or clear
// the display. Full-TUI agents (codex, gemini) emit dozens of these between
// visible text, and if we just strip them everything becomes one long line —
// the `^` anchor in the prompt regex can't catch the prompt char anymore. By
// replacing each with `\n` before stripping, we preserve the line structure
// the user sees on screen.
var cursorPositionRe = regexp.MustCompile(`\x1b\[\d*(?:;\d+)?[HfEFJd]`)

// Detect implements Adapter.
func (h *HeuristicCLI) Detect(window []byte, idle time.Duration) protocol.State {
	if idle < h.idle {
		return protocol.StateWorking
	}
	tail := window
	if len(tail) > 4096 {
		tail = tail[len(tail)-4096:]
	}
	withLineBreaks := cursorPositionRe.ReplaceAllString(string(tail), "\n")
	clean := ansi.Strip(withLineBreaks)
	if h.prompt.MatchString(clean) {
		return protocol.StateNeedsInput
	}
	return protocol.StateWorking
}
