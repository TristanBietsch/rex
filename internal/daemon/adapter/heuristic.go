package adapter

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/tristanbietsch/rex/internal/daemon/termtext"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// ErrUnknownDetect signals an unsupported detect.kind in the registry.
var ErrUnknownDetect = errors.New("unknown detect kind")

// HeuristicCLI is a regex+idle adapter for CLIs without structured output.
type HeuristicCLI struct {
	prompt *regexp.Regexp
	done   *regexp.Regexp // nil = no auto-done; manual completion or process exit only
	busy   *regexp.Regexp // nil = none; text shown only while the agent works
	idle   time.Duration

	// login matches the agent's signed-out screen (nil = none). While it is
	// the latest thing on screen the session waits, with notice as the reason.
	login      *regexp.Regexp
	notice     string
	loginShown atomic.Bool
}

// Noticer is implemented by adapters that know a fixed notice for the
// session's current screen (the agent is signed out). The supervisor shows it
// as the session's last line.
type Noticer interface {
	// Notice is the text to show, "" when there is none.
	Notice() string
}

// setLogin sets the signed-out pattern and its notice ("" regex clears it).
func (h *HeuristicCLI) setLogin(loginRegex, notice string) error {
	if loginRegex == "" {
		h.login = nil
		return nil
	}
	re, err := compileMultilineRegex(loginRegex)
	if err != nil {
		return fmt.Errorf("compile login regex %q: %w", loginRegex, err)
	}
	h.login, h.notice = re, notice
	return nil
}

// Notice implements Noticer: the login notice while the signed-out screen is
// the latest the adapter saw, "" otherwise.
func (h *HeuristicCLI) Notice() string {
	if h.loginShown.Load() {
		return h.notice
	}
	return ""
}

// setBusy sets the optional busy pattern ("" clears it).
func (h *HeuristicCLI) setBusy(busyRegex string) error {
	if busyRegex == "" {
		h.busy = nil
		return nil
	}
	busy, err := compileMultilineRegex(busyRegex)
	if err != nil {
		return fmt.Errorf("compile busy regex %q: %w", busyRegex, err)
	}
	h.busy = busy
	return nil
}

// lastMatch is where re last matches s, or -1.
func lastMatch(re *regexp.Regexp, s string) int {
	if re == nil {
		return -1
	}
	all := re.FindAllStringIndex(s, -1)
	if len(all) == 0 {
		return -1
	}
	return all[len(all)-1][0]
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

func (h *HeuristicCLI) cleanTail(window []byte) string {
	tail := window
	if len(tail) > 4096 {
		tail = tail[len(tail)-4096:]
	}
	// Cursor moves → line breaks so `^` anchors land where the user sees a row start.
	return termtext.Normalize(tail)
}

// staleWorking is how long a full silence counts as "waiting on the user"
// even without a prompt match. Working agents stream output or animate a
// spinner/timer; a frozen screen is a dialog (auth, trust, menu) or an idle
// REPL whose prompt the regex doesn't know.
const staleWorking = 15 * time.Second

// Detect implements Adapter. Precedence after the idle gate: done > busy
// shown after the last prompt (until stale) > prompt > stale silence >
// working.
//
// The window is raw output, not the screen: a full-screen agent that swaps
// its footer ("? for shortcuts" → "esc to cancel") leaves the old text in the
// window, so the busy pattern wins only when it appeared after the last
// prompt match. It does not outlast the stale-silence rule: a working agent
// keeps animating a spinner, so a screen frozen that long with the busy
// footer is waiting on the user (a permission question, say).
func (h *HeuristicCLI) Detect(window []byte, idle time.Duration) protocol.State {
	if idle < h.idle {
		return protocol.StateWorking
	}
	clean := h.cleanTail(window)
	if h.done != nil && h.done.MatchString(clean) {
		return protocol.StateDone
	}
	prompt := lastMatch(h.prompt, clean)
	if h.login != nil {
		// Signed out only while the login text is the latest screen: a prompt
		// after it means the agent got past it.
		login := lastMatch(h.login, clean)
		h.loginShown.Store(login >= 0 && login > prompt && login > lastMatch(h.busy, clean))
		if h.loginShown.Load() {
			return protocol.StateNeedsInput
		}
	}
	if busy := lastMatch(h.busy, clean); busy > prompt && idle < staleWorking {
		return protocol.StateWorking
	}
	if prompt >= 0 {
		return protocol.StateNeedsInput
	}
	if idle >= staleWorking {
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
