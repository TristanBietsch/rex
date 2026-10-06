package termtext

import (
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
