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

func TestHeuristic_IsReadyForInput_MatchesPromptRegex(t *testing.T) {
	h, err := NewHeuristic("^>>> ", 100*time.Millisecond)
	require.NoError(t, err)
	require.True(t, h.IsReadyForInput([]byte("hi there\n>>> Send a message"), 0))
}

// Ollama renders its prompt via cursor positioning rather than newlines:
// `\x1b[2K\x1b[1G\x1b[?2004h>>> Send a message`. Without recognizing `[1G`
// as a logical line break, the `^>>> ` anchor never matches and the
// initial-prompt goroutine times out. Real captured output from a live ollama
// session; this is the regression that broke ollama prompt delivery.
func TestHeuristic_IsReadyForInput_OllamaCursorPositioning(t *testing.T) {
	h, err := NewHeuristic("^>>> ", 100*time.Millisecond)
	require.NoError(t, err)
	raw := "\x1b[?2026h\x1b[?25l\x1b[1G⠙ \x1b[K\x1b[?25h\x1b[?2026l\x1b[2K\x1b[1G\x1b[?25h\x1b[?2004h>>> \x1b[38;5;245mSend a message (/? for help)\x1b[28D\x1b[0m"
	require.True(t, h.IsReadyForInput([]byte(raw), 0),
		"prompt regex should match after cursor-position-to-newline replacement")
}

func TestHeuristic_IsReadyForInput_NoMatch(t *testing.T) {
	h, err := NewHeuristic("^>>> ", 100*time.Millisecond)
	require.NoError(t, err)
	require.False(t, h.IsReadyForInput([]byte("loading model..."), 0))
}

func TestHeuristic_IsReadyForInput_IgnoresIdle(t *testing.T) {
	h, err := NewHeuristic("^>>> ", 100*time.Millisecond)
	require.NoError(t, err)
	got1 := h.IsReadyForInput([]byte(">>> "), 1*time.Millisecond)
	got2 := h.IsReadyForInput([]byte(">>> "), 60*time.Second)
	require.True(t, got1)
	require.True(t, got2)
}

func TestHeuristic_IsReadyForInput_AnsiAndCursorScrubbed(t *testing.T) {
	h, err := NewHeuristic("^› ", 100*time.Millisecond)
	require.NoError(t, err)
	raw := "\x1b[2J\x1b[H• Test received.\x1b[5;1H\x1b[K\x1b[36m› \x1b[mtype here"
	require.True(t, h.IsReadyForInput([]byte(raw), 0))
}
