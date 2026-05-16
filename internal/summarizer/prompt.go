package summarizer

import (
	"fmt"
	"strings"
)

const maxDescription = 60

// buildPrompt assembles the few-shot prompt for the Ollama call.
func buildPrompt(tool, slug, transcript string) string {
	return fmt.Sprintf(`You are watching a CLI coding agent named %s working on a task slugged "%s".
Below is the recent terminal output from the agent (ANSI stripped).
In ONE line of at most 60 characters, describe what the agent is doing RIGHT NOW.
Use simple verbs. No quotes, no preface, no trailing period.

Examples:
- rewriting webhook handlers — 12 of 14
- running pnpm test:billing
- waiting on user: pick a theme

Transcript:
%s
`, tool, slug, transcript)
}

// cleanResponse normalizes whatever the model produced into the form that
// belongs in the desc column: at most 60 runes, no surrounding quotes, no
// "Output:" / "Description:" / "- " preface, no trailing period.
func cleanResponse(s string) string {
	s = strings.TrimSpace(s)
	// Strip common prefixes the model adds.
	for _, p := range []string{"Output:", "Description:", "Activity:", "Status:"} {
		if strings.HasPrefix(strings.ToLower(s), strings.ToLower(p)) {
			s = strings.TrimSpace(s[len(p):])
			break
		}
	}
	for strings.HasPrefix(s, "- ") || strings.HasPrefix(s, "> ") || strings.HasPrefix(s, "* ") {
		s = strings.TrimSpace(s[2:])
	}
	// Strip surrounding quotes.
	if len(s) >= 2 {
		switch s[0] {
		case '"', '\'', '`':
			if s[len(s)-1] == s[0] {
				s = s[1 : len(s)-1]
			}
		}
	}
	// Drop a trailing period that isn't part of an ellipsis.
	if strings.HasSuffix(s, ".") && !strings.HasSuffix(s, "..") {
		s = strings.TrimRight(s, ".")
	}
	s = strings.TrimSpace(s)
	// Clamp to 60 runes with ellipsis.
	if runes := []rune(s); len(runes) > maxDescription {
		s = string(runes[:maxDescription-1]) + "…"
	}
	return s
}
