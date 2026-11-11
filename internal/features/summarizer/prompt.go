package summarizer

import "fmt"

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
