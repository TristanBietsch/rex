package summarizer

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

const maxDescription = 60

// buildPrompt assembles the few-shot prompt for the Ollama call.
func buildPrompt(tool, slug, task, transcript string) string {
	if task == "" {
		task = slug
	}
	return fmt.Sprintf(`You are watching a CLI coding agent named %s.
Its task: %q
Below is the agent's current terminal screen (UI chrome removed).
In ONE line of at most 60 characters, describe what the agent is doing RIGHT NOW.
Use simple verbs. No quotes, no preface, no trailing period.

Examples:
- rewriting webhook handlers — 12 of 14
- running pnpm test:billing
- waiting on user: pick a theme

Transcript:
%s
`, tool, task, transcript)
}

// cleanResponse normalizes whatever the model produced into the form that
// belongs in the desc column: at most 60 runes, no surrounding quotes, no
// "Output:" / "Description:" / "- " preface, no trailing period.
//
// The response is untrusted: the model paraphrases the agent's transcript,
// and every attached TUI prints the result verbatim. So it is reduced to
// the first line of printable text first (see firstSafeLine) — an OSC 52
// clipboard write or a screen clear in the transcript must not reach the
// user's terminal through the summary (backlog S-03).
func cleanResponse(s string) string {
	s = firstSafeLine(s)
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
		s = strings.TrimSuffix(s, ".")
	}
	s = strings.TrimSpace(s)
	// Clamp to 60 runes with ellipsis.
	if runes := []rune(s); len(runes) > maxDescription {
		s = string(runes[:maxDescription-1]) + "…"
	}
	return s
}

// firstSafeLine returns the first non-blank line of s as printable text:
// invalid UTF-8 is dropped, ANSI escape sequences (CSI, OSC, DCS, …) are
// removed whole, the text is cut at the first line break, tabs become
// spaces, and every other C0/C1 control rune and DEL is dropped. The result
// is trimmed. Order matters: sequences are stripped before the cut so a
// newline inside an OSC payload can't split it into a printable tail.
//
// internal/surface/tui has a similar render-side helper (sanitizeText); the
// wire protocol package is getting a shared one. The three should converge.
func firstSafeLine(s string) string {
	s = ansi.Strip(strings.ToValidUTF8(s, ""))
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\t':
			return ' '
		case unicode.IsControl(r):
			return -1
		default:
			return r
		}
	}, s)
	return strings.TrimSpace(s)
}
