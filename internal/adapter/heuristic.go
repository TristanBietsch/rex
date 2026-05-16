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
	done   *regexp.Regexp // nil = no auto-done; manual completion or process exit only
	idle   time.Duration
}

// NewHeuristic builds a HeuristicCLI. promptRegex is required; doneRegex is optional
// (empty string disables auto-done). Returns an error if either regex is invalid.
func NewHeuristic(promptRegex, doneRegex string, idle time.Duration) (*HeuristicCLI, error) {
	prompt, err := regexp.Compile("(?m)" + promptRegex)
	if err != nil {
		return nil, fmt.Errorf("compile prompt regex %q: %w", promptRegex, err)
	}
	h := &HeuristicCLI{prompt: prompt, idle: idle}
	if doneRegex != "" {
		done, err := regexp.Compile("(?m)" + doneRegex)
		if err != nil {
			return nil, fmt.Errorf("compile done regex %q: %w", doneRegex, err)
		}
		h.done = done
	}
	return h, nil
}

// cursorPositionRe matches CSI sequences that reposition the cursor or clear
// the display. Full-TUI agents (codex, gemini) emit dozens of these between
// visible text, and if we just strip them everything becomes one long line —
// the `^` anchor in the prompt regex can't catch the prompt char anymore. By
// replacing each with `\n` before stripping, we preserve the line structure
// the user sees on screen.
var cursorPositionRe = regexp.MustCompile(`\x1b\[\d*(?:;\d+)?[HfEFJd]`)

// Detect implements Adapter. Precedence after the idle gate: done > prompt > working.
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
	if h.done != nil && h.done.MatchString(clean) {
		return protocol.StateDone
	}
	if h.prompt.MatchString(clean) {
		return protocol.StateNeedsInput
	}
	return protocol.StateWorking
}
