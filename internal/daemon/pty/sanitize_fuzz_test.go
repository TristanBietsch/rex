package pty

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// FuzzSanitizeForDisplay feeds raw PTY lines to sanitizeForDisplay. Its
// output is rendered verbatim in the TUI board, so any control rune that
// survives (C0, DEL, C1 such as U+009B CSI) can drive the user's terminal.
func FuzzSanitizeForDisplay(f *testing.F) {
	for _, s := range []string{
		"", "plain", "\x1b[33myellow\x1b[0m", "\x1b[2J\x1b[Hhello",
		"first\rsecond", "x\x07y\x08z", "\x1b]0;title\x07payload",
		"ok ✓ done", "data\x1b[K   ",
		"a\u009b31mb", "x\u009d0;t\u009cy", "\xc9\x1b[31mred", "\x9b31m",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		out := sanitizeForDisplay(in)
		if !utf8.ValidString(out) {
			t.Fatalf("sanitizeForDisplay(%q) = %q: invalid UTF-8", in, out)
		}
		for _, r := range out {
			if unicode.IsControl(r) {
				t.Fatalf("sanitizeForDisplay(%q) = %q: control rune %U survived", in, out, r)
			}
		}
		if strings.HasPrefix(out, " ") || strings.HasSuffix(out, " ") || strings.Contains(out, "  ") {
			t.Fatalf("sanitizeForDisplay(%q) = %q: whitespace not collapsed and trimmed", in, out)
		}
		_ = hasVisibleText([]byte(in))
	})
}
