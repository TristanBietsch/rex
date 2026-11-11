package summarizer

import "fmt"

// maxDescription is the original cap, still the width of cleanResponse.
const maxDescription = 60

// activityPrompt builds the KindActivity prompt.
func activityPrompt(in Input, _ Detail) Prompt {
	return Prompt{
		User: fmt.Sprintf(`You are watching a CLI coding agent named %s.
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
`, in.Tool, in.Task, in.Text),
		MaxRunes:   maxDescription,
		NumPredict: 30,
	}
}
