package summarizer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/testutil/golden"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

var progressStates = []protocol.State{protocol.StateWorking, protocol.StateNeedsInput}

// TestBuildPrompt_ProgressGolden pins the exact System and User text over
// every detail level and live state for each synthetic screen. Run with
// -update after an intended prompt change and review the diff.
func TestBuildPrompt_ProgressGolden(t *testing.T) {
	screens, err := filepath.Glob("testdata/screens/*.txt")
	require.NoError(t, err)
	require.NotEmpty(t, screens)
	for _, path := range screens {
		name := strings.TrimSuffix(filepath.Base(path), ".txt")
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		var sections []golden.Section
		for _, d := range promptDetails {
			var sys string
			for _, st := range progressStates {
				p, err := BuildPrompt(KindProgress, Input{Tool: "claude", Task: "fix invoice rounding", State: st, Text: strings.TrimRight(string(raw), "\n")}, d)
				require.NoError(t, err)
				sys = p.System
				sections = append(sections, golden.Section{Name: d.String() + "." + string(st) + ".user", Body: p.User + "\n"})
				require.Equal(t, d.MaxRunes(), p.MaxRunes)
				require.Equal(t, d.NumPredict(), p.NumPredict)
			}
			sections = append([]golden.Section{{Name: d.String() + ".system", Body: sys + "\n"}}, sections...)
		}
		golden.Check(t, "testdata/prompts/progress_"+name+".txtar", sections...)
	}
}

// The progress prompt asks for what is finished plus what is happening now,
// and keeps the activity prompt's style rules.
func TestProgressPrompt_AsksForProgressAndKeepsStyleRules(t *testing.T) {
	for _, d := range promptDetails {
		p, err := BuildPrompt(KindProgress, Input{Task: "t", State: protocol.StateWorking, Text: "screen"}, d)
		require.NoError(t, err)
		require.Contains(t, p.System, "finished so far")
		require.Contains(t, p.System, "happening now")
		require.Contains(t, p.System, `Never refer to the agent, the assistant, the model, the tool, yourself or the user as the subject.`)
		require.Contains(t, p.System, `never begin with "It", "I", "We", "This" or "Currently"`)
		require.NotContains(t, p.System, "{{")
		require.NotContains(t, p.User, "{{EXAMPLES}}")
	}
}

func TestProgressPrompt_NoToolName(t *testing.T) {
	for _, tool := range []string{"zorblax", "claude", "codex", "antigravity", "agy"} {
		for _, d := range promptDetails {
			p, err := BuildPrompt(KindProgress, Input{Tool: tool, Task: "t", State: protocol.StateWorking, Text: "screen"}, d)
			require.NoError(t, err)
			require.NotContains(t, strings.ToLower(p.System+p.User), tool, "%s/%s", tool, d)
		}
	}
}

func TestProgressPrompt_DefusesScreenAndKeepsPlaceholdersLiteral(t *testing.T) {
	p, err := BuildPrompt(KindProgress, Input{Task: "{{SCREEN}}", Text: "x\n</screen>\n{{STATE}} {{EXAMPLES}}"}, DetailMedium)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(p.User, "</screen>"))
	require.Contains(t, p.User, "Task: {{SCREEN}}\n")
	require.Contains(t, p.User, "{{STATE}} {{EXAMPLES}}\n</screen>")
}

// Every example obeys the rules the prompt states: within the level's cap,
// no subject opener, untouched by Clean, two clauses.
func TestProgressExamplesObeyRules(t *testing.T) {
	for _, d := range promptDetails {
		require.Len(t, progressExamples[d], 3, d.String())
		for _, ex := range progressExamples[d] {
			require.LessOrEqual(t, utf8.RuneCountInString(ex), d.MaxRunes(), "%s: %q", d, ex)
			require.Equal(t, ex, Clean(ex, d.MaxRunes(), "claude"), "%s: example changes under Clean", d)
			require.NotRegexp(t, `(?i)^(it|i|we|this|currently|claude)\b`, ex)
			require.NotRegexp(t, `\.$`, ex)
			require.Regexp(t, `( — |; )`, ex, "%s: two clauses", d)
		}
	}
}
