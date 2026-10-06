package termtext

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalize_CursorMovesBecomeLineBreaks(t *testing.T) {
	raw := "\x1b[2J\x1b[H• Test received.\x1b[5;1H\x1b[K\x1b[36m› \x1b[mtype here\r\x1b[2Cnext\x1b[1Bdown"
	got := Normalize([]byte(raw))
	require.Contains(t, got, "\n› type here")
	require.Contains(t, got, "\n next\ndown")
}

func TestIsChrome(t *testing.T) {
	for _, l := range []string{
		"✶", "─────────────", "42", "0s)", "✻29", "s",
		"· esc to interrupt · ←…",
		"⏵⏵ auto mode on (shift+tab to cycle) · ← for agents",
		"113 tokens · thinking with high effort)",
		"⠋⠙⠹⠸⠼",
		"✳ Unfurling… (5s · thought for 4s)",
		"✻ Cooked for 2s · done 3:55 PM",
		">>> Send a message (/? for help)",
	} {
		require.True(t, IsChrome(l), l)
	}
	for _, l := range []string{
		"Pushed to origin/master (fee653a..223102e).",
		"running pnpm test:billing",
		"⏺ Bash(git status)",
	} {
		require.False(t, IsChrome(l), l)
	}
}

func TestLines_DropsChromeAndRepaintDuplicates(t *testing.T) {
	raw := "⠋ loading\nreading files\nreading files\n✶\n───\nwrote main.go\nesc to interrupt\n"
	require.Equal(t, []string{"⠋ loading", "reading files", "wrote main.go"}, Lines([]byte(raw)))
}

func TestTail_CutsOnLineBoundary(t *testing.T) {
	raw := []byte("first line here\nsecond line here\nthird line here")
	require.Equal(t, "third line here", Tail(raw, 20))
	require.Equal(t, "first line here\nsecond line here\nthird line here", Tail(raw, 0))
}

func TestLastLine_Empty(t *testing.T) {
	require.Equal(t, "", LastLine([]byte("✶\n──\n")))
}

// Rendered screens from live claude / codex sessions: the last content line is
// the agent's reply, not the input box or status bars below it.
func TestScreenLines_CutsInputBoxAndStatusBars(t *testing.T) {
	claude := strings.Join([]string{
		"❯ do not use any tools; just say hi then ask me one short question",
		"⏺ Hi.",
		"  What're you working on?",
		"✻ Cooked for 2s · done 3:55 PM",
		"────────────────────────────────────",
		"❯ ",
		"────────────────────────────────────",
		"  ⚠ Transcript saving is off",
		"  [CAVEMAN]",
		"  ⏸ manual mode on",
	}, "\n")
	lines := ScreenLines(claude)
	require.Equal(t, "What're you working on?", lines[len(lines)-1])

	codex := strings.Join([]string{
		"› do not use any tools; just say hi then ask me one short question",
		"• Hi. What are we working on?",
		"› Improve documentation in @filename",
		"  gpt-5.5 low · ~/Documents/personal/dev/rex",
	}, "\n")
	lines = ScreenLines(codex)
	require.Equal(t, "• Hi. What are we working on?", lines[len(lines)-1])

	plain := "hello there\nsecond line"
	require.Equal(t, []string{"hello there", "second line"}, ScreenLines(plain))
}
