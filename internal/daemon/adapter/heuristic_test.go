package adapter

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestHeuristic_NeedsInputWhenIdleAndPromptMatches(t *testing.T) {
	h, err := NewHeuristic("^awaiting input:", "", 100*time.Millisecond)
	require.NoError(t, err)
	got := h.Detect([]byte("hello\nawaiting input:"), 200*time.Millisecond)
	require.Equal(t, protocol.StateNeedsInput, got)
}

func TestHeuristic_WorkingWhenNotIdle(t *testing.T) {
	h, err := NewHeuristic("^awaiting input:", "", 100*time.Millisecond)
	require.NoError(t, err)
	got := h.Detect([]byte("doing things..."), 10*time.Millisecond)
	require.Equal(t, protocol.StateWorking, got)
}

func TestHeuristic_WorkingWhenIdleButNoPromptMatch(t *testing.T) {
	h, err := NewHeuristic("^awaiting input:", "", 100*time.Millisecond)
	require.NoError(t, err)
	got := h.Detect([]byte("doing things"), 5*time.Second)
	require.Equal(t, protocol.StateWorking, got)
}

func TestNewHeuristic_RejectsBadRegex(t *testing.T) {
	_, err := NewHeuristic("[unclosed", "", 100*time.Millisecond)
	require.Error(t, err)
	require.Contains(t, err.Error(), "compile prompt regex")
}

// Ollama wraps the prompt with placeholder text on the same line:
// `>>> Send a message (/? for help)`. The line never ends with `>>> `, so
// the old `(?m)>>> $` pattern never matched. `(?m)^>>> ` should.
func TestHeuristic_OllamaPromptWithPlaceholder(t *testing.T) {
	h, err := NewHeuristic("^>>> Send a message", "", 100*time.Millisecond)
	require.NoError(t, err)
	got := h.Detect([]byte("hi there\n>>> Send a message (/? for help)"), 2*time.Second)
	require.Equal(t, protocol.StateNeedsInput, got)
}

// Codex (v0.130.0) emits its prompt wrapped in cyan ANSI; the visible char
// is `›` (U+203A). Without ANSI stripping the `^` anchor fails.
func TestHeuristic_CodexPromptWithAnsi(t *testing.T) {
	h, err := NewHeuristic(`^[ \t]*›`, "", 100*time.Millisecond)
	require.NoError(t, err)
	got := h.Detect([]byte("response done\n\x1b[36m› \x1b[mthis is a test"), 2*time.Second)
	require.Equal(t, protocol.StateNeedsInput, got)
}

// Full-TUI agents (codex, gemini) redraw via cursor positioning instead of
// newlines — the visible "lines" are joined by `\x1b[H` / `\x1b[<row>;<col>H`
// rather than `\n`. Without translating those to newlines, `^›` lands in the
// middle of one giant joined string and never matches.
func TestHeuristic_CodexPromptViaCursorPositioning(t *testing.T) {
	h, err := NewHeuristic(`^[ \t]*›`, "", 100*time.Millisecond)
	require.NoError(t, err)
	raw := "\x1b[2J\x1b[H• Test received.\x1b[5;1H\x1b[K› Implement {feature}\x1b[10;1Hgpt-5.5 low · ~/dev/rex"
	got := h.Detect([]byte(raw), 2*time.Second)
	require.Equal(t, protocol.StateNeedsInput, got)
}

func TestHeuristic_IsReadyForInput_MatchesPromptRegex(t *testing.T) {
	h, err := NewHeuristic("^>>> ", "", 100*time.Millisecond)
	require.NoError(t, err)
	require.True(t, h.IsReadyForInput([]byte("hi there\n>>> Send a message"), 0))
}

