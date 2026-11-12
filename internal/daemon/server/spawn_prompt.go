package server

import (
	"strings"
	"time"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
)

// promptDelivery says how a tool receives the wizard / quick-spawn task text.
type promptDelivery struct {
	// argv is the full argv when the prompt rides on the command line; nil
	// means the supervisor must paste it into the PTY instead.
	argv []string
	// plainPaste skips DEC bracketed-paste markers (line-oriented REPLs).
	plainPaste bool
	// readySettle is how long output must be quiet before pasting.
	readySettle time.Duration
}

// initialPromptDelivery decides argv vs PTY paste for a tool's first prompt
// from its registry entry. Tools with prompt_args take the prompt on argv
// ({prompt} stays one element): pasting into a half-drawn full-screen TUI is
// racy. Tools with prompt_paste (a one-shot `ollama run MODEL PROMPT` would
// answer and exit) get it pasted. A tool with neither gets no initial prompt.
func initialPromptDelivery(tool registry.Tool, cmdArgs []string, prompt string) promptDelivery {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return promptDelivery{}
	}
	if len(tool.PromptArgs) > 0 {
		argv := append(append([]string{}, cmdArgs...), tool.PromptArgsFor(prompt)...)
		return promptDelivery{argv: argv}
	}
	if pp := tool.PromptPaste; pp != nil {
		return promptDelivery{plainPaste: pp.Plain, readySettle: time.Duration(pp.SettleMs) * time.Millisecond}
	}
	return promptDelivery{}
}
