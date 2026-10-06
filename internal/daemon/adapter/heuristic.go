package adapter

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
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
	prompt, err := compileMultilineRegex(promptRegex)
	if err != nil {
		return nil, fmt.Errorf("compile prompt regex %q: %w", promptRegex, err)
	}
	h := &HeuristicCLI{prompt: prompt, idle: idle}
	if doneRegex != "" {
		done, err := compileMultilineRegex(doneRegex)
		if err != nil {
			return nil, fmt.Errorf("compile done regex %q: %w", doneRegex, err)
		}
		h.done = done
	}
	return h, nil
}

// compileMultilineRegex wraps a registry pattern with (?m). YAML entries often
// include their own (?m) prefix; strip one so we don't end up with (?m)(?m).
func compileMultilineRegex(regex string) (*regexp.Regexp, error) {
	r := strings.TrimPrefix(regex, "(?m)")
	return regexp.Compile("(?m)" + r)
}

// cursorPositionRe matches CSI sequences that reposition the cursor or clear
// the display. Full-TUI agents (codex, gemini, ollama) emit dozens of these
// between visible text, and if we just strip them everything becomes one long
// line — the `^` anchor in the prompt regex can't catch the prompt char
// anymore. By replacing each with `\n` before stripping, we preserve the line
// structure the user sees on screen.
//
// Codes matched:
//
//	H, f — cursor absolute position
//	E, F — cursor next/previous line
//	J    — erase display
//	d    — line position absolute
//	G    — cursor column position (ollama and gemini use `[1G` to return to
//	       column 1 before writing the next prompt — without this, the prompt
//	       text lands on the same logical line as the prior animation frames
//	       and the `^>>> ` anchor never matches).
var cursorPositionRe = regexp.MustCompile(`\x1b\[\d*(?:;\d+)?[HfEFJdG]`)

func (h *HeuristicCLI) cleanTail(window []byte) string {
	tail := window
	if len(tail) > 4096 {
		tail = tail[len(tail)-4096:]
	}
	withLineBreaks := cursorPositionRe.ReplaceAllString(string(tail), "\n")
	return ansi.Strip(withLineBreaks)
}

// Detect implements Adapter. Precedence after the idle gate: done > prompt > working.
func (h *HeuristicCLI) Detect(window []byte, idle time.Duration) protocol.State {
	if idle < h.idle {
		return protocol.StateWorking
	}
	clean := h.cleanTail(window)
	if h.done != nil && h.done.MatchString(clean) {
		return protocol.StateDone
	}
	if h.prompt.MatchString(clean) {
		return protocol.StateNeedsInput
	}
	return protocol.StateWorking
}

// IsReadyForInput returns true when the agent's prompt regex matches the
// cleaned output window. The visible prompt IS the ready signal, so no idle
// gate is applied — the idle parameter is part of the Adapter contract but
// ignored here.
func (h *HeuristicCLI) IsReadyForInput(window []byte, _ time.Duration) bool {
	return h.prompt.MatchString(h.cleanTail(window))
}
