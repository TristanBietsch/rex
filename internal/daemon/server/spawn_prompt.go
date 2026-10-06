package server

import (
	"strings"
	"time"
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

// initialPromptDelivery decides argv vs PTY paste for a tool's first prompt.
// Full-screen TUIs (claude, codex, gemini) take the prompt natively on argv;
// pasting into a half-drawn TUI is racy. Ollama's `run MODEL PROMPT` is
// one-shot (answers and exits), so it keeps the PTY paste path.
func initialPromptDelivery(toolID string, cmdArgs []string, prompt string) promptDelivery {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return promptDelivery{}
	}
	base := append([]string{}, cmdArgs...)
	switch toolID {
	case "claude", "codex":
		// `--` stops option parsing so a prompt starting with `-` stays a prompt.
		return promptDelivery{argv: append(base, "--", prompt)}
	case "gemini":
		// A bare positional is one-shot in gemini; -i keeps the session interactive.
		return promptDelivery{argv: append(base, "--prompt-interactive="+prompt)}
	case "ollama":
		return promptDelivery{plainPaste: true, readySettle: 800 * time.Millisecond}
	default:
		return promptDelivery{}
	}
}
