package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestTruncateWords(t *testing.T) {
	for _, tc := range []struct {
		name, in string
		n        int
		want     string
	}{
		{"fits", "Running tests", 20, "Running tests"},
		{"exact", "Running tests", 13, "Running tests"},
		{"cuts on a word", "Running pnpm test:billing after the fix", 30, "Running pnpm test:billing…"},
		{"cut lands on a boundary", "Editing main.go now please", 16, "Editing main.go…"},
		{"backs up", "Editing main now please", 15, "Editing main…"},
		{"no space late enough: mid-word", "Verifying " + strings.Repeat("x", 40), 20, "Verifying xxxxxxxxx…"},
		{"trims trailing separators", "Editing files, then running tests and more", 15, "Editing files…"},
		{"em dash tail", "Running tests — 3 failures left in the suite", 16, "Running tests…"},
		{"width one", "abcdef", 1, "…"},
		{"zero", "abc", 0, ""},
		{"wide runes by columns", "日本語日本語日本語", 7, "日本語…"},
	} {
		got := truncateWords(tc.in, tc.n)
		require.Equal(t, tc.want, got, tc.name)
		require.LessOrEqual(t, ansi.StringWidth(got), tc.n, tc.name)
	}
}

func TestTruncateWords_Sanitizes(t *testing.T) {
	got := truncateWords("Running\nrm -rf \x1b[2Jtests "+strings.Repeat("y", 40), 30)
	require.NotContains(t, got, "\n")
	require.NotContains(t, got, "\x1b")
	require.LessOrEqual(t, ansi.StringWidth(got), 30)
}

// The last animation frame is the settled cell: no jump when it ends.
func TestRenderAnimFrame_EndsAtSettledText(t *testing.T) {
	long := "Running pnpm test:billing after the rounding fix — 3 failures left in invoice.spec.ts"
	for _, effect := range []string{"typewriter", "decode", "wipe"} {
		a := DescAnim{From: "old text", To: long, Effect: effect, StartedAt: time.Unix(0, 0), Duration: time.Second}
		const w = 40
		got := renderAnimFrame(a, w, a.StartedAt.Add(a.Duration))
		want := truncateWords(long, w)
		require.Equal(t, want, strings.TrimRight(got, " "), effect)
		require.Equal(t, w, len([]rune(got)), effect)
	}
}
