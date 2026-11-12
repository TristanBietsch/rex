package loop

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStripEcho(t *testing.T) {
	prompt := "Fix the build and then print <promise>DONE</promise> when it is green"
	// The terminal soft-wrapped the echo so the marker sits alone on a row.
	wrapped := "> Fix the build and then print\n  <promise>DONE</promise>\n  when it is green\nworking..."
	got := stripEcho(wrapped, prompt)
	require.NotContains(t, got, "promise")
	require.Contains(t, got, "working...")
	require.Contains(t, got, ">", "decoration the terminal added stays")

	// Output that is not the prompt is untouched.
	out := "all good\n<promise>DONE</promise>\n"
	require.Equal(t, out, stripEcho(out, prompt))
	require.Equal(t, out, stripEcho(out), "no prompts")
	require.Equal(t, out, stripEcho(out, "   "), "a blank prompt matches nothing")

	// Two echoes, and the same prompt given twice, are both removed.
	two := wrapped + "\n" + wrapped
	require.NotContains(t, stripEcho(two, prompt, prompt), "promise")
}

func TestMarkerMatches_IgnoresWrappedPromptEcho(t *testing.T) {
	d, err := Parse([]byte("name: x\nkind: repeat\ntool: claude\nmodel: sonnet\n"+
		"prompt: \"Fix it and print <promise>DONE</promise> when it is green\"\n"+
		"followup_prompt: \"Keep going; print <promise>DONE</promise> when green\"\n"+
		"until:\n  marker: '<promise>DONE</promise>'\n"), "x")
	require.NoError(t, err)

	echo := "> Fix it and print\n  <promise>DONE</promise>\n  when it is green\n> Keep going; print\n  <promise>DONE</promise>\n  when green\n"
	require.False(t, d.MarkerMatches(echo), "a wrapped prompt echo is not the agent finishing")
	require.True(t, d.MarkerMatches(echo+"all tests pass\n<promise>DONE</promise>\n"), "the agent's own line still counts")
	require.False(t, d.MarkerMatches(""))

	none, err := Parse([]byte(goodYAML), "fix")
	require.NoError(t, err)
	require.False(t, none.MarkerMatches("anything"))
}

// The window Output returns is only the end of the screen: a long prompt's
// echo can start before it. The cut-off head of the echo, even cut in the
// middle of a word, must not leave a wrapped marker row behind.
func TestStripEcho_EchoCutAtTheStartOfTheWindow(t *testing.T) {
	prompt := "Refactor the parser carefully, run the whole test suite and then print <promise>DONE</promise> once everything is green"
	full := "> Refactor the parser carefully, run the whole\n  test suite and then print\n  <promise>DONE</promise>\n  once everything is green\n"
	// Cut the window in the middle of "whole" (the echo's head is gone).
	i := strings.Index(full, "ole\n")
	require.GreaterOrEqual(t, i, 0)
	window := full[i:]
	got := stripEcho(window, prompt)
	require.NotContains(t, got, "promise")

	d, err := Parse([]byte("name: x\nkind: repeat\ntool: claude\nmodel: sonnet\nprompt: \""+prompt+"\"\nuntil:\n  marker: '<promise>DONE</promise>'\n"), "x")
	require.NoError(t, err)
	require.False(t, d.MarkerMatches(window), "no early stop from a cut-off echo")
	require.True(t, d.MarkerMatches(window+"all green\n<promise>DONE</promise>\n"))

	// Output that merely starts with some of the same words is kept.
	require.Contains(t, stripEcho("plain\n<promise>DONE</promise>\n", prompt), "<promise>DONE</promise>")
}
