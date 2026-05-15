package adapter

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/protocol"
)

func TestHeuristic_NeedsInputWhenIdleAndPromptMatches(t *testing.T) {
	h, err := NewHeuristic("^awaiting input:", 100*time.Millisecond)
	require.NoError(t, err)
	got := h.Detect([]byte("hello\nawaiting input:"), 200*time.Millisecond)
	require.Equal(t, protocol.StateNeedsInput, got)
}

func TestHeuristic_WorkingWhenNotIdle(t *testing.T) {
	h, err := NewHeuristic("^awaiting input:", 100*time.Millisecond)
	require.NoError(t, err)
	got := h.Detect([]byte("doing things..."), 10*time.Millisecond)
	require.Equal(t, protocol.StateWorking, got)
}

func TestHeuristic_WorkingWhenIdleButNoPromptMatch(t *testing.T) {
	h, err := NewHeuristic("^awaiting input:", 100*time.Millisecond)
	require.NoError(t, err)
	got := h.Detect([]byte("doing things"), 5*time.Second)
	require.Equal(t, protocol.StateWorking, got)
}

func TestNewHeuristic_RejectsBadRegex(t *testing.T) {
	_, err := NewHeuristic("[unclosed", 100*time.Millisecond)
	require.Error(t, err)
	require.Contains(t, err.Error(), "compile prompt regex")
}

// Ollama wraps the prompt with placeholder text on the same line:
// `>>> Send a message (/? for help)`. The line never ends with `>>> `, so
// the old `(?m)>>> $` pattern never matched. `(?m)^>>> ` should.
func TestHeuristic_OllamaPromptWithPlaceholder(t *testing.T) {
	h, err := NewHeuristic("^>>> ", 100*time.Millisecond)
	require.NoError(t, err)
	got := h.Detect([]byte("hi there\n>>> Send a message (/? for help)"), 2*time.Second)
	require.Equal(t, protocol.StateNeedsInput, got)
}

// Codex (v0.130.0) emits its prompt wrapped in cyan ANSI; the visible char
// is `›` (U+203A). Without ANSI stripping the `^` anchor fails.
func TestHeuristic_CodexPromptWithAnsi(t *testing.T) {
	h, err := NewHeuristic("^› ", 100*time.Millisecond)
	require.NoError(t, err)
	got := h.Detect([]byte("response done\n\x1b[36m› \x1b[mthis is a test"), 2*time.Second)
	require.Equal(t, protocol.StateNeedsInput, got)
}

// Full-TUI agents (codex, gemini) redraw via cursor positioning instead of
// newlines — the visible "lines" are joined by `\x1b[H` / `\x1b[<row>;<col>H`
// rather than `\n`. Without translating those to newlines, `^›` lands in the
// middle of one giant joined string and never matches.
func TestHeuristic_CodexPromptViaCursorPositioning(t *testing.T) {
	h, err := NewHeuristic("^› ", 100*time.Millisecond)
	require.NoError(t, err)
	raw := "\x1b[2J\x1b[H• Test received.\x1b[5;1H\x1b[K› Implement {feature}\x1b[10;1Hgpt-5.5 low · ~/dev/rex"
	got := h.Detect([]byte(raw), 2*time.Second)
	require.Equal(t, protocol.StateNeedsInput, got)
}