// Ollama renders its prompt via cursor positioning rather than newlines:
// `\x1b[2K\x1b[1G\x1b[?2004h>>> Send a message`. Without recognizing `[1G`
// as a logical line break, the `^>>> ` anchor never matches and the
// initial-prompt goroutine times out. Real captured output from a live ollama
// session; this is the regression that broke ollama prompt delivery.
func TestHeuristic_IsReadyForInput_OllamaCursorPositioning(t *testing.T) {
	h, err := NewHeuristic("^>>> ", "", 100*time.Millisecond)
	require.NoError(t, err)
	raw := "\x1b[?2026h\x1b[?25l\x1b[1G⠙ \x1b[K\x1b[?25h\x1b[?2026l\x1b[2K\x1b[1G\x1b[?25h\x1b[?2004h>>> \x1b[38;5;245mSend a message (/? for help)\x1b[28D\x1b[0m"
	require.True(t, h.IsReadyForInput([]byte(raw), 0),
		"prompt regex should match after cursor-position-to-newline replacement")
}

func TestHeuristic_IsReadyForInput_NoMatch(t *testing.T) {
	h, err := NewHeuristic("^>>> ", "", 100*time.Millisecond)
	require.NoError(t, err)
	require.False(t, h.IsReadyForInput([]byte("loading model..."), 0))
}

func TestHeuristic_IsReadyForInput_IgnoresIdle(t *testing.T) {
	h, err := NewHeuristic("^>>> ", "", 100*time.Millisecond)
	require.NoError(t, err)
	got1 := h.IsReadyForInput([]byte(">>> "), 1*time.Millisecond)
	got2 := h.IsReadyForInput([]byte(">>> "), 60*time.Second)
	require.True(t, got1)
	require.True(t, got2)
}

func TestHeuristic_IsReadyForInput_AnsiAndCursorScrubbed(t *testing.T) {
	h, err := NewHeuristic(`^[ \t]*›`, "", 100*time.Millisecond)
	require.NoError(t, err)
	raw := "\x1b[2J\x1b[H• Test received.\x1b[5;1H\x1b[K\x1b[36m› \x1b[mtype here"
	require.True(t, h.IsReadyForInput([]byte(raw), 0))
}

func TestHeuristic_DoneRegexMatchesReturnsDone(t *testing.T) {
	h, err := NewHeuristic("^> ", "^✓ session complete$", 100*time.Millisecond)
	require.NoError(t, err)
	got := h.Detect([]byte("output here\n✓ session complete"), 200*time.Millisecond)
	require.Equal(t, protocol.StateDone, got)
}

func TestHeuristic_DoneRegexEmptyKeepsExistingBehavior(t *testing.T) {
	h, err := NewHeuristic("^awaiting input:", "", 100*time.Millisecond)
	require.NoError(t, err)
	got := h.Detect([]byte("awaiting input:"), 200*time.Millisecond)
	require.Equal(t, protocol.StateNeedsInput, got)
}

func TestHeuristic_DoneRegexWinsOverPrompt(t *testing.T) {
	h, err := NewHeuristic("^> ", "^✓ done$", 100*time.Millisecond)
	require.NoError(t, err)
	// Both patterns appear in the window; done wins.
	got := h.Detect([]byte("✓ done\n> "), 200*time.Millisecond)
	require.Equal(t, protocol.StateDone, got)
}

func TestHeuristic_DoneRegexGatedByIdle(t *testing.T) {
	h, err := NewHeuristic("^> ", "^✓ done$", 200*time.Millisecond)
	require.NoError(t, err)
	// Idle < idle_ms — should return Working regardless of pattern match.
	got := h.Detect([]byte("✓ done"), 10*time.Millisecond)
	require.Equal(t, protocol.StateWorking, got)
}

func TestNewHeuristic_RejectsBadDoneRegex(t *testing.T) {
	_, err := NewHeuristic("^> ", "[unclosed", 100*time.Millisecond)
	require.Error(t, err)
	require.Contains(t, err.Error(), "compile done regex")
}

