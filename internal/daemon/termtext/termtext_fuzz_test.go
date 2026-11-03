package termtext

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// requireReadableLines asserts the contract Lines documents: no blank lines,
// no chrome, no consecutive duplicates, whitespace already collapsed.
func requireReadableLines(t *testing.T, lines []string) {
	t.Helper()
	for i, l := range lines {
		require.NotEmpty(t, l, "blank line at %d", i)
		require.False(t, IsChrome(l), "chrome line %q kept", l)
		require.Equal(t, collapse(l), l, "line %q not collapsed", l)
		if i > 0 {
			require.NotEqual(t, lines[i-1], l, "repaint duplicate %q kept", l)
		}
	}
}

// FuzzTermtext drives every public termtext function with raw PTY bytes. The
// summarizer and the TUI call these on untrusted child output, so they must
// not panic and must keep the guarantees their docs state.
func FuzzTermtext(f *testing.F) {
	for _, s := range []string{
		"\x1b[2J\x1b[H• Test received.\x1b[5;1H\x1b[K\x1b[36m› \x1b[mtype here\r\x1b[2Cnext\x1b[1Bdown",
		"⠋ loading\nreading files\nreading files\n✶\n───\nwrote main.go\nesc to interrupt\n",
		"first line here\nsecond line here\nthird line here",
		"✶\n──\n",
		"❯ do not use any tools\n⏺ Hi.\n  What're you working on?\n❯ \n  ⏸ manual mode on",
		"› do not use any tools\n• Hi. What are we working on?\n› Improve documentation",
		"hello there\nsecond line",
		"\x1b", "\x1b[", "\x1b]0;title\x07after", "\xff\xfe broken utf8 line",
	} {
		f.Add([]byte(s), 20)
	}

	f.Fuzz(func(t *testing.T, b []byte, max int) {
		norm := Normalize(b)
		require.NotContains(t, norm, "\r", "carriage return survived Normalize")
		// Known failure under -fuzz: "\xc9\x1b[31m" comes back unchanged,
		// because ansi.Strip passes escapes that follow an invalid UTF-8
		// lead byte. Kept as a finding for the hardening backlog.
		require.NotContains(t, norm, "\x1b", "escape survived Normalize")

		lines := Lines(b)
		requireReadableLines(t, lines)

		last := LastLine(b)
		if len(lines) == 0 {
			require.Empty(t, last)
		} else {
			require.Equal(t, lines[len(lines)-1], last)
		}

		tail := Tail(b, max)
		if max <= 0 {
			require.Equal(t, strings.Join(lines, "\n"), tail)
		} else {
			require.LessOrEqual(t, len(tail), max, "Tail exceeded max bytes")
		}

		requireReadableLines(t, ScreenLines(string(b)))
		_ = IsChrome(string(b))
	})
}