// Gemini draws its input box indented; `\s` in the old regex also let the
// match span a newline onto unrelated output.
func TestHeuristic_GeminiPromptIndentedSameLineOnly(t *testing.T) {
	h, err := NewHeuristic(`^[ \t]*>[ \t]`, "", 100*time.Millisecond)
	require.NoError(t, err)
	require.True(t, h.IsReadyForInput([]byte("│  > Type your message"), 0) ||
		h.IsReadyForInput([]byte("   > Type your message"), 0))
	require.False(t, h.IsReadyForInput([]byte("result>\nnext line"), 0))
	require.False(t, h.IsReadyForInput([]byte(">\nnext"), 0))
}

// A frozen screen with no prompt match (auth / trust dialog) is waiting on the
// user, not working.
func TestHeuristic_StaleSilenceIsNeedsInput(t *testing.T) {
	h, err := NewHeuristic(`^[ \t]*>[ \t]`, "", 1200*time.Millisecond)
	require.NoError(t, err)
	dialog := []byte("│ ● 1. Sign in with Google\n│   2. Use Gemini API Key\n")
	require.Equal(t, protocol.StateWorking, h.Detect(dialog, 2*time.Second))
	require.Equal(t, protocol.StateNeedsInput, h.Detect(dialog, staleWorking))
}

// agy-style turn, built from the escape patterns its TUI writes (no real
// session data): the idle footer, a turn that swaps the footer to "esc to
// cancel" with a spinner, then the idle footer again.
var (
	agyIdleFooter = "\x1b[?25l\r\x1b[2A\x1b[K\n\n\n\n\x1b[90m? for shortcuts\x1b[m\x1b[2A\x1b[13D\x1b[?25h"
	agyBusyFooter = "\x1b[?25l\r\x1b[2A\xe2\xa3\xb7  \x1b[90mGenerating...\x1b[m\r\n\n\n\n\x1b[90mesc to cancel\x1b[m  \x1b[2A\x1b[13D\x1b[?25h"
	agyInputLine  = "\x1b[20;1H\x1b[K> \x1b[20;3H"
)

func agyHeuristic(t *testing.T) *HeuristicCLI {
	t.Helper()
	h, err := NewHeuristic(`\? for shortcuts`, "", 1200*time.Millisecond)
	require.NoError(t, err)
	require.NoError(t, h.setBusy("esc to cancel"))
	return h
}

func TestHeuristic_BusyFooterAfterPromptIsWorking(t *testing.T) {
	h := agyHeuristic(t)
	idle := []byte(agyInputLine + agyIdleFooter)
	require.Equal(t, protocol.StateNeedsInput, h.Detect(idle, 2*time.Second), "idle footer")

	turn := append(append([]byte{}, idle...), []byte(agyBusyFooter+agyInputLine)...)
	require.Equal(t, protocol.StateWorking, h.Detect(turn, 2*time.Second),
		"a quiet stretch mid-turn: the old idle footer is still in the window")
	require.Equal(t, protocol.StateNeedsInput, h.Detect(turn, staleWorking),
		"frozen 15 s with the busy footer: waiting on the user (e.g. a permission question)")

	done := append(append([]byte{}, turn...), []byte("  1\r\n  2\r\n"+agyIdleFooter)...)
	require.Equal(t, protocol.StateNeedsInput, h.Detect(done, 2*time.Second), "turn over: idle footer last")
}

// Without the busy pattern the same quiet stretch reads as waiting: the
// regression busy_regex fixes.
func TestHeuristic_WithoutBusyQuietTurnLooksIdle(t *testing.T) {
	h, err := NewHeuristic(`\? for shortcuts`, "", 1200*time.Millisecond)
	require.NoError(t, err)
	turn := []byte(agyInputLine + agyIdleFooter + agyBusyFooter + agyInputLine)
	require.Equal(t, protocol.StateNeedsInput, h.Detect(turn, 2*time.Second))
}

func TestHeuristic_BusyRegexMustCompile(t *testing.T) {
	h, err := NewHeuristic(`x`, "", time.Second)
	require.NoError(t, err)
	require.Error(t, h.setBusy("("))
	require.NoError(t, h.setBusy(""))
	require.Nil(t, h.busy)
}
